// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// Beacon represents a ZRE UDP beacon for peer discovery
type Beacon struct {
	uuid     [16]byte           // Our node UUID
	port     uint16             // Our mailbox port (0 = silent)
	interval time.Duration      // Beacon broadcast interval
	
	// Network components
	conn     *net.UDPConn       // UDP socket for broadcasting
	addr     *net.UDPAddr       // Broadcast address
	
	// Channel-based communication
	discoveries chan *Discovery  // Channel for peer discoveries
	commands    chan beaconCmd   // Channel for control commands
	
	// State management
	ctx      context.Context    // Context for cancellation
	cancel   context.CancelFunc // Cancel function
	wg       sync.WaitGroup     // Wait group for goroutines
	running  bool               // Whether beacon is running
}

// Discovery represents a discovered peer
type Discovery struct {
	UUID [16]byte // Peer UUID
	Port uint16   // Peer port (0 means peer is leaving)
	Addr net.IP   // Peer IP address
	Time time.Time // When discovered
}

// beaconCmd represents internal beacon commands
type beaconCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan error  // Reply channel
}

// NewBeacon creates a new UDP beacon for peer discovery
func NewBeacon(uuid [16]byte, port uint16, interval time.Duration) (*Beacon, error) {
	if interval == 0 {
		interval = DefaultBeaconInterval
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	beacon := &Beacon{
		uuid:        uuid,
		port:        port,
		interval:    interval,
		discoveries: make(chan *Discovery, 100), // Buffer for peer discoveries
		commands:    make(chan beaconCmd, 10),   // Buffer for commands
		ctx:         ctx,
		cancel:      cancel,
	}
	
	return beacon, nil
}

// Start begins beacon operation (broadcasting and listening)
func (b *Beacon) Start() error {
	if b.running {
		return fmt.Errorf("beacon already running")
	}
	
	// Set up UDP socket for broadcasting
	if err := b.setupUDP(); err != nil {
		return fmt.Errorf("failed to setup UDP: %w", err)
	}
	
	b.running = true
	
	// Start broadcast goroutine
	b.wg.Add(1)
	go b.broadcastLoop()
	
	// Start listen goroutine
	b.wg.Add(1)
	go b.listenLoop()
	
	// Start command processing goroutine
	b.wg.Add(1)
	go b.commandLoop()
	
	return nil
}

// Stop stops the beacon and closes all channels
func (b *Beacon) Stop() {
	if !b.running {
		return
	}
	
	b.cancel()        // Cancel context
	b.wg.Wait()       // Wait for all goroutines to finish
	b.running = false
	
	if b.conn != nil {
		b.conn.Close()
	}
	
	close(b.discoveries)
	close(b.commands)
}

// Discoveries returns the channel for receiving peer discoveries
func (b *Beacon) Discoveries() <-chan *Discovery {
	return b.discoveries
}

// SetPort updates the beacon port (0 = silent mode)
func (b *Beacon) SetPort(port uint16) error {
	reply := make(chan error, 1)
	cmd := beaconCmd{
		action: "setport",
		data:   port,
		reply:  reply,
	}
	
	select {
	case b.commands <- cmd:
		return <-reply
	case <-b.ctx.Done():
		return b.ctx.Err()
	}
}

// setupUDP configures the UDP socket for beacon broadcasting
func (b *Beacon) setupUDP() error {
	// Create UDP socket
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: BeaconPort,
	})
	if err != nil {
		return fmt.Errorf("failed to create UDP socket: %w", err)
	}
	
	b.conn = conn
	
	// Set broadcast address
	b.addr = &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: BeaconPort,
	}
	
	return nil
}

// broadcastLoop handles periodic beacon broadcasting
func (b *Beacon) broadcastLoop() {
	defer b.wg.Done()
	
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if b.port > 0 { // Only broadcast if not in silent mode
				b.sendBeacon()
			}
		case <-b.ctx.Done():
			return
		}
	}
}

// listenLoop handles incoming beacon messages
func (b *Beacon) listenLoop() {
	defer b.wg.Done()
	
	buffer := make([]byte, BeaconSize)
	
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			// Set read timeout to avoid blocking indefinitely
			b.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			
			n, addr, err := b.conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, continue listening
				}
				continue // Other errors, continue listening
			}
			
			if discovery := b.parseBeacon(buffer[:n], addr.IP); discovery != nil {
				select {
				case b.discoveries <- discovery:
					// Discovery sent successfully
				default:
					// Channel full, drop discovery
				}
			}
		}
	}
}

// commandLoop processes internal commands
func (b *Beacon) commandLoop() {
	defer b.wg.Done()
	
	for {
		select {
		case cmd := <-b.commands:
			switch cmd.action {
			case "setport":
				port := cmd.data.(uint16)
				b.port = port
				cmd.reply <- nil
			}
		case <-b.ctx.Done():
			return
		}
	}
}

// sendBeacon broadcasts our beacon message
func (b *Beacon) sendBeacon() {
	beacon := make([]byte, BeaconSize)
	
	// Beacon format: "ZRE" + version + UUID + port
	copy(beacon[0:3], BeaconPrefix)
	beacon[3] = BeaconVersion
	copy(beacon[4:20], b.uuid[:])
	binary.BigEndian.PutUint16(beacon[20:22], b.port)
	
	b.conn.WriteToUDP(beacon, b.addr)
}

// parseBeacon parses an incoming beacon message
func (b *Beacon) parseBeacon(data []byte, sourceIP net.IP) *Discovery {
	if len(data) != BeaconSize {
		return nil
	}
	
	// Check beacon format
	if string(data[0:3]) != BeaconPrefix || data[3] != BeaconVersion {
		return nil
	}
	
	// Extract UUID and port
	var uuid [16]byte
	copy(uuid[:], data[4:20])
	port := binary.BigEndian.Uint16(data[20:22])
	
	// Don't discover ourselves
	if uuid == b.uuid {
		return nil
	}
	
	return &Discovery{
		UUID: uuid,
		Port: port,
		Addr: sourceIP,
		Time: time.Now(),
	}
}