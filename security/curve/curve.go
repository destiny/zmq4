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
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"

	"github.com/destiny/zmq4"
)

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

// Handshake implements the ZMTP security handshake according to CURVE specification
func (s *Security) Handshake(conn *zmq4.Conn, server bool) error {
	if server {
		return s.serverHandshake(conn)
	}
	return s.clientHandshake(conn)
}

// Encrypt writes the encrypted form of data to w
func (s *Security) Encrypt(w io.Writer, data []byte) (int, error) {
	return s.EncryptWithFlags(w, data, false)
}

// EncryptWithFlags writes the encrypted form of data to w with continuation support
func (s *Security) EncryptWithFlags(w io.Writer, data []byte, hasMore bool) (int, error) {
	// CURVE MESSAGE format per RFC 26: [flags][nonce][box]
	// flags: 1 byte (0x00 for final message, 0x01 for more messages coming)
	// nonce: 8 bytes (little-endian counter)
	// box: encrypted data with 16-byte authentication tag
	
	// Generate next nonce for message
	nonce := s.nextSendNonce()
	
	// Determine which keys to use based on our role
	var recipientKey, senderKey *[KeySize]byte
	if s.clientTransient == nil || s.serverTransient == nil {
		return 0, fmt.Errorf("curve: handshake not completed")
	}
	
	if s.isServer {
		// We are server, encrypt for client
		recipientKey = &s.clientTransient.Public
		senderKey = &s.serverTransient.Secret
	} else {
		// We are client, encrypt for server
		recipientKey = &s.serverTransient.Public
		senderKey = &s.clientTransient.Secret
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
	
	return w.Write(msg)
}

// Decrypt writes the decrypted form of data to w
func (s *Security) Decrypt(w io.Writer, data []byte) (int, error) {
	n, _, err := s.DecryptWithFlags(w, data)
	return n, err
}

// DecryptWithFlags decrypts data and returns the number of bytes written, continuation flag, and error
func (s *Security) DecryptWithFlags(w io.Writer, data []byte) (int, bool, error) {
	if len(data) < 1+8+BoxOverhead {
		return 0, false, ErrInvalidCommand
	}
	
	// Parse MESSAGE format per RFC 26: [flags][8-byte nonce][box]
	flags := data[0]
	hasMore := (flags & 0x01) != 0 // Check continuation flag
	
	nonce8 := data[1:9]
	encrypted := data[9:]
	
	// Validate flags (only bit 0 is defined in RFC 26)
	if flags > 0x01 {
		return 0, false, fmt.Errorf("curve: invalid flags byte: %02x", flags)
	}
	
	// Reconstruct full 24-byte nonce
	var fullNonce [NonceSize]byte
	copy(fullNonce[:8], nonce8)
	copy(fullNonce[8:], "CurveZMQMESSAGE-") // 16-byte prefix per RFC
	
	// Determine which keys to use based on our role
	var senderKey, recipientKey *[KeySize]byte
	if s.clientTransient == nil || s.serverTransient == nil {
		return 0, false, fmt.Errorf("curve: handshake not completed")
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
	decrypted, ok := box.Open(nil, encrypted, &fullNonce, senderKey, recipientKey)
	if !ok {
		return 0, false, ErrDecryptionFailed
	}
	
	n, err := w.Write(decrypted)
	return n, hasMore, err
}

// nextSendNonce generates the next nonce for outgoing messages per RFC 26/CurveZMQ
func (s *Security) nextSendNonce() [8]byte {
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

var (
	_ zmq4.Security = (*Security)(nil)
)