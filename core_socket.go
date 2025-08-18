// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zmq4

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultRetry      = 250 * time.Millisecond
	defaultTimeout    = 5 * time.Minute
	defaultMaxRetries = 10
)

var (
	errInvalidAddress = errors.New("zmq4: invalid address")

	ErrBadProperty = errors.New("zmq4: bad property")
)

// ZMTP 3.1 Heartbeat message types following Majordomo pattern
type heartbeatUpdateType int

const (
	heartbeatConnected heartbeatUpdateType = iota
	heartbeatDisconnected
	heartbeatPingSent
	heartbeatPongReceived
	heartbeatTimeout
	heartbeatActivityDetected
)

// heartbeatUpdate represents a heartbeat state change message
type heartbeatUpdate struct {
	updateType heartbeatUpdateType
	connID     string      // connection identifier
	timestamp  time.Time   // event timestamp
	data       interface{} // additional data (TTL, context, etc.)
}

// heartbeatQueryType represents different types of heartbeat queries
type heartbeatQueryType int

const (
	queryHeartbeatConnections heartbeatQueryType = iota
	queryHeartbeatStats
	queryConnectionLiveness
)

// heartbeatQuery represents a request for heartbeat state
type heartbeatQuery struct {
	queryType  heartbeatQueryType
	connID     string // specific connection (optional)
	responseCh chan interface{}
}

// socket implements the ZeroMQ socket interface
type socket struct {
	ep            string // socket end-point
	typ           SocketType
	id            SocketIdentity
	retry         time.Duration
	maxRetries    int
	sec           Security
	log           *log.Logger
	subTopics     func() []string
	autoReconnect bool
	timeout       time.Duration

	mu    sync.RWMutex
	conns []*Conn // ZMTP connections
	r     rpool
	w     wpool

	props map[string]interface{} // properties of this socket

	ctx      context.Context // life-line of socket
	cancel   context.CancelFunc
	listener net.Listener
	dialer   net.Dialer

	closedConns   []*Conn
	reaperCond    *sync.Cond
	reaperStarted bool
	
	// ZMTP 3.1 Heartbeat state (RFC 37) - Channel-based like Majordomo
	heartbeatIVL     time.Duration // Interval between PING commands
	heartbeatTTL     time.Duration // Timeout for remote peer
	heartbeatTimeout time.Duration // Local timeout after sending PING
	
	// Heartbeat channels following Majordomo pattern
	heartbeatUpdateCh chan heartbeatUpdate
	heartbeatQueryCh  chan heartbeatQuery
	heartbeatDone     chan struct{}
	heartbeatStarted  bool
}

func newDefaultSocket(ctx context.Context, sockType SocketType) *socket {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	return &socket{
		typ:        sockType,
		retry:      defaultRetry,
		maxRetries: defaultMaxRetries,
		timeout:    defaultTimeout,
		sec:        nullSecurity{},
		conns:      nil,
		r:          newQReader(ctx),
		w:          newMWriter(ctx),
		props:      make(map[string]interface{}),
		ctx:        ctx,
		cancel:           cancel,
		dialer:           net.Dialer{Timeout: defaultTimeout},
		reaperCond:       sync.NewCond(&sync.Mutex{}),
		heartbeatUpdateCh: make(chan heartbeatUpdate, 10),
		heartbeatQueryCh:  make(chan heartbeatQuery, 5),
		heartbeatDone:     make(chan struct{}),
	}
}

func newSocket(ctx context.Context, sockType SocketType, opts ...Option) *socket {
	sck := newDefaultSocket(ctx, sockType)
	for _, opt := range opts {
		opt(sck)
	}
	if len(sck.id) == 0 {
		sck.id = SocketIdentity(newUUID())
	}
	if sck.log == nil {
		sck.log = log.New(os.Stderr, "zmq4: ", 0)
	}

	return sck
}

func (sck *socket) topics() []string {
	var (
		keys   = make(map[string]struct{})
		topics []string
	)
	sck.mu.RLock()
	for _, con := range sck.conns {
		con.mu.RLock()
		for topic := range con.topics {
			if _, dup := keys[topic]; dup {
				continue
			}
			keys[topic] = struct{}{}
			topics = append(topics, topic)
		}
		con.mu.RUnlock()
	}
	sck.mu.RUnlock()
	sort.Strings(topics)
	return topics
}

// Close closes the open Socket
func (sck *socket) Close() error {
	// The Lock around Signal ensures the connReaper is running
	// and is in sck.reaperCond.Wait()
	sck.reaperCond.L.Lock()
	sck.cancel()
	sck.reaperCond.Signal()
	sck.reaperCond.L.Unlock()

	if sck.listener != nil {
		defer sck.listener.Close()
	}

	sck.mu.RLock()
	defer sck.mu.RUnlock()

	var err error
	for _, conn := range sck.conns {
		e := conn.Close()
		if e != nil && err == nil {
			err = e
		}
	}

	// Remove the unix socket file if created by net.Listen
	if sck.listener != nil && strings.HasPrefix(sck.ep, "ipc://") {
		os.Remove(sck.ep[len("ipc://"):])
	}

	return err
}

// Send puts the message on the outbound send queue.
// Send blocks until the message can be queued or the send deadline expires.
func (sck *socket) Send(msg Msg) error {
	ctx, cancel := context.WithTimeout(sck.ctx, sck.Timeout())
	defer cancel()
	return sck.w.write(ctx, msg)
}

// SendMulti puts the message on the outbound send queue.
// SendMulti blocks until the message can be queued or the send deadline expires.
// The message will be sent as a multipart message.
func (sck *socket) SendMulti(msg Msg) error {
	msg.multipart = true
	ctx, cancel := context.WithTimeout(sck.ctx, sck.Timeout())
	defer cancel()
	return sck.w.write(ctx, msg)
}

// Recv receives a complete message.
func (sck *socket) Recv() (Msg, error) {
	ctx, cancel := context.WithCancel(sck.ctx)
	defer cancel()
	var msg Msg
	err := sck.r.read(ctx, &msg)
	return msg, err
}

// Listen connects a local endpoint to the Socket.
func (sck *socket) Listen(endpoint string) error {
	sck.ep = endpoint
	network, addr, err := splitAddr(endpoint)
	if err != nil {
		return err
	}

	trans, ok := drivers.get(network)
	if !ok {
		return UnknownTransportError{Name: network}
	}

	l, err := trans.Listen(sck.ctx, addr)
	if err != nil {
		return fmt.Errorf("zmq4: could not listen to %q: %w", endpoint, err)
	}
	sck.listener = l

	go sck.accept()
	if !sck.reaperStarted {
		sck.reaperCond.L.Lock()
		go sck.connReaper()
		sck.reaperStarted = true
	}

	return nil
}

func (sck *socket) accept() {
	ctx, cancel := context.WithCancel(sck.ctx)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := sck.listener.Accept()
			if err != nil {
				// FIXME(sbinet): maybe bubble up this error to application code?
				//sck.log.Printf("error accepting connection from %q: %+v", sck.ep, err)
				continue
			}

			zconn, err := Open(conn, sck.sec, sck.typ, sck.id, true, sck.scheduleRmConn)
			if err != nil {
				// FIXME(sbinet): maybe bubble up this error to application code?
				sck.log.Printf("could not open a ZMTP connection with %q: %+v", sck.ep, err)
				continue
			}

			sck.addConn(zconn)
		}
	}
}

// Dial connects a remote endpoint to the Socket.
func (sck *socket) Dial(endpoint string) error {
	sck.ep = endpoint

	network, addr, err := splitAddr(endpoint)
	if err != nil {
		return err
	}

	var (
		conn      net.Conn
		trans, ok = drivers.get(network)
		retries   = 0
	)
	if !ok {
		return UnknownTransportError{Name: network}
	}

connect:
	conn, err = trans.Dial(sck.ctx, &sck.dialer, addr)
	if err != nil {
		// retry if retry count is lower than maximum retry count and context has not been canceled
		if (sck.maxRetries == -1 || retries < sck.maxRetries) && sck.ctx.Err() == nil {
			retries++
			time.Sleep(sck.retry)
			goto connect
		}
		return fmt.Errorf("zmq4: could not dial to %q (retry=%v): %w", endpoint, sck.retry, err)
	}

	if conn == nil {
		return fmt.Errorf("zmq4: got a nil dial-conn to %q", endpoint)
	}

	zconn, err := Open(conn, sck.sec, sck.typ, sck.id, false, sck.scheduleRmConn)
	if err != nil {
		return fmt.Errorf("zmq4: could not open a ZMTP connection: %w", err)
	}
	if zconn == nil {
		return fmt.Errorf("zmq4: got a nil ZMTP connection to %q", endpoint)
	}

	if !sck.reaperStarted {
		sck.reaperCond.L.Lock()
		go sck.connReaper()
		sck.reaperStarted = true
	}
	sck.addConn(zconn)
	return nil
}

func (sck *socket) addConn(c *Conn) {
	sck.mu.Lock()
	defer sck.mu.Unlock()
	
	// Set up heartbeat callback for this connection
	c.heartbeatNotifyCB = sck.heartbeatCallback
	
	sck.conns = append(sck.conns, c)
	
	// Notify heartbeat state manager of new connection
	connID := sck.getConnID(c)
	sck.updateHeartbeatState(heartbeatConnected, connID, time.Now(), nil)
	if len(c.Peer.Meta[sysSockID]) == 0 {
		switch c.typ {
		case Router: // STREAM type not yet implemented
			// if empty Identity metadata is received from some client
			// need to assign an uuid such that router socket can reply to the correct client
			c.Peer.Meta[sysSockID] = newUUID()
		}
	}
	if sck.w != nil {
		sck.w.addConn(c)
	}
	if sck.r != nil {
		sck.r.addConn(c)
	}
	// Send subscriptions immediately to new connection if this is a SUB socket
	if sck.subTopics != nil {
		for _, topic := range sck.subTopics() {
			// Send subscription message directly to the new connection
			// This ensures immediate subscription after handshake per RFC requirements
			subscribeMsg := NewMsg(append([]byte{1}, topic...))
			_ = c.SendMsg(subscribeMsg)
		}
	}
}

func (sck *socket) rmConn(c *Conn) {
	sck.mu.Lock()
	defer sck.mu.Unlock()

	cur := -1
	for i := range sck.conns {
		if sck.conns[i] == c {
			cur = i
			break
		}
	}

	if cur == -1 {
		return
	}

	// Notify heartbeat state manager of disconnection
	connID := sck.getConnID(c)
	sck.updateHeartbeatState(heartbeatDisconnected, connID, time.Now(), nil)
	
	sck.conns = append(sck.conns[:cur], sck.conns[cur+1:]...)
	if sck.r != nil {
		sck.r.rmConn(c)
	}
	if sck.w != nil {
		sck.w.rmConn(c)
	}
}

func (sck *socket) scheduleRmConn(c *Conn) {
	sck.reaperCond.L.Lock()
	sck.closedConns = append(sck.closedConns, c)
	sck.reaperCond.Signal()
	sck.reaperCond.L.Unlock()

	if sck.autoReconnect {
		sck.Dial(sck.ep)
	}
}

// Type returns the type of this Socket (PUB, SUB, ...)
func (sck *socket) Type() SocketType {
	return sck.typ
}

// Addr returns the listener's address.
// Addr returns nil if the socket isn't a listener.
func (sck *socket) Addr() net.Addr {
	if sck.listener == nil {
		return nil
	}
	return sck.listener.Addr()
}

// GetOption is used to retrieve an option for a socket.
func (sck *socket) GetOption(name string) (interface{}, error) {
	v, ok := sck.props[name]
	if !ok {
		return nil, ErrBadProperty
	}
	return v, nil
}

// SetOption is used to set an option for a socket.
func (sck *socket) SetOption(name string, value interface{}) error {
	// Handle heartbeat options per RFC 37/ZMTP 3.1
	switch name {
	case OptionHeartbeatIVL:
		if duration, ok := value.(time.Duration); ok {
			sck.heartbeatIVL = duration
			if duration > 0 && !sck.heartbeatStarted {
				go sck.heartbeatStateManager()
				sck.heartbeatStarted = true
			}
		}
	case OptionHeartbeatTTL:
		if duration, ok := value.(time.Duration); ok {
			// TTL is specified in deciseconds with max 6553.5 seconds per RFC 37
			const maxTTL = time.Duration(65535) * 100 * time.Millisecond
			if duration > maxTTL {
				return fmt.Errorf("zmq4: heartbeat TTL exceeds maximum of %v", maxTTL)
			}
			sck.heartbeatTTL = duration
		}
	case OptionHeartbeatTimeout:
		if duration, ok := value.(time.Duration); ok {
			sck.heartbeatTimeout = duration
		}
	}
	
	// Store the property for retrieval
	sck.props[name] = value
	return nil
}

func (sck *socket) Timeout() time.Duration {
	return sck.timeout
}

func (sck *socket) connReaper() {
	// We are not locking here sck.reaperCond.L.Lock()
	// as it should be locked prior starting connReaper as goroutine
	// That would ensure that sck.reaperCond.Signal()
	// would be delivered only when reaper goroutine is really started
	// and is in sck.reaperCond.Wait()
	defer sck.reaperCond.L.Unlock()

	for {
		for len(sck.closedConns) == 0 && sck.ctx.Err() == nil {
			sck.reaperCond.Wait()
		}

		if sck.ctx.Err() != nil {
			return
		}

		// Clone the known closed connections to avoid data race
		// and remove those under reaper unlocked.
		// That should fix the deadlock reported in #149.
		cc := append([]*Conn{}, sck.closedConns...) // clone
		sck.closedConns = sck.closedConns[:0]
		sck.reaperCond.L.Unlock()
		for _, c := range cc {
			sck.rmConn(c)
		}
		sck.reaperCond.L.Lock()
	}
}

// heartbeatStateManager manages ZMTP 3.1 heartbeat state following Majordomo pattern
func (sck *socket) heartbeatStateManager() {
	defer close(sck.heartbeatDone)
	
	if sck.heartbeatIVL <= 0 {
		return // Heartbeat disabled
	}
	
	// Local state variables (only accessed by this goroutine)
	type connState struct {
		lastActivity   time.Time
		lastPingSent   time.Time
		waitingForPong bool
		connected      bool
	}
	
	connections := make(map[string]*connState)
	ticker := time.NewTicker(sck.heartbeatIVL)
	defer ticker.Stop()
	
	for {
		select {
		case <-sck.ctx.Done():
			return
			
		case update := <-sck.heartbeatUpdateCh:
			// Handle heartbeat state updates
			state, exists := connections[update.connID]
			if !exists {
				state = &connState{
					lastActivity: update.timestamp,
					connected:    true,
				}
				connections[update.connID] = state
			}
			
			switch update.updateType {
			case heartbeatConnected:
				state.connected = true
				state.lastActivity = update.timestamp
				state.waitingForPong = false
				
			case heartbeatDisconnected:
				state.connected = false
				delete(connections, update.connID)
				
			case heartbeatPingSent:
				state.lastPingSent = update.timestamp
				state.waitingForPong = true
				
			case heartbeatPongReceived:
				state.waitingForPong = false
				state.lastActivity = update.timestamp
				
			case heartbeatActivityDetected:
				state.lastActivity = update.timestamp
			}
			
		case query := <-sck.heartbeatQueryCh:
			// Handle heartbeat queries
			switch query.queryType {
			case queryHeartbeatConnections:
				connList := make([]string, 0, len(connections))
				for connID := range connections {
					connList = append(connList, connID)
				}
				query.responseCh <- connList
				
			case queryConnectionLiveness:
				if state, exists := connections[query.connID]; exists {
					query.responseCh <- state.connected && !state.waitingForPong
				} else {
					query.responseCh <- false
				}
				
			case queryHeartbeatStats:
				stats := map[string]interface{}{
					"active_connections": len(connections),
					"heartbeat_interval": sck.heartbeatIVL,
					"heartbeat_ttl":      sck.heartbeatTTL,
					"heartbeat_timeout":  sck.heartbeatTimeout,
				}
				query.responseCh <- stats
			}
			
		case <-ticker.C:
			// Check for timeouts and send heartbeats
			now := time.Now()
			var timeoutConns []string
			
			for connID, state := range connections {
				if !state.connected {
					continue
				}
				
				// Check for timeout if waiting for PONG
				if state.waitingForPong && sck.heartbeatTimeout > 0 {
					if now.Sub(state.lastPingSent) > sck.heartbeatTimeout {
						timeoutConns = append(timeoutConns, connID)
						continue
					}
				}
				
				// Check if we need to send a PING
				needsPing := now.Sub(state.lastActivity) > sck.heartbeatIVL ||
					now.Sub(state.lastPingSent) > sck.heartbeatIVL
				
				if needsPing && !state.waitingForPong {
					// Send PING request through connection-specific channel
					sck.sendHeartbeatPing(connID)
				}
			}
			
			// Handle timeouts
			for _, connID := range timeoutConns {
				sck.handleHeartbeatTimeout(connID)
				delete(connections, connID)
			}
		}
	}
}

// sendHeartbeatPing sends a PING command to a specific connection
func (sck *socket) sendHeartbeatPing(connID string) {
	// Find the connection
	sck.mu.RLock()
	var targetConn *Conn
	for _, conn := range sck.conns {
		if sck.getConnID(conn) == connID {
			targetConn = conn
			break
		}
	}
	sck.mu.RUnlock()
	
	if targetConn == nil || targetConn.Closed() {
		return
	}
	
	// Create PING command body per RFC 37
	// ping = command-size %d4 "PING" ping-ttl ping-context
	// ping-ttl = 2OCTET (16-bit unsigned integer in network order)
	// ping-context = 0*16OCTET (can be empty, max 16 octets)
	
	ttlDeciseconds := uint16(sck.heartbeatTTL / (100 * time.Millisecond))
	pingBody := make([]byte, 2) // TTL only, no context for simplicity
	binary.BigEndian.PutUint16(pingBody, ttlDeciseconds)
	
	err := targetConn.SendCmd(CmdPing, pingBody)
	if err != nil {
		if sck.log != nil {
			sck.log.Printf("zmq4: failed to send heartbeat PING to %s: %v", connID, err)
		}
		// Notify state manager of failure
		sck.updateHeartbeatState(heartbeatTimeout, connID, time.Now(), nil)
		return
	}
	
	// Notify state manager that PING was sent
	sck.updateHeartbeatState(heartbeatPingSent, connID, time.Now(), nil)
}

// handleHeartbeatTimeout handles connection timeout due to heartbeat failure
func (sck *socket) handleHeartbeatTimeout(connID string) {
	// Find and close the connection
	sck.mu.RLock()
	var targetConn *Conn
	for _, conn := range sck.conns {
		if sck.getConnID(conn) == connID {
			targetConn = conn
			break
		}
	}
	sck.mu.RUnlock()
	
	if targetConn != nil {
		if sck.log != nil {
			sck.log.Printf("zmq4: closing connection %s due to heartbeat timeout", connID)
		}
		targetConn.Close()
		sck.scheduleRmConn(targetConn)
	}
}

// getConnID generates a unique identifier for a connection
func (sck *socket) getConnID(conn *Conn) string {
	if conn.rw != nil {
		return conn.rw.RemoteAddr().String()
	}
	return fmt.Sprintf("%p", conn)
}

// updateHeartbeatState sends a state update to the heartbeat manager
func (sck *socket) updateHeartbeatState(updateType heartbeatUpdateType, connID string, timestamp time.Time, data interface{}) {
	update := heartbeatUpdate{
		updateType: updateType,
		connID:     connID,
		timestamp:  timestamp,
		data:       data,
	}
	
	select {
	case sck.heartbeatUpdateCh <- update:
		// Successfully sent update
	default:
		// Channel full, drop update to prevent blocking
	}
}

// queryHeartbeatState queries the heartbeat manager for connection state
func (sck *socket) queryHeartbeatState(queryType heartbeatQueryType, connID string) interface{} {
	responseCh := make(chan interface{}, 1)
	query := heartbeatQuery{
		queryType:  queryType,
		connID:     connID,
		responseCh: responseCh,
	}
	
	select {
	case sck.heartbeatQueryCh <- query:
		select {
		case result := <-responseCh:
			return result
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	default:
		return nil
	}
}

// heartbeatCallback handles heartbeat notifications from connections
func (sck *socket) heartbeatCallback(updateType, connID string, timestamp time.Time, data interface{}) {
	var hbType heartbeatUpdateType
	
	switch updateType {
	case "activity":
		hbType = heartbeatActivityDetected
	case "pong":
		hbType = heartbeatPongReceived
	default:
		return // Unknown update type
	}
	
	sck.updateHeartbeatState(hbType, connID, timestamp, data)
}

var (
	_ Socket = (*socket)(nil)
)
