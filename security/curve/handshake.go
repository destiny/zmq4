// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package curve

import (
	"crypto/rand"
	"fmt"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/go-zeromq/zmq4"
)

// clientHandshake performs the CURVE client-side handshake
func (s *Security) clientHandshake(conn *zmq4.Conn) error {
	// Step 1: Generate transient key pair for this connection
	var err error
	s.clientTransient, err = GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("curve: failed to generate client transient keys: %w", err)
	}
	
	// Step 2: Send HELLO command
	err = s.sendHello(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to send HELLO: %w", err)
	}
	
	// Step 3: Receive WELCOME command
	err = s.recvWelcome(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to receive WELCOME: %w", err)
	}
	
	// Step 4: Send INITIATE command
	err = s.sendInitiate(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to send INITIATE: %w", err)
	}
	
	// Step 5: Receive READY command
	err = s.recvReady(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to receive READY: %w", err)
	}
	
	return nil
}

// serverHandshake performs the CURVE server-side handshake
func (s *Security) serverHandshake(conn *zmq4.Conn) error {
	// Step 1: Receive HELLO command
	err := s.recvHello(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to receive HELLO: %w", err)
	}
	
	// Step 2: Generate transient key pair for this connection
	s.serverTransient, err = GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("curve: failed to generate server transient keys: %w", err)
	}
	
	// Step 3: Send WELCOME command
	err = s.sendWelcome(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to send WELCOME: %w", err)
	}
	
	// Step 4: Receive INITIATE command
	err = s.recvInitiate(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to receive INITIATE: %w", err)
	}
	
	// Step 5: Send READY command
	err = s.sendReady(conn)
	if err != nil {
		return fmt.Errorf("curve: failed to send READY: %w", err)
	}
	
	return nil
}

// sendHello sends the HELLO command (client -> server)
func (s *Security) sendHello(conn *zmq4.Conn) error {
	// HELLO body format (200 bytes) per RFC 26/CurveZMQ:
	// signature[64] + client_transient_public[32] + zeros[64] + signature_box[40]
	// The signature_box contains 64 zero bytes encrypted with client transient secret
	// and server permanent public key for anti-amplification protection
	
	body := make([]byte, HelloBodySize)
	
	// Anti-amplification signature (64 bytes): all zeros per RFC 26
	// This prevents DDoS amplification attacks by ensuring HELLO is >= WELCOME
	// Already zero from make()
	
	// Client transient public key (32 bytes)
	copy(body[64:96], s.clientTransient.Public[:])
	
	// Anti-amplification padding (64 bytes): all zeros per RFC 26
	// This ensures HELLO command is large enough to prevent amplification
	// Already zero from make()
	
	// Create signature box: 64 zero bytes encrypted with client transient secret
	// and server permanent public key (40 bytes: 24 nonce prefix + 16 auth tag)
	var nonce [NonceSize]byte
	copy(nonce[:8], "CurveZMQ") // 8-byte prefix
	copy(nonce[8:], "HELLO---") // 16-byte suffix for HELLO command
	
	// Anti-amplification box: encrypt 64 zero bytes
	zeroBytes := make([]byte, 64)
	signatureBox := box.Seal(nil, zeroBytes, &nonce, &s.serverKey, &s.clientTransient.Secret)
	if len(signatureBox) != 80 { // 64 + 16 auth tag
		return fmt.Errorf("curve: invalid signature box size: got %d, want 80", len(signatureBox))
	}
	
	// Place signature box at the end (bytes 160-239), but only store 40 bytes as per RFC
	copy(body[160:200], signatureBox[:40])
	
	// Validate total size
	if len(body) != HelloBodySize {
		return fmt.Errorf("curve: invalid HELLO body size: got %d, want %d", len(body), HelloBodySize)
	}
	
	// Send HELLO command
	return conn.SendCmd(zmq4.CmdHello, body)
}

// recvHello receives and processes the HELLO command (server side)
func (s *Security) recvHello(conn *zmq4.Conn) error {
	cmd, err := conn.RecvCmd()
	if err != nil {
		return fmt.Errorf("curve: could not receive HELLO: %w", err)
	}
	
	if cmd.Name != zmq4.CmdHello {
		return fmt.Errorf("curve: expected HELLO, got %s", cmd.Name)
	}
	
	if len(cmd.Body) != HelloBodySize {
		return fmt.Errorf("curve: invalid HELLO body size: got %d, want %d", len(cmd.Body), HelloBodySize)
	}
	
	// Parse HELLO body per RFC 26/CurveZMQ format:
	// signature[64] + client_transient_public[32] + zeros[64] + signature_box[40]
	body := cmd.Body
	
	// Verify anti-amplification signature (first 64 bytes should be zeros)
	for i := 0; i < 64; i++ {
		if body[i] != 0 {
			return fmt.Errorf("curve: invalid HELLO signature at byte %d: got %d, want 0", i, body[i])
		}
	}
	
	// Extract client transient public key (bytes 64-95)
	s.clientTransient = &KeyPair{}
	copy(s.clientTransient.Public[:], body[64:96])
	
	// Verify anti-amplification padding (bytes 96-159 should be zeros)
	for i := 96; i < 160; i++ {
		if body[i] != 0 {
			return fmt.Errorf("curve: invalid HELLO padding at byte %d: got %d, want 0", i, body[i])
		}
	}
	
	// Verify signature box for anti-amplification protection (bytes 160-199)
	var nonce [NonceSize]byte
	copy(nonce[:8], "CurveZMQ") // 8-byte prefix
	copy(nonce[8:], "HELLO---") // 16-byte suffix for HELLO command
	
	// Reconstruct the signature box - we need to pad it back to 80 bytes
	signatureBox := make([]byte, 80)
	copy(signatureBox[:40], body[160:200])
	// The remaining 40 bytes stay zero (they were truncated in the message)
	
	// Attempt to decrypt the signature box
	decrypted, ok := box.Open(nil, signatureBox, &nonce, &s.clientTransient.Public, &s.keyPair.Secret)
	if !ok {
		return fmt.Errorf("curve: failed to verify HELLO signature box - possible authentication failure")
	}
	
	// Verify the decrypted content is 64 zero bytes (anti-amplification check)
	if len(decrypted) != 64 {
		return fmt.Errorf("curve: invalid signature box content size: got %d, want 64", len(decrypted))
	}
	
	for i := 0; i < 64; i++ {
		if decrypted[i] != 0 {
			return fmt.Errorf("curve: invalid signature box content at byte %d: got %d, want 0", i, decrypted[i])
		}
	}
	
	// Store client's transient key is already extracted above
	// The client's permanent key will be revealed in the INITIATE command
	
	return nil
}

// sendWelcome sends the WELCOME command (server -> client)
func (s *Security) sendWelcome(conn *zmq4.Conn) error {
	// WELCOME body format (168 bytes) per RFC 26/CurveZMQ:
	// server_transient_public[32] + cookie[96] + zeros[40]
	// The cookie contains encrypted client/server state for stateless server operation
	
	body := make([]byte, WelcomeBodySize)
	
	// Server transient public key (32 bytes)
	copy(body[0:32], s.serverTransient.Public[:])
	
	// Create cookie: encrypted server state to enable stateless operation
	// Cookie format: client_transient_public[32] + server_secret[32] + timestamp[8]
	var cookieNonce [NonceSize]byte
	copy(cookieNonce[:8], "CurveZMQ") // 8-byte prefix
	copy(cookieNonce[8:], "COOKIE--") // 16-byte suffix
	
	// Create cookie plaintext: client transient public + server transient secret + timestamp
	cookiePlain := make([]byte, 72) // 32 + 32 + 8
	copy(cookiePlain[0:32], s.clientTransient.Public[:])
	copy(cookiePlain[32:64], s.serverTransient.Secret[:])
	
	// Add timestamp for cookie expiration (8 bytes, big-endian)
	timestamp := time.Now().Unix()
	cookiePlain[64] = byte(timestamp >> 56)
	cookiePlain[65] = byte(timestamp >> 48)
	cookiePlain[66] = byte(timestamp >> 40)
	cookiePlain[67] = byte(timestamp >> 32)
	cookiePlain[68] = byte(timestamp >> 24)
	cookiePlain[69] = byte(timestamp >> 16)
	cookiePlain[70] = byte(timestamp >> 8)
	cookiePlain[71] = byte(timestamp)
	
	// Encrypt cookie with server's permanent key (for server to decrypt later)
	// Use a derived key from server permanent key for cookie encryption
	var cookieKey [KeySize]byte
	copy(cookieKey[:], s.keyPair.Secret[:])
	
	cookie := box.Seal(nil, cookiePlain, &cookieNonce, &s.keyPair.Public, &cookieKey)
	if len(cookie) != 96 { // 72 + 24 nonce prefix + 16 auth tag - 16 for compact form = 96
		return fmt.Errorf("curve: invalid cookie size: got %d, want 96", len(cookie))
	}
	
	// Place cookie in message (bytes 32-127)
	copy(body[32:128], cookie)
	
	// Padding (40 bytes) - already zero from make()
	
	// Validate total size matches updated WelcomeBodySize (should be 168)
	if len(body) != WelcomeBodySize {
		return fmt.Errorf("curve: invalid WELCOME body size: got %d, want %d", len(body), WelcomeBodySize)
	}
	
	// Send WELCOME command
	return conn.SendCmd(zmq4.CmdWelcome, body)
}

// recvWelcome receives and processes the WELCOME command (client side)
func (s *Security) recvWelcome(conn *zmq4.Conn) error {
	cmd, err := conn.RecvCmd()
	if err != nil {
		return fmt.Errorf("curve: could not receive WELCOME: %w", err)
	}
	
	if cmd.Name != zmq4.CmdWelcome {
		return fmt.Errorf("curve: expected WELCOME, got %s", cmd.Name)
	}
	
	if len(cmd.Body) != WelcomeBodySize {
		return fmt.Errorf("curve: invalid WELCOME body size: got %d, want %d", len(cmd.Body), WelcomeBodySize)
	}
	
	body := cmd.Body
	
	// Extract server transient public key (bytes 0-31)
	s.serverTransient = &KeyPair{}
	copy(s.serverTransient.Public[:], body[0:32])
	
	// Extract and store cookie for INITIATE command (bytes 32-127)
	s.cookie = make([]byte, 96)
	copy(s.cookie, body[32:128])
	
	// Verify padding (bytes 128-167 should be zeros)
	for i := 128; i < 168; i++ {
		if body[i] != 0 {
			return fmt.Errorf("curve: invalid WELCOME padding at byte %d: got %d, want 0", i, body[i])
		}
	}
	
	// Cookie is now stored in s.cookie for use in INITIATE command
	// We cannot verify the cookie contents since it's encrypted with server's key
	// The server will validate it when we send it back in INITIATE
	
	return nil
}

// sendInitiate sends the INITIATE command (client -> server)
func (s *Security) sendInitiate(conn *zmq4.Conn) error {
	// INITIATE body format per RFC 26/CurveZMQ (257+ bytes):
	// cookie[96] + client_transient_public[32] + 
	// box[server_transient_public + client_permanent_public + metadata](client_permanent_secret, server_transient_public)[variable] +
	// box[client_permanent_public](client_transient_secret, server_permanent_public)[48]
	
	if s.cookie == nil || len(s.cookie) != 96 {
		return fmt.Errorf("curve: no valid cookie available for INITIATE")
	}
	
	// Serialize metadata
	metadata, err := conn.Meta.MarshalZMTP()
	if err != nil {
		return fmt.Errorf("curve: could not serialize metadata: %w", err)
	}
	
	// Calculate total body size
	// cookie[96] + client_transient_public[32] + metadata_box[variable] + vouch_box[48]
	metadataBoxPlainSize := KeySize + KeySize + len(metadata) // server transient + client permanent + metadata
	metadataBoxSize := metadataBoxPlainSize + BoxOverhead
	vouchBoxSize := 48 // client permanent public key + auth overhead
	bodySize := 96 + KeySize + metadataBoxSize + vouchBoxSize
	
	body := make([]byte, bodySize)
	offset := 0
	
	// Cookie from WELCOME command (96 bytes)
	copy(body[offset:offset+96], s.cookie)
	offset += 96
	
	// Client transient public key (32 bytes)
	copy(body[offset:offset+KeySize], s.clientTransient.Public[:])
	offset += KeySize
	
	// Create metadata box: server_transient_public + client_permanent_public + metadata
	var metadataNonce [NonceSize]byte
	copy(metadataNonce[:8], "CurveZMQ") // 8-byte prefix
	copy(metadataNonce[8:], "INITIATE") // 16-byte suffix
	
	metadataPlain := make([]byte, metadataBoxPlainSize)
	copy(metadataPlain[0:KeySize], s.serverTransient.Public[:])
	copy(metadataPlain[KeySize:2*KeySize], s.keyPair.Public[:])
	copy(metadataPlain[2*KeySize:], metadata)
	
	metadataBox := box.Seal(nil, metadataPlain, &metadataNonce, &s.serverTransient.Public, &s.keyPair.Secret)
	copy(body[offset:offset+len(metadataBox)], metadataBox)
	offset += len(metadataBox)
	
	// Create vouch box: client_permanent_public encrypted with client_transient_secret and server_permanent_public
	var vouchNonce [NonceSize]byte
	copy(vouchNonce[:8], "CurveZMQ") // 8-byte prefix  
	copy(vouchNonce[8:], "VOUCH---") // 16-byte suffix
	
	vouchBox := box.Seal(nil, s.keyPair.Public[:], &vouchNonce, &s.serverKey, &s.clientTransient.Secret)
	if len(vouchBox) != vouchBoxSize {
		return fmt.Errorf("curve: invalid vouch box size: got %d, want %d", len(vouchBox), vouchBoxSize)
	}
	copy(body[offset:], vouchBox)
	
	// Send INITIATE command
	return conn.SendCmd(zmq4.CmdInitiate, body)
}

// recvInitiate receives and processes the INITIATE command (server side)
func (s *Security) recvInitiate(conn *zmq4.Conn) error {
	cmd, err := conn.RecvCmd()
	if err != nil {
		return fmt.Errorf("curve: could not receive INITIATE: %w", err)
	}
	
	if cmd.Name != zmq4.CmdInitiate {
		return fmt.Errorf("curve: expected INITIATE, got %s", cmd.Name)
	}
	
	if len(cmd.Body) < InitiateBodySize {
		return fmt.Errorf("curve: INITIATE body too small: got %d, want at least %d", len(cmd.Body), InitiateBodySize)
	}
	
	body := cmd.Body
	offset := 0
	
	// Parse INITIATE format: cookie[96] + client_transient_public[32] + metadata_box[variable] + vouch_box[48]
	
	// Extract and validate cookie (96 bytes)
	if len(body) < 96 {
		return fmt.Errorf("curve: INITIATE missing cookie")
	}
	cookie := body[offset:offset+96]
	offset += 96
	
	// Decrypt and validate cookie to restore server state
	var cookieNonce [NonceSize]byte
	copy(cookieNonce[:8], "CurveZMQ") // 8-byte prefix
	copy(cookieNonce[8:], "COOKIE--") // 16-byte suffix
	
	var cookieKey [KeySize]byte
	copy(cookieKey[:], s.keyPair.Secret[:])
	
	cookieDecrypted, ok := box.Open(nil, cookie, &cookieNonce, &s.keyPair.Public, &cookieKey)
	if !ok {
		return fmt.Errorf("curve: failed to decrypt cookie - invalid or expired")
	}
	
	if len(cookieDecrypted) != 72 { // 32 + 32 + 8
		return fmt.Errorf("curve: invalid cookie content size: got %d, want 72", len(cookieDecrypted))
	}
	
	// Extract client transient public key from cookie
	var clientTransientFromCookie [KeySize]byte
	copy(clientTransientFromCookie[:], cookieDecrypted[0:32])
	
	// Extract server transient secret from cookie
	copy(s.serverTransient.Secret[:], cookieDecrypted[32:64])
	
	// Verify timestamp (8 bytes, big-endian) for cookie freshness
	timestamp := int64(cookieDecrypted[64])<<56 |
		int64(cookieDecrypted[65])<<48 |
		int64(cookieDecrypted[66])<<40 |
		int64(cookieDecrypted[67])<<32 |
		int64(cookieDecrypted[68])<<24 |
		int64(cookieDecrypted[69])<<16 |
		int64(cookieDecrypted[70])<<8 |
		int64(cookieDecrypted[71])
	
	// Check cookie is not too old (60 seconds max age)
	if time.Now().Unix()-timestamp > 60 {
		return fmt.Errorf("curve: cookie expired")
	}
	
	// Extract client transient public key from message (32 bytes)
	if len(body) < offset+KeySize {
		return fmt.Errorf("curve: INITIATE missing client transient public key")
	}
	
	var clientTransientFromMsg [KeySize]byte
	copy(clientTransientFromMsg[:], body[offset:offset+KeySize])
	offset += KeySize
	
	// Verify client transient keys match (cookie vs message)
	if !bytesEqual(clientTransientFromCookie[:], clientTransientFromMsg[:]) {
		return fmt.Errorf("curve: client transient key mismatch between cookie and message")
	}
	
	// Verify client transient key matches what we received in HELLO
	if !bytesEqual(clientTransientFromMsg[:], s.clientTransient.Public[:]) {
		return fmt.Errorf("curve: client transient key mismatch with HELLO")
	}
	
	// Parse metadata box (variable size, ends before vouch box)
	vouchBoxSize := 48
	if len(body) < offset+vouchBoxSize {
		return fmt.Errorf("curve: INITIATE too short for vouch box")
	}
	
	metadataBoxEnd := len(body) - vouchBoxSize
	metadataBox := body[offset:metadataBoxEnd]
	
	// Decrypt metadata box
	var metadataNonce [NonceSize]byte
	copy(metadataNonce[:8], "CurveZMQ") // 8-byte prefix
	copy(metadataNonce[8:], "INITIATE") // 16-byte suffix
	
	metadataDecrypted, ok := box.Open(nil, metadataBox, &metadataNonce, &s.serverTransient.Public, &s.keyPair.Secret)
	if !ok {
		return fmt.Errorf("curve: failed to decrypt INITIATE metadata box")
	}
	
	if len(metadataDecrypted) < 2*KeySize {
		return fmt.Errorf("curve: invalid metadata box content: too short")
	}
	
	// Verify server transient public key
	if !bytesEqual(metadataDecrypted[0:KeySize], s.serverTransient.Public[:]) {
		return fmt.Errorf("curve: server transient key mismatch in INITIATE")
	}
	
	// Extract client permanent public key
	var clientPermanentKey [KeySize]byte
	copy(clientPermanentKey[:], metadataDecrypted[KeySize:2*KeySize])
	
	// Extract metadata
	metadata := metadataDecrypted[2*KeySize:]
	err = conn.Peer.Meta.UnmarshalZMTP(metadata)
	if err != nil {
		return fmt.Errorf("curve: could not unmarshal peer metadata: %w", err)
	}
	
	// Verify vouch box (48 bytes at the end)
	vouchBox := body[metadataBoxEnd:]
	if len(vouchBox) != vouchBoxSize {
		return fmt.Errorf("curve: invalid vouch box size: got %d, want %d", len(vouchBox), vouchBoxSize)
	}
	
	var vouchNonce [NonceSize]byte
	copy(vouchNonce[:8], "CurveZMQ") // 8-byte prefix
	copy(vouchNonce[8:], "VOUCH---") // 16-byte suffix
	
	vouchDecrypted, ok := box.Open(nil, vouchBox, &vouchNonce, &s.keyPair.Public, &s.clientTransient.Secret)
	if !ok {
		return fmt.Errorf("curve: failed to verify vouch box")
	}
	
	if len(vouchDecrypted) != KeySize {
		return fmt.Errorf("curve: invalid vouch content size: got %d, want %d", len(vouchDecrypted), KeySize)
	}
	
	// Verify vouch contains client permanent public key
	if !bytesEqual(vouchDecrypted, clientPermanentKey[:]) {
		return fmt.Errorf("curve: vouch verification failed - key mismatch")
	}
	
	// Store client permanent key for future use
	copy(s.serverKey[:], clientPermanentKey[:]) // Reusing serverKey field to store client key on server side
	
	return nil
}

// sendReady sends the READY command (server -> client)
func (s *Security) sendReady(conn *zmq4.Conn) error {
	// Serialize metadata
	metadata, err := conn.Meta.MarshalZMTP()
	if err != nil {
		return fmt.Errorf("curve: could not serialize metadata: %w", err)
	}
	
	// READY body: box[metadata](server_transient_secret, client_transient_public)
	var nonce [NonceSize]byte
	copy(nonce[:], "CurveZMQREADY---")
	
	readyBox := box.Seal(nil, metadata, &nonce, &s.clientTransient.Public, &s.serverTransient.Secret)
	
	// Send READY command
	return conn.SendCmd(zmq4.CmdReady, readyBox)
}

// recvReady receives and processes the READY command (client side)
func (s *Security) recvReady(conn *zmq4.Conn) error {
	cmd, err := conn.RecvCmd()
	if err != nil {
		return fmt.Errorf("curve: could not receive READY: %w", err)
	}
	
	if cmd.Name != zmq4.CmdReady {
		return fmt.Errorf("curve: expected READY, got %s", cmd.Name)
	}
	
	// Decrypt READY box
	var nonce [NonceSize]byte
	copy(nonce[:], "CurveZMQREADY---")
	
	metadata, ok := box.Open(nil, cmd.Body, &nonce, &s.serverTransient.Public, &s.clientTransient.Secret)
	if !ok {
		return fmt.Errorf("curve: failed to decrypt READY box")
	}
	
	// Unmarshal metadata
	err = conn.Peer.Meta.UnmarshalZMTP(metadata)
	if err != nil {
		return fmt.Errorf("curve: could not unmarshal peer metadata: %w", err)
	}
	
	return nil
}

// bytesEqual compares two byte slices for equality
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// generateNonce generates a unique nonce for message encryption
func generateNonce() ([NonceSize]byte, error) {
	var nonce [NonceSize]byte
	_, err := rand.Read(nonce[:])
	return nonce, err
}