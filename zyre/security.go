// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"context"
	"fmt"
	"sync"

	"github.com/destiny/zmq4/v25"
	"github.com/destiny/zmq4/v25/security/curve"
	"github.com/destiny/zmq4/v25/security/null"
	"github.com/destiny/zmq4/v25/security/plain"
)

// SecurityType represents the type of security mechanism
type SecurityType int

const (
	SecurityTypeNull SecurityType = iota
	SecurityTypePlain
	SecurityTypeCurve
)

// SecurityConfig holds security configuration for ZRE nodes
type SecurityConfig struct {
	Type SecurityType // Security mechanism type
	
	// PLAIN authentication
	Username string // Username for PLAIN auth
	Password string // Password for PLAIN auth
	
	// CURVE authentication
	PublicKey  string // Our public key (Z85 encoded)
	SecretKey  string // Our secret key (Z85 encoded)
	ServerKey  string // Server public key for CURVE client
	
	// Certificate management
	CertDir    string            // Directory for certificates
	Whitelist  []string          // Whitelisted public keys
	Blacklist  []string          // Blacklisted public keys
	Headers    map[string]string // Additional security headers
}

// SecurityManager manages security for ZRE communications
type SecurityManager struct {
	config     *SecurityConfig    // Security configuration
	mechanisms map[string]zmq4.Security // Available security mechanisms
	
	// State management
	ctx        context.Context    // Context for cancellation
	cancel     context.CancelFunc // Cancel function
	mutex      sync.RWMutex       // Protects manager state
}

// SecureSocket represents a ZMQ socket with security applied
type SecureSocket struct {
	socket   zmq4.Socket    // Underlying ZMQ socket
	security zmq4.Security  // Applied security mechanism
	peerKey  string         // Peer's public key (for CURVE)
}

// NewSecurityManager creates a new security manager
func NewSecurityManager(config *SecurityConfig) (*SecurityManager, error) {
	if config == nil {
		config = &SecurityConfig{
			Type: SecurityTypeNull,
		}
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	sm := &SecurityManager{
		config:     config,
		mechanisms: make(map[string]zmq4.Security),
		ctx:        ctx,
		cancel:     cancel,
	}
	
	// Initialize security mechanisms
	if err := sm.initializeMechanisms(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize security mechanisms: %w", err)
	}
	
	return sm, nil
}

// initializeMechanisms sets up available security mechanisms
func (sm *SecurityManager) initializeMechanisms() error {
	// NULL security (no authentication)
	nullSec := null.Security()
	sm.mechanisms["NULL"] = nullSec
	
	// PLAIN security (username/password)
	if sm.config.Username != "" && sm.config.Password != "" {
		plainSec := plain.Security(sm.config.Username, sm.config.Password)
		sm.mechanisms["PLAIN"] = plainSec
	}
	
	// CURVE security (public key cryptography)
	if sm.config.PublicKey != "" && sm.config.SecretKey != "" {
		curveSec, err := curve.NewSecurityClient(sm.config.PublicKey, sm.config.SecretKey, sm.config.ServerKey)
		if err != nil {
			return fmt.Errorf("failed to create CURVE client security: %w", err)
		}
		sm.mechanisms["CURVE"] = curveSec
	}
	
	return nil
}

// GetSecurityMechanism returns the configured security mechanism
func (sm *SecurityManager) GetSecurityMechanism() zmq4.Security {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	switch sm.config.Type {
	case SecurityTypePlain:
		return sm.mechanisms["PLAIN"]
	case SecurityTypeCurve:
		return sm.mechanisms["CURVE"]
	default:
		return sm.mechanisms["NULL"]
	}
}

// SecureSocket creates a secure ZMQ socket with the configured security mechanism
func (sm *SecurityManager) SecureSocket(socketType zmq4.SocketType, ctx context.Context) (*SecureSocket, error) {
	var socket zmq4.Socket
	
	// Create socket based on type
	switch socketType {
	case zmq4.ROUTER:
		socket = zmq4.NewRouter(ctx)
	case zmq4.DEALER:
		socket = zmq4.NewDealer(ctx)
	case zmq4.PUB:
		socket = zmq4.NewPub(ctx)
	case zmq4.SUB:
		socket = zmq4.NewSub(ctx)
	default:
		return nil, fmt.Errorf("unsupported socket type: %v", socketType)
	}
	
	// Apply security mechanism
	security := sm.GetSecurityMechanism()
	
	// Set security options on socket
	switch sm.config.Type {
	case SecurityTypePlain:
		socket.SetOption(zmq4.OptionSecurityMechanism, zmq4.PlainSecurity)
		socket.SetOption(zmq4.OptionPlainUsername, sm.config.Username)
		socket.SetOption(zmq4.OptionPlainPassword, sm.config.Password)
		
	case SecurityTypeCurve:
		socket.SetOption(zmq4.OptionSecurityMechanism, zmq4.CurveSecurity)
		socket.SetOption(zmq4.OptionCurvePublickey, sm.config.PublicKey)
		socket.SetOption(zmq4.OptionCurveSecretkey, sm.config.SecretKey)
		if sm.config.ServerKey != "" {
			socket.SetOption(zmq4.OptionCurveServerkey, sm.config.ServerKey)
		}
		
	default:
		socket.SetOption(zmq4.OptionSecurityMechanism, zmq4.NullSecurity)
	}
	
	return &SecureSocket{
		socket:   socket,
		security: security,
	}, nil
}

// ValidatePeer validates a peer's credentials based on the security mechanism
func (sm *SecurityManager) ValidatePeer(peerKey string, peerHeaders map[string]string) error {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	switch sm.config.Type {
	case SecurityTypeCurve:
		return sm.validateCurvePeer(peerKey)
	case SecurityTypePlain:
		return sm.validatePlainPeer(peerHeaders)
	default:
		return nil // NULL security accepts all peers
	}
}

// validateCurvePeer validates a CURVE peer's public key
func (sm *SecurityManager) validateCurvePeer(peerKey string) error {
	// Check blacklist first
	for _, blacklistedKey := range sm.config.Blacklist {
		if peerKey == blacklistedKey {
			return fmt.Errorf("peer key is blacklisted: %s", peerKey)
		}
	}
	
	// If whitelist is configured, peer must be in it
	if len(sm.config.Whitelist) > 0 {
		for _, whitelistedKey := range sm.config.Whitelist {
			if peerKey == whitelistedKey {
				return nil // Peer is whitelisted
			}
		}
		return fmt.Errorf("peer key not in whitelist: %s", peerKey)
	}
	
	return nil // No whitelist configured, accept any non-blacklisted peer
}

// validatePlainPeer validates a PLAIN peer's credentials
func (sm *SecurityManager) validatePlainPeer(headers map[string]string) error {
	username, hasUsername := headers["username"]
	password, hasPassword := headers["password"]
	
	if !hasUsername || !hasPassword {
		return fmt.Errorf("missing PLAIN authentication credentials")
	}
	
	// Simple validation - in production, this would check against a user database
	if username == sm.config.Username && password == sm.config.Password {
		return nil
	}
	
	return fmt.Errorf("invalid PLAIN authentication credentials")
}

// GenerateCurveKeypair generates a new CURVE keypair
func (sm *SecurityManager) GenerateCurveKeypair() (publicKey, secretKey string, err error) {
	return curve.NewKeypair()
}

// SetCurveKeys updates the CURVE keys
func (sm *SecurityManager) SetCurveKeys(publicKey, secretKey, serverKey string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.config.PublicKey = publicKey
	sm.config.SecretKey = secretKey
	sm.config.ServerKey = serverKey
	
	// Reinitialize CURVE mechanism
	curveSec, err := curve.NewSecurityClient(publicKey, secretKey, serverKey)
	if err != nil {
		return fmt.Errorf("failed to update CURVE security: %w", err)
	}
	sm.mechanisms["CURVE"] = curveSec
	
	return nil
}

// SetPlainCredentials updates the PLAIN credentials
func (sm *SecurityManager) SetPlainCredentials(username, password string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.config.Username = username
	sm.config.Password = password
	
	// Update PLAIN mechanism
	plainSec := plain.Security(username, password)
	sm.mechanisms["PLAIN"] = plainSec
}

// AddToWhitelist adds a public key to the whitelist
func (sm *SecurityManager) AddToWhitelist(publicKey string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.config.Whitelist = append(sm.config.Whitelist, publicKey)
}

// RemoveFromWhitelist removes a public key from the whitelist
func (sm *SecurityManager) RemoveFromWhitelist(publicKey string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	for i, key := range sm.config.Whitelist {
		if key == publicKey {
			sm.config.Whitelist = append(sm.config.Whitelist[:i], sm.config.Whitelist[i+1:]...)
			break
		}
	}
}

// AddToBlacklist adds a public key to the blacklist
func (sm *SecurityManager) AddToBlacklist(publicKey string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.config.Blacklist = append(sm.config.Blacklist, publicKey)
}

// RemoveFromBlacklist removes a public key from the blacklist
func (sm *SecurityManager) RemoveFromBlacklist(publicKey string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	for i, key := range sm.config.Blacklist {
		if key == publicKey {
			sm.config.Blacklist = append(sm.config.Blacklist[:i], sm.config.Blacklist[i+1:]...)
			break
		}
	}
}

// GetConfig returns a copy of the current security configuration
func (sm *SecurityManager) GetConfig() *SecurityConfig {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	config := *sm.config
	
	// Deep copy slices and maps
	config.Whitelist = make([]string, len(sm.config.Whitelist))
	copy(config.Whitelist, sm.config.Whitelist)
	
	config.Blacklist = make([]string, len(sm.config.Blacklist))
	copy(config.Blacklist, sm.config.Blacklist)
	
	config.Headers = make(map[string]string)
	for k, v := range sm.config.Headers {
		config.Headers[k] = v
	}
	
	return &config
}

// Close cleans up the security manager
func (sm *SecurityManager) Close() {
	sm.cancel()
}

// Methods for SecureSocket

// Close closes the secure socket
func (ss *SecureSocket) Close() error {
	return ss.socket.Close()
}

// Listen binds the socket to an endpoint
func (ss *SecureSocket) Listen(endpoint string) error {
	return ss.socket.Listen(endpoint)
}

// Dial connects the socket to an endpoint
func (ss *SecureSocket) Dial(endpoint string) error {
	return ss.socket.Dial(endpoint)
}

// Send sends a message through the secure socket
func (ss *SecureSocket) Send(msg zmq4.Msg) error {
	return ss.socket.Send(msg)
}

// SendMulti sends a multipart message through the secure socket
func (ss *SecureSocket) SendMulti(msg zmq4.Msg) error {
	return ss.socket.SendMulti(msg)
}

// Recv receives a message from the secure socket
func (ss *SecureSocket) Recv() (zmq4.Msg, error) {
	return ss.socket.Recv()
}

// SetOption sets a socket option
func (ss *SecureSocket) SetOption(option string, value interface{}) error {
	return ss.socket.SetOption(option, value)
}

// GetOption gets a socket option
func (ss *SecureSocket) GetOption(option string) (interface{}, error) {
	return ss.socket.GetOption(option)
}

// Type returns the socket type
func (ss *SecureSocket) Type() zmq4.SocketType {
	return ss.socket.Type()
}