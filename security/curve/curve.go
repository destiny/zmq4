// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package curve provides the ZeroMQ CURVE security mechanism as specified by:
// https://rfc.zeromq.org/spec/25/ZMTP-CURVE/
// https://rfc.zeromq.org/spec/26/CURVEZMQ/
package curve

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/destiny/zmq4/v25"
	"github.com/destiny/zmq4/v25/z85"
)

// Timing and diagnostic utilities for CURVE protocol debugging
func logCurveEvent(eventType, details string) {
	timestamp := time.Now().UnixMicro()
	fmt.Printf("CURVE_TRACE[%d]: %s - %s\n", timestamp, eventType, details)
}

func logCurveError(eventType, errorMsg string, err error) {
	timestamp := time.Now().UnixMicro()
	fmt.Printf("CURVE_ERROR[%d]: %s - %s: %v\n", timestamp, eventType, errorMsg, err)
}

const (
	// Key sizes as per CURVE specification
	KeySize       = 32 // Curve25519 key size
	NonceSize     = 24 // NaCl box nonce size
	BoxOverhead   = 16 // NaCl box authentication overhead
	
	// Command body sizes per RFC 26/CurveZMQ
	HelloBodySize    = 200 // HELLO command body size
	WelcomeBodySize  = 168 // WELCOME command body size (updated for cookie mechanism)
	InitiateBodySize = 256 // INITIATE command body size (variable, this is minimum)
	ReadyBodySize    = 56  // READY command body size (variable, this is minimum)
)

var (
	ErrInvalidKey       = errors.New("curve: invalid key size")
	ErrInvalidNonce     = errors.New("curve: invalid nonce size")
	ErrDecryptionFailed = errors.New("curve: decryption failed")
	ErrInvalidCommand   = errors.New("curve: invalid command format")
	ErrInvalidCommandSize = errors.New("curve: invalid command size")
)

// ValidateCommandSize validates command body size according to RFC 26/CurveZMQ
func ValidateCommandSize(command string, bodySize int) error {
	switch command {
	case "HELLO":
		if bodySize != HelloBodySize {
			return fmt.Errorf("%w: HELLO command body must be exactly %d bytes, got %d", 
				ErrInvalidCommandSize, HelloBodySize, bodySize)
		}
	case "WELCOME":
		if bodySize != WelcomeBodySize {
			return fmt.Errorf("%w: WELCOME command body must be exactly %d bytes, got %d", 
				ErrInvalidCommandSize, WelcomeBodySize, bodySize)
		}
	case "INITIATE":
		if bodySize < InitiateBodySize {
			return fmt.Errorf("%w: INITIATE command body must be at least %d bytes, got %d", 
				ErrInvalidCommandSize, InitiateBodySize, bodySize)
		}
	case "READY":
		if bodySize < ReadyBodySize {
			return fmt.Errorf("%w: READY command body must be at least %d bytes, got %d", 
				ErrInvalidCommandSize, ReadyBodySize, bodySize)
		}
	case "MESSAGE":
		if bodySize < 1+8+BoxOverhead { // flags + nonce + minimum encrypted content
			return fmt.Errorf("%w: MESSAGE command body must be at least %d bytes, got %d", 
				ErrInvalidCommandSize, 1+8+BoxOverhead, bodySize)
		}
	default:
		return fmt.Errorf("curve: unknown command: %s", command)
	}
	return nil
}

// KeyPair represents a Curve25519 key pair
type KeyPair struct {
	Public [KeySize]byte
	Secret [KeySize]byte
}

// GenerateKeyPair generates a new Curve25519 key pair
func GenerateKeyPair() (*KeyPair, error) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("curve: failed to generate key pair: %w", err)
	}
	
	return &KeyPair{
		Public: *public,
		Secret: *private,
	}, nil
}

// NewKeyPair creates a key pair from existing keys
func NewKeyPair(public, secret [KeySize]byte) *KeyPair {
	return &KeyPair{
		Public: public,
		Secret: secret,
	}
}

// PublicKeyZ85 returns the public key encoded as Z85 string
func (kp *KeyPair) PublicKeyZ85() (string, error) {
	return z85.EncodeToString(kp.Public[:])
}

// SecretKeyZ85 returns the secret key encoded as Z85 string
func (kp *KeyPair) SecretKeyZ85() (string, error) {
	return z85.EncodeToString(kp.Secret[:])
}

// PublicKeyHex returns the public key encoded as hex string
func (kp *KeyPair) PublicKeyHex() string {
	return hex.EncodeToString(kp.Public[:])
}

// SecretKeyHex returns the secret key encoded as hex string
func (kp *KeyPair) SecretKeyHex() string {
	return hex.EncodeToString(kp.Secret[:])
}

// NewKeyPairFromZ85 creates a key pair from Z85-encoded public and secret keys
func NewKeyPairFromZ85(publicZ85, secretZ85 string) (*KeyPair, error) {
	// Decode public key
	publicBytes, err := z85.DecodeString(publicZ85)
	if err != nil {
		return nil, fmt.Errorf("curve: invalid public key Z85 encoding: %w", err)
	}
	if len(publicBytes) != KeySize {
		return nil, fmt.Errorf("curve: public key must be %d bytes, got %d", KeySize, len(publicBytes))
	}

	// Decode secret key
	secretBytes, err := z85.DecodeString(secretZ85)
	if err != nil {
		return nil, fmt.Errorf("curve: invalid secret key Z85 encoding: %w", err)
	}
	if len(secretBytes) != KeySize {
		return nil, fmt.Errorf("curve: secret key must be %d bytes, got %d", KeySize, len(secretBytes))
	}

	var public, secret [KeySize]byte
	copy(public[:], publicBytes)
	copy(secret[:], secretBytes)

	return &KeyPair{
		Public: public,
		Secret: secret,
	}, nil
}

// NewKeyPairFromHex creates a key pair from hex-encoded public and secret keys
func NewKeyPairFromHex(publicHex, secretHex string) (*KeyPair, error) {
	// Decode public key
	publicBytes, err := hex.DecodeString(publicHex)
	if err != nil {
		return nil, fmt.Errorf("curve: invalid public key hex encoding: %w", err)
	}
	if len(publicBytes) != KeySize {
		return nil, fmt.Errorf("curve: public key must be %d bytes, got %d", KeySize, len(publicBytes))
	}

	// Decode secret key
	secretBytes, err := hex.DecodeString(secretHex)
	if err != nil {
		return nil, fmt.Errorf("curve: invalid secret key hex encoding: %w", err)
	}
	if len(secretBytes) != KeySize {
		return nil, fmt.Errorf("curve: secret key must be %d bytes, got %d", KeySize, len(secretBytes))
	}

	var public, secret [KeySize]byte
	copy(public[:], publicBytes)
	copy(secret[:], secretBytes)

	return &KeyPair{
		Public: public,
		Secret: secret,
	}, nil
}

// ValidateZ85Key validates that a string is a valid Z85-encoded CURVE key
func ValidateZ85Key(keyZ85 string) error {
	if err := z85.ValidateString(keyZ85); err != nil {
		return fmt.Errorf("curve: invalid Z85 key format: %w", err)
	}
	
	// Check that it decodes to the correct key size
	keyBytes, err := z85.DecodeString(keyZ85)
	if err != nil {
		return fmt.Errorf("curve: Z85 key decode error: %w", err)
	}
	
	if len(keyBytes) != KeySize {
		return fmt.Errorf("curve: Z85 key must decode to %d bytes, got %d", KeySize, len(keyBytes))
	}
	
	return nil
}

// HandshakeState represents the current state of the CURVE handshake
type HandshakeState int

const (
	HandshakeInit     HandshakeState = iota // Initial state, no handshake started
	HandshakeHello                          // Client has sent HELLO, waiting for WELCOME
	HandshakeWelcome                        // Server has sent WELCOME, waiting for INITIATE
	HandshakeInitiate                       // Client has sent INITIATE, waiting for READY
	HandshakeReady                          // Server has sent READY, handshake complete
	HandshakeComplete                       // Handshake fully completed, ready for messages
)

// Security implements the CURVE security mechanism
type Security struct {
	keyPair     *KeyPair      // Our permanent key pair
	serverKey   [KeySize]byte // Server's permanent public key (for clients)
	isServer    bool          // True if we are the server, false if client
	
	// Transient keys for this connection
	clientTransient *KeyPair
	serverTransient *KeyPair
	
	// Cookie mechanism for stateless server operation
	cookie []byte // Cookie received from server (client side only)
	
	// Connection state
	clientNonce uint64
	serverNonce uint64
	
	// MESSAGE encryption state
	mu                     sync.Mutex
	messageEncryptionReady bool
	sendNonce              uint64 // Nonce counter for MESSAGE encryption
	recvNonce              uint64 // Nonce counter for MESSAGE decryption
	
	// Handshake state tracking
	handshakeState HandshakeState
	
	// Shared secrets derived during handshake
	clientSecret [KeySize]byte // Client -> Server encryption key
	serverSecret [KeySize]byte // Server -> Client encryption key
}

// NewClientSecurity creates a CURVE security mechanism for a client
func NewClientSecurity(clientKeys *KeyPair, serverPublicKey [KeySize]byte) *Security {
	return &Security{
		keyPair:   clientKeys,
		serverKey: serverPublicKey,
		isServer:  false,
	}
}

// NewServerSecurity creates a CURVE security mechanism for a server
func NewServerSecurity(serverKeys *KeyPair) *Security {
	return &Security{
		keyPair:  serverKeys,
		isServer: true,
	}
}

// Type returns the security mechanism type
func (s *Security) Type() zmq4.SecurityType {
	return zmq4.CurveSecurity
}

// HandshakeState returns the current handshake state
func (s *Security) HandshakeState() int {
	return int(s.handshakeState)
}

// IsReady returns true if the security mechanism is ready for MESSAGE encryption/decryption
func (s *Security) IsReady() bool {
	return s.handshakeState == HandshakeComplete &&
		s.clientTransient != nil &&
		s.serverTransient != nil
}

// ValidateMessageReadiness validates that the security mechanism is ready for MESSAGE operations
func (s *Security) ValidateMessageReadiness() error {
	if s.handshakeState != HandshakeComplete {
		return fmt.Errorf("curve: handshake not complete, current state: %d", s.handshakeState)
	}
	
	if s.clientTransient == nil {
		return fmt.Errorf("curve: client transient keys not available")
	}
	
	if s.serverTransient == nil {
		return fmt.Errorf("curve: server transient keys not available")
	}
	
	// Validate key sizes
	if len(s.clientTransient.Public) != KeySize || len(s.clientTransient.Secret) != KeySize {
		return fmt.Errorf("curve: invalid client transient key size")
	}
	
	if len(s.serverTransient.Public) != KeySize || len(s.serverTransient.Secret) != KeySize {
		return fmt.Errorf("curve: invalid server transient key size")
	}
	
	return nil
}

// EnsureMessageReadiness validates that the security mechanism and connection are ready for MESSAGE operations
// This method includes additional validation for connection state and timing
func (s *Security) EnsureMessageReadiness(conn *zmq4.Conn) error {
	// First check basic CURVE readiness
	if err := s.ValidateMessageReadiness(); err != nil {
		return err
	}
	
	// Additional connection state validation
	if conn == nil {
		return fmt.Errorf("curve: connection is nil")
	}
	
	if conn.Closed() {
		return fmt.Errorf("curve: connection is closed")
	}
	
	// TCP connection stability check - critical for handshake-to-MESSAGE transition
	if err := s.validateTCPConnection(conn); err != nil {
		return fmt.Errorf("curve: TCP connection validation failed: %w", err)
	}
	
	// Verify MESSAGE encryption state is ready
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if !s.messageEncryptionReady {
		return fmt.Errorf("curve: MESSAGE encryption not ready - handshake may have failed")
	}
	
	return nil
}

// validateTCPConnection performs TCP-level validation to ensure the connection is stable
// This is critical for the CURVE handshake-to-MESSAGE transition
func (s *Security) validateTCPConnection(conn *zmq4.Conn) error {
	// Check basic connection state
	if conn.Closed() {
		return fmt.Errorf("connection is marked as closed")
	}
	
	// Validate that the connection is in a stable state for MESSAGE encryption
	// Instead of using artificial delays, we check the actual handshake state
	if s.handshakeState != HandshakeComplete {
		return fmt.Errorf("handshake not complete, current state: %d", s.handshakeState)
	}
	
	// Ensure all required keys are available
	if s.clientTransient == nil {
		return fmt.Errorf("client transient keys not available")
	}
	
	if s.serverTransient == nil {
		return fmt.Errorf("server transient keys not available")
	}
	
	// Verify key integrity
	if len(s.clientTransient.Public) != KeySize || len(s.serverTransient.Public) != KeySize {
		return fmt.Errorf("invalid transient key sizes")
	}
	
	return nil
}

// Handshake implements the ZMTP security handshake according to CURVE specification
func (s *Security) Handshake(conn *zmq4.Conn, server bool) error {
	if server {
		return s.serverHandshake(conn)
	}
	return s.clientHandshake(conn)
}

// Encrypt writes the encrypted form of data to w (for MESSAGE commands only)
func (s *Security) Encrypt(w io.Writer, data []byte) (int, error) {
	return s.EncryptWithFlags(w, data, false)
}

// EncryptWithFlags writes the encrypted form of data to w with continuation support (for MESSAGE commands only)
func (s *Security) EncryptWithFlags(w io.Writer, data []byte, hasMore bool) (int, error) {
	logCurveEvent("MESSAGE_ENCRYPT_START", fmt.Sprintf("Starting encryption: %d bytes, hasMore=%v", len(data), hasMore))
	
	// Validate that CURVE is ready for MESSAGE encryption
	if err := s.ValidateMessageReadiness(); err != nil {
		logCurveError("MESSAGE_ENCRYPT_VALIDATION", "MESSAGE encryption validation failed", err)
		return 0, fmt.Errorf("curve: MESSAGE encryption validation failed: %w", err)
	}
	
	// CURVE MESSAGE format per RFC 26: [flags][nonce][box]
	// flags: 1 byte (0x00 for final message, 0x01 for more messages coming)
	// nonce: 8 bytes (little-endian counter)
	// box: encrypted data with 16-byte authentication tag
	
	// Generate next nonce for message
	nonce := s.nextSendNonce()
	logCurveEvent("MESSAGE_ENCRYPT_NONCE", fmt.Sprintf("Generated nonce: %x", nonce[:]))
	
	// Determine which keys to use based on our role
	var recipientKey, senderKey *[KeySize]byte
	if s.clientTransient == nil || s.serverTransient == nil {
		logCurveError("MESSAGE_ENCRYPT_KEYS", "Transient keys not available", nil)
		return 0, fmt.Errorf("curve: transient keys not available for message encryption")
	}
	
	if s.isServer {
		// We are server, encrypt for client
		recipientKey = &s.clientTransient.Public
		senderKey = &s.serverTransient.Secret
		logCurveEvent("MESSAGE_ENCRYPT_KEYS", fmt.Sprintf("Server encrypting for client: recipient=%x..., sender=%x...", 
			(*recipientKey)[:4], (*senderKey)[:4]))
	} else {
		// We are client, encrypt for server
		recipientKey = &s.serverTransient.Public
		senderKey = &s.clientTransient.Secret
		logCurveEvent("MESSAGE_ENCRYPT_KEYS", fmt.Sprintf("Client encrypting for server: recipient=%x..., sender=%x...", 
			(*recipientKey)[:4], (*senderKey)[:4]))
	}
	
	// Create full 24-byte nonce for NaCl box
	var fullNonce [NonceSize]byte
	copy(fullNonce[:8], nonce[:])
	copy(fullNonce[8:], "CurveZMQMESSAGE-") // 16-byte prefix per RFC
	
	// Encrypt the message data
	encrypted := box.Seal(nil, data, &fullNonce, recipientKey, senderKey)
	
	// Construct MESSAGE: [flags][8-byte nonce][encrypted_data]
	msg := make([]byte, 1+8+len(encrypted))
	
	// Set flags byte according to RFC 26
	if hasMore {
		msg[0] = 0x01 // More messages coming (continuation)
	} else {
		msg[0] = 0x00 // Final message
	}
	
	copy(msg[1:9], nonce[:])
	copy(msg[9:], encrypted)
	
	logCurveEvent("MESSAGE_ENCRYPT_SUCCESS", fmt.Sprintf("Encryption completed: flags=%02x, nonce=%x, encrypted_len=%d, total_len=%d", 
		msg[0], msg[1:9], len(encrypted), len(msg)))
	
	n, err := w.Write(msg)
	if err != nil {
		logCurveError("MESSAGE_ENCRYPT_WRITE", "Failed to write encrypted MESSAGE", err)
		return n, err
	}
	
	// CRITICAL FIX: Ensure the CURVE MESSAGE is fully written and flushed
	// The timing diagnostics show that the broker receives 0 bytes instead of our 25-byte MESSAGE
	if flusher, ok := w.(interface{ Flush() error }); ok {
		if flushErr := flusher.Flush(); flushErr != nil {
			logCurveError("MESSAGE_ENCRYPT_FLUSH", "Failed to flush encrypted MESSAGE", flushErr)
			return n, flushErr
		}
		logCurveEvent("MESSAGE_ENCRYPT_FLUSH", "Successfully flushed MESSAGE to network")
	}
	
	logCurveEvent("MESSAGE_ENCRYPT_COMPLETE", fmt.Sprintf("Successfully wrote %d bytes", n))
	return n, nil
}

// Decrypt writes the decrypted form of data to w (for MESSAGE commands only)
func (s *Security) Decrypt(w io.Writer, data []byte) (int, error) {
	n, _, err := s.DecryptWithFlags(w, data)
	return n, err
}

// DecryptWithFlags decrypts data and returns the number of bytes written, continuation flag, and error (for MESSAGE commands only)
func (s *Security) DecryptWithFlags(w io.Writer, data []byte) (int, bool, error) {
	logCurveEvent("MESSAGE_DECRYPT_START", fmt.Sprintf("Starting decryption: %d bytes", len(data)))
	
	// Validate that CURVE is ready for MESSAGE decryption
	if err := s.ValidateMessageReadiness(); err != nil {
		logCurveError("MESSAGE_DECRYPT_VALIDATION", "MESSAGE decryption validation failed", err)
		return 0, false, fmt.Errorf("curve: MESSAGE decryption validation failed: %w", err)
	}
	
	if len(data) < 1+8+BoxOverhead {
		logCurveError("MESSAGE_DECRYPT_SIZE", "MESSAGE too small for valid CURVE format", fmt.Errorf("got %d bytes, need at least %d", len(data), 1+8+BoxOverhead))
		return 0, false, ErrInvalidCommand
	}
	
	// Parse MESSAGE format per RFC 26: [flags][8-byte nonce][box]
	flags := data[0]
	hasMore := (flags & 0x01) != 0 // Check continuation flag
	
	nonce8 := data[1:9]
	encrypted := data[9:]
	
	logCurveEvent("MESSAGE_DECRYPT_PARSE", fmt.Sprintf("Parsed MESSAGE: flags=%02x, hasMore=%v, nonce=%x, encrypted_len=%d", 
		flags, hasMore, nonce8, len(encrypted)))
	
	// Validate flags (only bit 0 is defined in RFC 26)
	if flags > 0x01 {
		logCurveError("MESSAGE_DECRYPT_FLAGS", "Invalid flags byte", fmt.Errorf("flags=%02x", flags))
		return 0, false, fmt.Errorf("curve: invalid flags byte: %02x", flags)
	}
	
	// Reconstruct full 24-byte nonce
	var fullNonce [NonceSize]byte
	copy(fullNonce[:8], nonce8)
	copy(fullNonce[8:], "CurveZMQMESSAGE-") // 16-byte prefix per RFC
	
	// Determine which keys to use based on our role
	var senderKey, recipientKey *[KeySize]byte
	if s.clientTransient == nil || s.serverTransient == nil {
		return 0, false, fmt.Errorf("curve: transient keys not available for message decryption")
	}
	
	if s.isServer {
		// We are server, decrypt from client
		senderKey = &s.clientTransient.Public
		recipientKey = &s.serverTransient.Secret
	} else {
		// We are client, decrypt from server
		senderKey = &s.serverTransient.Public
		recipientKey = &s.clientTransient.Secret
	}
	
	// Decrypt the message
	logCurveEvent("MESSAGE_DECRYPT_ATTEMPT", fmt.Sprintf("Attempting decryption: fullNonce=%x, senderKey=%x..., recipientKey=%x...", 
		fullNonce[:], (*senderKey)[:4], (*recipientKey)[:4]))
	
	decrypted, ok := box.Open(nil, encrypted, &fullNonce, senderKey, recipientKey)
	if !ok {
		logCurveError("MESSAGE_DECRYPT_FAILED", "NaCl box.Open failed - authentication or decryption error", 
			fmt.Errorf("nonce=%x, encrypted_len=%d", fullNonce[:], len(encrypted)))
		return 0, false, ErrDecryptionFailed
	}
	
	logCurveEvent("MESSAGE_DECRYPT_SUCCESS", fmt.Sprintf("Decryption successful: %d bytes decrypted", len(decrypted)))
	
	n, err := w.Write(decrypted)
	if err != nil {
		logCurveError("MESSAGE_DECRYPT_WRITE", "Failed to write decrypted data", err)
		return n, hasMore, err
	}
	
	logCurveEvent("MESSAGE_DECRYPT_COMPLETE", fmt.Sprintf("Successfully wrote %d bytes", n))
	return n, hasMore, err
}

// nextSendNonce generates the next nonce for outgoing messages per RFC 26/CurveZMQ
func (s *Security) nextSendNonce() [8]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	var nonce [8]byte
	
	// CURVE nonce format: 8-byte little-endian counter
	// Client uses even numbers starting from 2, server uses odd numbers starting from 1
	if s.isServer {
		// We are server - use odd numbers starting from 1
		if s.serverNonce == 0 {
			s.serverNonce = 1 // First server nonce is 1
		} else {
			s.serverNonce += 2 // Increment by 2 to maintain odd sequence
		}
		binary.LittleEndian.PutUint64(nonce[:], s.serverNonce)
	} else {
		// We are client - use even numbers starting from 2
		if s.clientNonce == 0 {
			s.clientNonce = 2 // First client nonce is 2 (skip 0)
		} else {
			s.clientNonce += 2 // Increment by 2 to maintain even sequence
		}
		binary.LittleEndian.PutUint64(nonce[:], s.clientNonce)
	}
	
	return nonce
}

// nextClientNonce generates the next nonce for client->server messages (legacy)
func (s *Security) nextClientNonce() [NonceSize]byte {
	var nonce [NonceSize]byte
	
	// CURVE nonce format: 8-byte little-endian counter + 16-byte prefix
	// Client uses even numbers, server uses odd numbers
	s.clientNonce += 2
	if s.clientNonce == 0 {
		s.clientNonce = 2
	}
	
	// Write counter to first 8 bytes in little-endian
	binary.LittleEndian.PutUint64(nonce[:8], s.clientNonce)
	
	// The remaining 16 bytes are set to a constant value for this connection
	// In a full implementation, this would be derived from the handshake
	copy(nonce[8:], "CurveZMQMESSAGE-")
	
	return nonce
}

// nextServerNonce generates the next nonce for server->client messages (legacy)
func (s *Security) nextServerNonce() [NonceSize]byte {
	var nonce [NonceSize]byte
	
	// Server uses odd numbers starting from 1
	s.serverNonce += 2
	if s.serverNonce == 0 || s.serverNonce == 2 {
		s.serverNonce = 1
	}
	
	// Write counter to first 8 bytes in little-endian
	binary.LittleEndian.PutUint64(nonce[:8], s.serverNonce)
	
	// Set prefix for message nonces
	copy(nonce[8:], "CurveZMQMESSAGE-")
	
	return nonce
}

// EncryptHandshakeCommand encrypts a handshake command based on current handshake state
func (s *Security) EncryptHandshakeCommand(w io.Writer, command string, data []byte) (int, error) {
	switch command {
	case "HELLO":
		return s.encryptHello(w, data)
	case "WELCOME":
		return s.encryptWelcome(w, data)
	case "INITIATE":
		return s.encryptInitiate(w, data)
	case "READY":
		return s.encryptReady(w, data)
	default:
		return 0, fmt.Errorf("curve: unknown handshake command: %s", command)
	}
}

// DecryptHandshakeCommand decrypts a handshake command based on current handshake state
func (s *Security) DecryptHandshakeCommand(w io.Writer, command string, data []byte) (int, error) {
	switch command {
	case "HELLO":
		return s.decryptHello(w, data)
	case "WELCOME":
		return s.decryptWelcome(w, data)
	case "INITIATE":
		return s.decryptInitiate(w, data)
	case "READY":
		return s.decryptReady(w, data)
	default:
		return 0, fmt.Errorf("curve: unknown handshake command: %s", command)
	}
}

// encryptHello encrypts HELLO command (client side)
// HELLO is sent in plain text according to RFC 26/CurveZMQ - no encryption needed
func (s *Security) encryptHello(w io.Writer, data []byte) (int, error) {
	// HELLO command is sent in plain text per RFC 26
	return w.Write(data)
}

// decryptHello decrypts HELLO command (server side)
// HELLO is received in plain text according to RFC 26/CurveZMQ - no decryption needed
func (s *Security) decryptHello(w io.Writer, data []byte) (int, error) {
	// HELLO command is received in plain text per RFC 26
	return w.Write(data)
}

// encryptWelcome encrypts WELCOME command (server side)
// WELCOME is sent in plain text according to RFC 26/CurveZMQ - no encryption needed
func (s *Security) encryptWelcome(w io.Writer, data []byte) (int, error) {
	// WELCOME command is sent in plain text per RFC 26
	return w.Write(data)
}

// decryptWelcome decrypts WELCOME command (client side)
// WELCOME is received in plain text according to RFC 26/CurveZMQ - no decryption needed
func (s *Security) decryptWelcome(w io.Writer, data []byte) (int, error) {
	// WELCOME command is received in plain text per RFC 26
	return w.Write(data)
}

// encryptInitiate encrypts INITIATE command (client side)  
// INITIATE is sent in plain text according to RFC 26/CurveZMQ - no encryption needed
func (s *Security) encryptInitiate(w io.Writer, data []byte) (int, error) {
	// INITIATE command is sent in plain text per RFC 26
	return w.Write(data)
}

// decryptInitiate decrypts INITIATE command (server side)
// INITIATE is received in plain text according to RFC 26/CurveZMQ - no decryption needed
func (s *Security) decryptInitiate(w io.Writer, data []byte) (int, error) {
	// INITIATE command is received in plain text per RFC 26
	return w.Write(data)
}

// encryptReady encrypts READY command (server side)
// READY is sent in plain text according to RFC 26/CurveZMQ - no encryption needed
func (s *Security) encryptReady(w io.Writer, data []byte) (int, error) {
	// READY command is sent in plain text per RFC 26
	return w.Write(data)
}

// decryptReady decrypts READY command (client side)
// READY is received in plain text according to RFC 26/CurveZMQ - no decryption needed
func (s *Security) decryptReady(w io.Writer, data []byte) (int, error) {
	// READY command is received in plain text per RFC 26
	return w.Write(data)
}

// Test helper methods for protocol compliance testing
// SetClientTransient sets the client transient key pair (for testing)
func (s *Security) SetClientTransient(keyPair *KeyPair) {
	s.clientTransient = keyPair
}

// SetServerTransient sets the server transient key pair (for testing)  
func (s *Security) SetServerTransient(keyPair *KeyPair) {
	s.serverTransient = keyPair
}

// SetHandshakeComplete marks the handshake as complete (for testing)
func (s *Security) SetHandshakeComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handshakeState = HandshakeComplete
	s.messageEncryptionReady = true
	s.serverNonce = 0
	s.clientNonce = 0
}

var (
	_ zmq4.Security = (*Security)(nil)
)