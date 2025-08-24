// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/destiny/zmq4/v25"
)

// Node represents a ZRE node with channel-based state management
type Node struct {
	// Node identity
	uuid     [16]byte          // Our UUID
	name     string            // Our name
	headers  map[string]string // Our headers
	groups   map[string]bool   // Groups we belong to
	port     uint16            // Our mailbox port
	endpoint string            // Our TCP endpoint
	
	// Network components
	beacon         *Beacon           // UDP beacon for discovery
	peerManager    *PeerManager      // Peer management
	socket         *SecureSocket     // Secure ZMQ ROUTER socket for incoming connections
	securityMgr    *SecurityManager  // Security manager
	
	// Channel-based communication
	events     *EventChannel   // Event system
	commands   chan nodeCmd    // Node control commands
	incoming   chan *Message   // Incoming ZRE messages
	
	// State management
	state      int             // Node state
	sequence   uint16          // Message sequence counter
	ctx        context.Context // Context for cancellation
	cancel     context.CancelFunc // Cancel function
	wg         sync.WaitGroup  // Wait group for goroutines
	mutex      sync.RWMutex    // Protects node state
}

// nodeCmd represents internal node commands
type nodeCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// NodeConfig holds configuration for creating a node
type NodeConfig struct {
	Name     string            // Node name
	Headers  map[string]string // Node headers
	Port     uint16            // Preferred port (0 for auto)
	Interval time.Duration     // Beacon interval
	Security *SecurityConfig   // Security configuration
}

// NewNode creates a new ZRE node
func NewNode(config *NodeConfig) (*Node, error) {
	// Generate random UUID
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}
	
	// Set defaults
	if config == nil {
		config = &NodeConfig{}
	}
	if config.Name == "" {
		config.Name = fmt.Sprintf("node-%x", uuid[:4])
	}
	if config.Headers == nil {
		config.Headers = make(map[string]string)
	}
	if config.Interval == 0 {
		config.Interval = DefaultBeaconInterval
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create event system
	events := NewEventChannel(1000)
	
	// Create security manager
	securityMgr, err := NewSecurityManager(config.Security)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create security manager: %w", err)
	}
	
	// Create peer manager
	peerManager := NewPeerManager(events)
	
	// Create beacon
	beacon, err := NewBeacon(uuid, config.Port, config.Interval)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create beacon: %w", err)
	}
	
	node := &Node{
		uuid:        uuid,
		name:        config.Name,
		headers:     config.Headers,
		groups:      make(map[string]bool),
		port:        config.Port,
		beacon:      beacon,
		peerManager: peerManager,
		securityMgr: securityMgr,
		events:      events,
		commands:    make(chan nodeCmd, 100),
		incoming:    make(chan *Message, 1000),
		state:       NodeStateStopped,
		ctx:         ctx,
		cancel:      cancel,
	}
	
	return node, nil
}

// Start starts the ZRE node
func (n *Node) Start() error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	
	if n.state != NodeStateStopped {
		return fmt.Errorf("node already started")
	}
	
	n.state = NodeStateStarting
	
	// Create secure ZMQ ROUTER socket
	secureSocket, err := n.securityMgr.SecureSocket(zmq4.ROUTER, n.ctx)
	if err != nil {
		return fmt.Errorf("failed to create secure socket: %w", err)
	}
	n.socket = secureSocket
	n.socket.SetOption(zmq4.OptionIdentity, fmt.Sprintf("%x", n.uuid))
	
	// Find available port if needed
	if n.port == 0 {
		if port, err := n.findAvailablePort(); err != nil {
			return fmt.Errorf("failed to find available port: %w", err)
		} else {
			n.port = port
		}
	}
	
	// Bind to endpoint
	n.endpoint = fmt.Sprintf("tcp://*:%d", n.port)
	if err := n.socket.Listen(n.endpoint); err != nil {
		return fmt.Errorf("failed to bind socket: %w", err)
	}
	
	// Update beacon with our port
	n.beacon.SetPort(n.port)
	
	// Start event system
	n.events.Start()
	
	// Start beacon
	if err := n.beacon.Start(); err != nil {
		return fmt.Errorf("failed to start beacon: %w", err)
	}
	
	// Start peer manager
	n.peerManager.Start()
	
	// Start internal loops
	n.wg.Add(1)
	go n.commandLoop()
	
	n.wg.Add(1)
	go n.incomingLoop()
	
	n.wg.Add(1)
	go n.discoveryLoop()
	
	n.wg.Add(1)
	go n.heartbeatLoop()
	
	n.state = NodeStateRunning
	
	return nil
}

// Stop stops the ZRE node
func (n *Node) Stop() {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	
	if n.state == NodeStateStopped {
		return
	}
	
	n.state = NodeStateStopping
	
	// Set beacon to silent mode (port 0) to signal departure
	n.beacon.SetPort(0)
	time.Sleep(100 * time.Millisecond) // Give time for final beacon
	
	// Cancel context and wait for goroutines
	n.cancel()
	n.wg.Wait()
	
	// Stop components
	n.beacon.Stop()
	n.peerManager.Stop()
	n.events.Close()
	
	if n.socket != nil {
		n.socket.Close()
	}
	
	close(n.commands)
	close(n.incoming)
	
	n.state = NodeStateStopped
}

// Events returns the event channel for subscribing to network events
func (n *Node) Events() <-chan *Event {
	return n.events.Subscribe(100)
}

// UUID returns the node's UUID as a string
func (n *Node) UUID() string {
	return fmt.Sprintf("%x", n.uuid)
}

// Name returns the node's name
func (n *Node) Name() string {
	n.mutex.RLock()
	defer n.mutex.RUnlock()
	return n.name
}

// SetName sets the node's name
func (n *Node) SetName(name string) {
	reply := make(chan interface{}, 1)
	cmd := nodeCmd{
		action: "set_name",
		data:   name,
		reply:  reply,
	}
	
	select {
	case n.commands <- cmd:
		<-reply
	case <-n.ctx.Done():
	}
}

// SetHeader sets a header value
func (n *Node) SetHeader(key, value string) {
	reply := make(chan interface{}, 1)
	cmd := nodeCmd{
		action: "set_header",
		data:   map[string]string{key: value},
		reply:  reply,
	}
	
	select {
	case n.commands <- cmd:
		<-reply
	case <-n.ctx.Done():
	}
}

// Join joins a group
func (n *Node) Join(group string) error {
	reply := make(chan interface{}, 1)
	cmd := nodeCmd{
		action: "join",
		data:   group,
		reply:  reply,
	}
	
	select {
	case n.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-n.ctx.Done():
		return n.ctx.Err()
	}
}

// Leave leaves a group
func (n *Node) Leave(group string) error {
	reply := make(chan interface{}, 1)
	cmd := nodeCmd{
		action: "leave",
		data:   group,
		reply:  reply,
	}
	
	select {
	case n.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-n.ctx.Done():
		return n.ctx.Err()
	}
}

// Whisper sends a direct message to a specific peer
func (n *Node) Whisper(peerUUID string, message [][]byte) error {
	reply := make(chan interface{}, 1)
	cmd := nodeCmd{
		action: "whisper",
		data: map[string]interface{}{
			"peer":    peerUUID,
			"message": message,
		},
		reply: reply,
	}
	
	select {
	case n.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-n.ctx.Done():
		return n.ctx.Err()
	}
}

// Shout sends a message to all peers in a group
func (n *Node) Shout(group string, message [][]byte) error {
	reply := make(chan interface{}, 1)
	cmd := nodeCmd{
		action: "shout",
		data: map[string]interface{}{
			"group":   group,
			"message": message,
		},
		reply: reply,
	}
	
	select {
	case n.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-n.ctx.Done():
		return n.ctx.Err()
	}
}

// Peers returns information about known peers
func (n *Node) Peers() map[string]*Peer {
	return n.peerManager.GetPeers()
}

// Security returns the security manager
func (n *Node) Security() *SecurityManager {
	return n.securityMgr
}

// SetSecurity updates the security configuration
func (n *Node) SetSecurity(config *SecurityConfig) error {
	newSecurityMgr, err := NewSecurityManager(config)
	if err != nil {
		return fmt.Errorf("failed to create new security manager: %w", err)
	}
	
	n.mutex.Lock()
	oldSecurityMgr := n.securityMgr
	n.securityMgr = newSecurityMgr
	n.mutex.Unlock()
	
	// Clean up old security manager
	if oldSecurityMgr != nil {
		oldSecurityMgr.Close()
	}
	
	return nil
}

// Internal methods

// commandLoop processes node commands
func (n *Node) commandLoop() {
	defer n.wg.Done()
	
	for {
		select {
		case cmd := <-n.commands:
			n.handleCommand(cmd)
		case <-n.ctx.Done():
			return
		}
	}
}

// incomingLoop processes incoming ZRE messages
func (n *Node) incomingLoop() {
	defer n.wg.Done()
	
	for {
		select {
		case <-n.ctx.Done():
			return
		default:
			// Receive message from socket
			msg, err := n.socket.Recv()
			if err != nil {
				continue
			}
			
			// Parse ZRE message
			var zreMsg Message
			if err := zreMsg.Unmarshal(msg.Bytes()); err != nil {
				continue
			}
			
			n.handleIncomingMessage(&zreMsg)
		}
	}
}

// discoveryLoop processes peer discoveries from beacon
func (n *Node) discoveryLoop() {
	defer n.wg.Done()
	
	discoveries := n.beacon.Discoveries()
	peerDiscoveries := n.peerManager.Discoveries()
	
	for {
		select {
		case discovery := <-discoveries:
			// Forward discovery to peer manager
			select {
			case peerDiscoveries <- discovery:
			case <-n.ctx.Done():
				return
			}
		case <-n.ctx.Done():
			return
		}
	}
}

// heartbeatLoop sends periodic heartbeats
func (n *Node) heartbeatLoop() {
	defer n.wg.Done()
	
	ticker := time.NewTicker(DefaultHeartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			n.sendHeartbeats()
		case <-n.ctx.Done():
			return
		}
	}
}

// handleCommand processes a node command
func (n *Node) handleCommand(cmd nodeCmd) {
	switch cmd.action {
	case "set_name":
		n.mutex.Lock()
		n.name = cmd.data.(string)
		n.mutex.Unlock()
		cmd.reply <- nil
		
	case "set_header":
		n.mutex.Lock()
		header := cmd.data.(map[string]string)
		for k, v := range header {
			n.headers[k] = v
		}
		n.mutex.Unlock()
		cmd.reply <- nil
		
	case "join":
		group := cmd.data.(string)
		n.mutex.Lock()
		n.groups[group] = true
		n.mutex.Unlock()
		
		// Send JOIN message to all peers
		n.broadcastJoin(group)
		cmd.reply <- nil
		
	case "leave":
		group := cmd.data.(string)
		n.mutex.Lock()
		delete(n.groups, group)
		n.mutex.Unlock()
		
		// Send LEAVE message to all peers
		n.broadcastLeave(group)
		cmd.reply <- nil
		
	case "whisper":
		data := cmd.data.(map[string]interface{})
		peerUUID := data["peer"].(string)
		message := data["message"].([][]byte)
		err := n.sendWhisper(peerUUID, message)
		cmd.reply <- err
		
	case "shout":
		data := cmd.data.(map[string]interface{})
		group := data["group"].(string)
		message := data["message"].([][]byte)
		err := n.sendShout(group, message)
		cmd.reply <- err
	}
}

// handleIncomingMessage processes an incoming ZRE message
func (n *Node) handleIncomingMessage(msg *Message) {
	switch msg.Type {
	case MessageTypeHello:
		n.handleHello(msg)
	case MessageTypeWhisper:
		n.handleWhisper(msg)
	case MessageTypeShout:
		n.handleShout(msg)
	case MessageTypeJoin:
		n.handleJoin(msg)
	case MessageTypeLeave:
		n.handleLeave(msg)
	case MessageTypePing:
		n.handlePing(msg)
	case MessageTypePingOK:
		n.handlePingOK(msg)
	}
}

// Helper methods for message handling
func (n *Node) handleHello(msg *Message) {
	// Parse HELLO message and update peer information
	var hello HelloMessage
	if err := hello.Unmarshal(msg.Body); err != nil {
		return
	}
	
	// Update peer information in peer manager
	// Implementation details depend on peer manager interface
}

func (n *Node) handleWhisper(msg *Message) {
	var whisper WhisperMessage
	if err := whisper.Unmarshal(msg.Body); err != nil {
		return
	}
	
	// Create and publish WHISPER event
	peerUUID := fmt.Sprintf("%x", msg.SenderUUID)
	event := NewWhisperEvent(peerUUID, "", whisper.Content)
	n.events.Publish(event)
}

func (n *Node) handleShout(msg *Message) {
	var shout ShoutMessage
	if err := shout.Unmarshal(msg.Body); err != nil {
		return
	}
	
	// Create and publish SHOUT event
	peerUUID := fmt.Sprintf("%x", msg.SenderUUID)
	event := NewShoutEvent(peerUUID, "", shout.Group, shout.Content)
	n.events.Publish(event)
}

func (n *Node) handleJoin(msg *Message) {
	var join JoinMessage
	if err := join.Unmarshal(msg.Body); err != nil {
		return
	}
	
	// Update peer group membership and publish event
	peerUUID := fmt.Sprintf("%x", msg.SenderUUID)
	event := NewJoinEvent(peerUUID, "", join.Group)
	n.events.Publish(event)
}

func (n *Node) handleLeave(msg *Message) {
	var leave LeaveMessage
	if err := leave.Unmarshal(msg.Body); err != nil {
		return
	}
	
	// Update peer group membership and publish event
	peerUUID := fmt.Sprintf("%x", msg.SenderUUID)
	event := NewLeaveEvent(peerUUID, "", leave.Group)
	n.events.Publish(event)
}

func (n *Node) handlePing(msg *Message) {
	// Respond with PING-OK
	response := &Message{
		Version:    ProtocolVersion,
		Type:       MessageTypePingOK,
		Sequence:   n.nextSequence(),
		SenderUUID: n.uuid,
	}
	
	pingOK := &PingOKMessage{}
	body, _ := pingOK.Marshal()
	response.Body = body
	
	// Send response (implementation depends on routing)
}

func (n *Node) handlePingOK(msg *Message) {
	// Update peer last seen time
	peerUUID := fmt.Sprintf("%x", msg.SenderUUID)
	// Implementation depends on peer manager interface
	_ = peerUUID
}

func (n *Node) broadcastJoin(group string) {
	// Implementation for broadcasting JOIN message
}

func (n *Node) broadcastLeave(group string) {
	// Implementation for broadcasting LEAVE message
}

func (n *Node) sendWhisper(peerUUID string, message [][]byte) error {
	// Implementation for sending WHISPER message
	return nil
}

func (n *Node) sendShout(group string, message [][]byte) error {
	// Implementation for sending SHOUT message
	return nil
}

func (n *Node) sendHeartbeats() {
	// Implementation for sending periodic heartbeats
}

func (n *Node) nextSequence() uint16 {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.sequence++
	return n.sequence
}

func (n *Node) findAvailablePort() (uint16, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	
	addr := listener.Addr().(*net.TCPAddr)
	return uint16(addr.Port), nil
}