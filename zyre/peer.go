// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/destiny/zmq4/v25"
)

// Peer represents a remote ZRE peer
type Peer struct {
	UUID        [16]byte          // Peer UUID
	Name        string            // Peer name
	Endpoint    string            // TCP endpoint for communication
	Headers     map[string]string // Peer headers
	Groups      map[string]bool   // Groups this peer belongs to
	Status      int               // Peer status
	LastSeen    time.Time         // When peer was last seen
	Connected   bool              // Whether we have a connection
	Socket      zmq4.Socket       // ZMQ socket for communication
	Sequence    uint16            // Message sequence number
	
	// Channel-based communication
	outgoing    chan *Message     // Outgoing messages to this peer
	commands    chan peerCmd      // Control commands
	
	// State management
	ctx         context.Context   // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup    // Wait group for goroutines
	mutex       sync.RWMutex      // Protects peer state
}

// peerCmd represents internal peer commands
type peerCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan error  // Reply channel
}

// PeerManager manages a collection of peers with channel-based coordination
type PeerManager struct {
	peers       map[string]*Peer   // Map of UUID to peer
	discoveries chan *Discovery    // Channel for peer discoveries
	commands    chan managerCmd    // Channel for manager commands
	events      *EventChannel      // Event channel for notifications
	
	// Configuration
	expiration  time.Duration      // Peer expiration timeout
	
	// State management
	ctx         context.Context    // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup     // Wait group for goroutines
	mutex       sync.RWMutex       // Protects manager state
}

// managerCmd represents internal manager commands
type managerCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// NewPeer creates a new peer instance
func NewPeer(uuid [16]byte, endpoint string) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Peer{
		UUID:      uuid,
		Endpoint:  endpoint,
		Headers:   make(map[string]string),
		Groups:    make(map[string]bool),
		LastSeen:  time.Now(),
		outgoing:  make(chan *Message, 100), // Buffer for outgoing messages
		commands:  make(chan peerCmd, 10),   // Buffer for commands
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start begins peer operation
func (p *Peer) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if p.Connected {
		return fmt.Errorf("peer already started")
	}
	
	// Create ZMQ DEALER socket for communication
	socket := zmq4.NewDealer(p.ctx)
	socket.SetOption(zmq4.OptionIdentity, fmt.Sprintf("%x", p.UUID))
	
	// Connect to peer's endpoint
	if err := socket.Dial(p.Endpoint); err != nil {
		return fmt.Errorf("failed to connect to peer %x: %w", p.UUID, err)
	}
	
	p.Socket = socket
	p.Connected = true
	
	// Start outgoing message handler
	p.wg.Add(1)
	go p.outgoingLoop()
	
	// Start command processing
	p.wg.Add(1)
	go p.commandLoop()
	
	return nil
}

// Stop stops the peer and closes connections
func (p *Peer) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if !p.Connected {
		return
	}
	
	p.cancel()      // Cancel context
	p.wg.Wait()     // Wait for goroutines
	p.Connected = false
	
	if p.Socket != nil {
		p.Socket.Close()
	}
	
	close(p.outgoing)
	close(p.commands)
}

// Send queues a message to be sent to this peer
func (p *Peer) Send(msg *Message) error {
	select {
	case p.outgoing <- msg:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		return fmt.Errorf("peer message queue full")
	}
}

// UpdateLastSeen updates the peer's last seen timestamp
func (p *Peer) UpdateLastSeen() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.LastSeen = time.Now()
}

// IsExpired checks if the peer has expired
func (p *Peer) IsExpired(timeout time.Duration) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return time.Since(p.LastSeen) > timeout
}

// JoinGroup adds the peer to a group
func (p *Peer) JoinGroup(group string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.Groups[group] = true
}

// LeaveGroup removes the peer from a group
func (p *Peer) LeaveGroup(group string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.Groups, group)
}

// InGroup checks if peer is in a specific group
func (p *Peer) InGroup(group string) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.Groups[group]
}

// outgoingLoop handles sending messages to the peer
func (p *Peer) outgoingLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case msg := <-p.outgoing:
			if err := p.sendMessage(msg); err != nil {
				// Log error or handle send failure
				continue
			}
		case <-p.ctx.Done():
			return
		}
	}
}

// commandLoop processes peer commands
func (p *Peer) commandLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case cmd := <-p.commands:
			switch cmd.action {
			case "ping":
				err := p.sendPing()
				cmd.reply <- err
			}
		case <-p.ctx.Done():
			return
		}
	}
}

// sendMessage sends a message to the peer via ZMQ socket
func (p *Peer) sendMessage(msg *Message) error {
	data, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	
	zmqMsg := zmq4.NewMsgFromBytes(data)
	return p.Socket.SendMulti(zmqMsg)
}

// sendPing sends a PING message to the peer
func (p *Peer) sendPing() error {
	msg := &Message{
		Version:    ProtocolVersion,
		Type:       MessageTypePing,
		Sequence:   p.nextSequence(),
		SenderUUID: [16]byte{}, // Will be filled by sender
	}
	
	ping := &PingMessage{}
	body, err := ping.Marshal()
	if err != nil {
		return err
	}
	msg.Body = body
	
	return p.Send(msg)
}

// nextSequence returns the next sequence number
func (p *Peer) nextSequence() uint16 {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.Sequence++
	return p.Sequence
}

// NewPeerManager creates a new peer manager
func NewPeerManager(events *EventChannel) *PeerManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &PeerManager{
		peers:       make(map[string]*Peer),
		discoveries: make(chan *Discovery, 100),
		commands:    make(chan managerCmd, 50),
		events:      events,
		expiration:  DefaultPeerExpiration,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins peer manager operation
func (pm *PeerManager) Start() {
	// Start discovery processing
	pm.wg.Add(1)
	go pm.discoveryLoop()
	
	// Start command processing
	pm.wg.Add(1)
	go pm.commandLoop()
	
	// Start expiration checking
	pm.wg.Add(1)
	go pm.expirationLoop()
}

// Stop stops the peer manager
func (pm *PeerManager) Stop() {
	pm.cancel()
	pm.wg.Wait()
	
	// Stop all peers
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	for _, peer := range pm.peers {
		peer.Stop()
	}
	
	close(pm.discoveries)
	close(pm.commands)
}

// Discoveries returns the channel for receiving peer discoveries
func (pm *PeerManager) Discoveries() chan<- *Discovery {
	return pm.discoveries
}

// GetPeers returns a copy of all current peers
func (pm *PeerManager) GetPeers() map[string]*Peer {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	peers := make(map[string]*Peer)
	for uuid, peer := range pm.peers {
		peers[uuid] = peer
	}
	return peers
}

// discoveryLoop processes peer discoveries
func (pm *PeerManager) discoveryLoop() {
	defer pm.wg.Done()
	
	for {
		select {
		case discovery := <-pm.discoveries:
			pm.handleDiscovery(discovery)
		case <-pm.ctx.Done():
			return
		}
	}
}

// commandLoop processes manager commands
func (pm *PeerManager) commandLoop() {
	defer pm.wg.Done()
	
	for {
		select {
		case cmd := <-pm.commands:
			switch cmd.action {
			case "get_peer":
				uuid := cmd.data.(string)
				pm.mutex.RLock()
				peer := pm.peers[uuid]
				pm.mutex.RUnlock()
				cmd.reply <- peer
			}
		case <-pm.ctx.Done():
			return
		}
	}
}

// expirationLoop periodically checks for expired peers
func (pm *PeerManager) expirationLoop() {
	defer pm.wg.Done()
	
	ticker := time.NewTicker(pm.expiration / 2)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			pm.checkExpiredPeers()
		case <-pm.ctx.Done():
			return
		}
	}
}

// handleDiscovery processes a peer discovery
func (pm *PeerManager) handleDiscovery(discovery *Discovery) {
	uuidStr := fmt.Sprintf("%x", discovery.UUID)
	
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	if discovery.Port == 0 {
		// Peer is leaving
		if peer, exists := pm.peers[uuidStr]; exists {
			peer.Stop()
			delete(pm.peers, uuidStr)
			
			// Send EXIT event
			event := NewExitEvent(uuidStr, peer.Name, peer.Endpoint)
			pm.events.Publish(event)
		}
		return
	}
	
	// Check if we already know this peer
	if peer, exists := pm.peers[uuidStr]; exists {
		peer.UpdateLastSeen()
		return
	}
	
	// Create new peer
	endpoint := fmt.Sprintf("tcp://%s:%d", discovery.Addr, discovery.Port)
	peer := NewPeer(discovery.UUID, endpoint)
	
	if err := peer.Start(); err != nil {
		return // Failed to start peer
	}
	
	pm.peers[uuidStr] = peer
	
	// Send ENTER event
	event := NewEnterEvent(uuidStr, peer.Name, peer.Endpoint, peer.Headers)
	pm.events.Publish(event)
}

// checkExpiredPeers removes peers that haven't been seen recently
func (pm *PeerManager) checkExpiredPeers() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	for uuid, peer := range pm.peers {
		if peer.IsExpired(pm.expiration) {
			peer.Stop()
			delete(pm.peers, uuid)
			
			// Send EXIT event
			event := NewExitEvent(uuid, peer.Name, peer.Endpoint)
			pm.events.Publish(event)
		}
	}
}