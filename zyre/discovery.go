// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DiscoveryEngine coordinates peer discovery and heartbeat management
type DiscoveryEngine struct {
	// Node information
	nodeUUID   [16]byte
	nodeName   string
	nodePort   uint16
	
	// Components
	beacon        *Beacon
	peerManager   *PeerManager
	groupManager  *GroupManager
	events        *EventChannel
	
	// Channel-based communication
	discoveries   chan *Discovery    // Incoming peer discoveries
	heartbeats    chan *HeartbeatMsg // Heartbeat messages
	commands      chan discoveryCmd  // Control commands
	
	// Configuration
	heartbeatInterval time.Duration   // How often to send heartbeats
	peerTimeout       time.Duration   // When to consider peer expired
	connectionTimeout time.Duration   // TCP connection timeout
	
	// State management
	ctx           context.Context    // Context for cancellation
	cancel        context.CancelFunc // Cancel function
	wg            sync.WaitGroup     // Wait group for goroutines
	mutex         sync.RWMutex       // Protects discovery state
	running       bool               // Whether discovery is running
}

// discoveryCmd represents internal discovery commands
type discoveryCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan error  // Reply channel
}

// HeartbeatMsg represents a heartbeat message
type HeartbeatMsg struct {
	PeerUUID   [16]byte  // Peer UUID
	Timestamp  time.Time // When heartbeat was sent
	Source     string    // Source of heartbeat (beacon, ping, etc.)
}

// DiscoveryConfig holds configuration for the discovery engine
type DiscoveryConfig struct {
	HeartbeatInterval time.Duration // How often to send heartbeats
	PeerTimeout       time.Duration // When to consider peer expired
	ConnectionTimeout time.Duration // TCP connection timeout
	BeaconInterval    time.Duration // Beacon broadcast interval
}

// NewDiscoveryEngine creates a new discovery engine
func NewDiscoveryEngine(nodeUUID [16]byte, nodeName string, nodePort uint16, config *DiscoveryConfig) *DiscoveryEngine {
	if config == nil {
		config = &DiscoveryConfig{
			HeartbeatInterval: DefaultHeartbeatInterval,
			PeerTimeout:       DefaultPeerExpiration,
			ConnectionTimeout: DefaultConnectionTimeout,
			BeaconInterval:    DefaultBeaconInterval,
		}
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create event system
	events := NewEventChannel(1000)
	
	// Create beacon
	beacon, err := NewBeacon(nodeUUID, nodePort, config.BeaconInterval)
	if err != nil {
		cancel()
		return nil
	}
	
	// Create peer manager
	peerManager := NewPeerManager(events)
	
	// Create group manager
	groupManager := NewGroupManager(events)
	
	return &DiscoveryEngine{
		nodeUUID:          nodeUUID,
		nodeName:          nodeName,
		nodePort:          nodePort,
		beacon:            beacon,
		peerManager:       peerManager,
		groupManager:      groupManager,
		events:            events,
		discoveries:       make(chan *Discovery, 500),
		heartbeats:        make(chan *HeartbeatMsg, 1000),
		commands:          make(chan discoveryCmd, 100),
		heartbeatInterval: config.HeartbeatInterval,
		peerTimeout:       config.PeerTimeout,
		connectionTimeout: config.ConnectionTimeout,
		ctx:               ctx,
		cancel:            cancel,
	}
}

// Start begins discovery engine operation
func (de *DiscoveryEngine) Start() error {
	de.mutex.Lock()
	defer de.mutex.Unlock()
	
	if de.running {
		return fmt.Errorf("discovery engine already running")
	}
	
	// Start event system
	de.events.Start()
	
	// Start beacon
	if err := de.beacon.Start(); err != nil {
		return fmt.Errorf("failed to start beacon: %w", err)
	}
	
	// Start peer manager
	de.peerManager.Start()
	
	// Start group manager
	de.groupManager.Start()
	
	// Start internal loops
	de.wg.Add(1)
	go de.discoveryLoop()
	
	de.wg.Add(1)
	go de.heartbeatLoop()
	
	de.wg.Add(1)
	go de.commandLoop()
	
	de.wg.Add(1)
	go de.expirationLoop()
	
	de.running = true
	
	return nil
}

// Stop stops the discovery engine
func (de *DiscoveryEngine) Stop() {
	de.mutex.Lock()
	defer de.mutex.Unlock()
	
	if !de.running {
		return
	}
	
	// Signal shutdown via beacon (port 0)
	de.beacon.SetPort(0)
	time.Sleep(100 * time.Millisecond) // Give time for final beacon
	
	// Cancel context and wait for goroutines
	de.cancel()
	de.wg.Wait()
	
	// Stop components
	de.beacon.Stop()
	de.peerManager.Stop()
	de.groupManager.Stop()
	de.events.Close()
	
	close(de.discoveries)
	close(de.heartbeats)
	close(de.commands)
	
	de.running = false
}

// Events returns the event channel for subscribing to discovery events
func (de *DiscoveryEngine) Events() <-chan *Event {
	return de.events.Subscribe(100)
}

// PeerManager returns the peer manager
func (de *DiscoveryEngine) PeerManager() *PeerManager {
	return de.peerManager
}

// GroupManager returns the group manager
func (de *DiscoveryEngine) GroupManager() *GroupManager {
	return de.groupManager
}

// SendHeartbeat manually sends a heartbeat for a specific peer
func (de *DiscoveryEngine) SendHeartbeat(peerUUID [16]byte, source string) {
	heartbeat := &HeartbeatMsg{
		PeerUUID:  peerUUID,
		Timestamp: time.Now(),
		Source:    source,
	}
	
	select {
	case de.heartbeats <- heartbeat:
		// Heartbeat queued successfully
	default:
		// Channel full, drop heartbeat
	}
}

// discoveryLoop processes beacon discoveries and forwards them to peer manager
func (de *DiscoveryEngine) discoveryLoop() {
	defer de.wg.Done()
	
	beaconDiscoveries := de.beacon.Discoveries()
	peerDiscoveries := de.peerManager.Discoveries()
	
	for {
		select {
		case discovery := <-beaconDiscoveries:
			// Forward beacon discovery to peer manager
			select {
			case peerDiscoveries <- discovery:
				// Also create a heartbeat for this discovery
				de.SendHeartbeat(discovery.UUID, "beacon")
			case <-de.ctx.Done():
				return
			}
			
		case discovery := <-de.discoveries:
			// Handle manual discoveries
			select {
			case peerDiscoveries <- discovery:
				de.SendHeartbeat(discovery.UUID, "manual")
			case <-de.ctx.Done():
				return
			}
			
		case <-de.ctx.Done():
			return
		}
	}
}

// heartbeatLoop processes heartbeat messages and updates peer status
func (de *DiscoveryEngine) heartbeatLoop() {
	defer de.wg.Done()
	
	ticker := time.NewTicker(de.heartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case heartbeat := <-de.heartbeats:
			de.processHeartbeat(heartbeat)
			
		case <-ticker.C:
			de.sendPeriodicHeartbeats()
			
		case <-de.ctx.Done():
			return
		}
	}
}

// commandLoop processes discovery engine commands
func (de *DiscoveryEngine) commandLoop() {
	defer de.wg.Done()
	
	for {
		select {
		case cmd := <-de.commands:
			de.handleCommand(cmd)
		case <-de.ctx.Done():
			return
		}
	}
}

// expirationLoop periodically checks for expired peers
func (de *DiscoveryEngine) expirationLoop() {
	defer de.wg.Done()
	
	ticker := time.NewTicker(de.peerTimeout / 2)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			de.checkExpiredPeers()
		case <-de.ctx.Done():
			return
		}
	}
}

// processHeartbeat processes a received heartbeat
func (de *DiscoveryEngine) processHeartbeat(heartbeat *HeartbeatMsg) {
	// Update peer's last seen time
	peers := de.peerManager.GetPeers()
	peerUUID := fmt.Sprintf("%x", heartbeat.PeerUUID)
	
	if peer, exists := peers[peerUUID]; exists {
		peer.UpdateLastSeen()
	}
}

// sendPeriodicHeartbeats sends heartbeats to all known peers
func (de *DiscoveryEngine) sendPeriodicHeartbeats() {
	peers := de.peerManager.GetPeers()
	
	for _, peer := range peers {
		if peer.Connected {
			// Send PING message to peer
			reply := make(chan error, 1)
			cmd := peerCmd{
				action: "ping",
				reply:  reply,
			}
			
			select {
			case peer.commands <- cmd:
				// Wait for reply with timeout
				select {
				case <-reply:
					// Ping sent successfully
				case <-time.After(1 * time.Second):
					// Timeout waiting for ping reply
				}
			default:
				// Peer command queue full
			}
		}
	}
}

// handleCommand processes a discovery engine command
func (de *DiscoveryEngine) handleCommand(cmd discoveryCmd) {
	switch cmd.action {
	case "discover_peer":
		// Manual peer discovery
		discovery := cmd.data.(*Discovery)
		select {
		case de.discoveries <- discovery:
			cmd.reply <- nil
		default:
			cmd.reply <- fmt.Errorf("discovery queue full")
		}
		
	case "set_beacon_port":
		port := cmd.data.(uint16)
		err := de.beacon.SetPort(port)
		de.nodePort = port
		cmd.reply <- err
		
	case "get_peer_count":
		peers := de.peerManager.GetPeers()
		cmd.reply <- len(peers)
	}
}

// checkExpiredPeers removes peers that haven't been seen recently
func (de *DiscoveryEngine) checkExpiredPeers() {
	peers := de.peerManager.GetPeers()
	expiredPeers := make(map[string]*Peer)
	
	// Find expired peers
	for uuid, peer := range peers {
		if peer.IsExpired(de.peerTimeout) {
			expiredPeers[uuid] = peer
		}
	}
	
	// Remove expired peers from groups
	if len(expiredPeers) > 0 {
		de.groupManager.CleanupExpiredPeers(expiredPeers)
	}
}

// DiscoverPeer manually discovers a peer at a specific address
func (de *DiscoveryEngine) DiscoverPeer(uuid [16]byte, addr string, port uint16) error {
	discovery := &Discovery{
		UUID: uuid,
		Port: port,
		Addr: nil, // Will be resolved from addr
		Time: time.Now(),
	}
	
	reply := make(chan error, 1)
	cmd := discoveryCmd{
		action: "discover_peer",
		data:   discovery,
		reply:  reply,
	}
	
	select {
	case de.commands <- cmd:
		return <-reply
	case <-de.ctx.Done():
		return de.ctx.Err()
	}
}

// SetBeaconPort changes the beacon port (0 = silent mode)
func (de *DiscoveryEngine) SetBeaconPort(port uint16) error {
	reply := make(chan error, 1)
	cmd := discoveryCmd{
		action: "set_beacon_port",
		data:   port,
		reply:  reply,
	}
	
	select {
	case de.commands <- cmd:
		return <-reply
	case <-de.ctx.Done():
		return de.ctx.Err()
	}
}

// GetPeerCount returns the number of known peers
func (de *DiscoveryEngine) GetPeerCount() int {
	reply := make(chan error, 1)
	cmd := discoveryCmd{
		action: "get_peer_count",
		reply:  reply,
	}
	
	select {
	case de.commands <- cmd:
		result := <-reply
		if count, ok := result.(int); ok {
			return count
		}
		return 0
	case <-de.ctx.Done():
		return 0
	}
}

// GetPeerStatistics returns statistics about peer discovery and heartbeats
type PeerStatistics struct {
	TotalPeers      int                    // Total number of peers
	ConnectedPeers  int                    // Number of connected peers
	Groups          map[string]int         // Group name -> member count
	LastHeartbeat   map[string]time.Time   // Peer UUID -> last heartbeat time
	Discovery       DiscoveryStatistics    // Discovery statistics
}

// DiscoveryStatistics holds discovery-related statistics
type DiscoveryStatistics struct {
	BeaconsSent     uint64 // Number of beacons sent
	BeaconsReceived uint64 // Number of beacons received
	PeersDiscovered uint64 // Number of peers discovered
	PeersExpired    uint64 // Number of peers that expired
}

// GetStatistics returns current peer and discovery statistics
func (de *DiscoveryEngine) GetStatistics() *PeerStatistics {
	peers := de.peerManager.GetPeers()
	groups := de.groupManager.ListGroups()
	
	stats := &PeerStatistics{
		TotalPeers:     len(peers),
		ConnectedPeers: 0,
		Groups:         make(map[string]int),
		LastHeartbeat:  make(map[string]time.Time),
		Discovery:      DiscoveryStatistics{}, // Would need to track these in actual implementation
	}
	
	// Count connected peers and collect last seen times
	for uuid, peer := range peers {
		if peer.Connected {
			stats.ConnectedPeers++
		}
		stats.LastHeartbeat[uuid] = peer.LastSeen
	}
	
	// Collect group statistics
	for groupName, membership := range groups {
		stats.Groups[groupName] = len(membership.Members)
	}
	
	return stats
}