// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package curve

import (
	"bytes"
	"testing"

	"github.com/go-zeromq/zmq4"
)

func TestKeyPairGeneration(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}
	
	if len(kp.Public) != KeySize {
		t.Errorf("Invalid public key size: got %d, want %d", len(kp.Public), KeySize)
	}
	
	if len(kp.Secret) != KeySize {
		t.Errorf("Invalid secret key size: got %d, want %d", len(kp.Secret), KeySize)
	}
	
	// Ensure keys are not all zeros
	zeroKey := [KeySize]byte{}
	if bytes.Equal(kp.Public[:], zeroKey[:]) {
		t.Error("Public key is all zeros")
	}
	if bytes.Equal(kp.Secret[:], zeroKey[:]) {
		t.Error("Secret key is all zeros")
	}
}

func TestSecurityType(t *testing.T) {
	serverKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	sec := NewServerSecurity(serverKeys)
	if sec.Type() != zmq4.CurveSecurity {
		t.Errorf("Invalid security type: got %s, want %s", sec.Type(), zmq4.CurveSecurity)
	}
}

func TestClientServerSecurity(t *testing.T) {
	// Generate key pairs
	serverKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	clientKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	// Create security instances
	serverSec := NewServerSecurity(serverKeys)
	clientSec := NewClientSecurity(clientKeys, serverKeys.Public)
	
	// Verify types
	if serverSec.Type() != zmq4.CurveSecurity {
		t.Errorf("Server security type mismatch")
	}
	if clientSec.Type() != zmq4.CurveSecurity {
		t.Errorf("Client security type mismatch")
	}
	
	// Verify key assignment
	if !bytes.Equal(clientSec.serverKey[:], serverKeys.Public[:]) {
		t.Error("Client does not have correct server public key")
	}
}

func TestNonceGeneration(t *testing.T) {
	serverKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	// Test server nonce generation
	serverSec := NewServerSecurity(serverKeys)
	
	nonce1 := serverSec.nextSendNonce()
	nonce2 := serverSec.nextSendNonce()
	
	// Nonces should be different
	if bytes.Equal(nonce1[:], nonce2[:]) {
		t.Error("Generated nonces are identical")
	}
	
	// Test client nonce generation
	clientKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	clientSec := NewClientSecurity(clientKeys, serverKeys.Public)
	// Simulate that handshake is completed by setting up transient keys
	clientSec.clientTransient, _ = GenerateKeyPair()
	clientSec.serverTransient, _ = GenerateKeyPair()
	
	clientNonce1 := clientSec.nextSendNonce()
	clientNonce2 := clientSec.nextSendNonce()
	
	// Client nonces should be different
	if bytes.Equal(clientNonce1[:], clientNonce2[:]) {
		t.Error("Generated client nonces are identical")
	}
	
	// Client and server nonces should be different
	if bytes.Equal(nonce1[:], clientNonce1[:]) {
		t.Error("Server and client nonces are identical")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	// Generate key pairs
	serverKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	clientKeys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	// Create security instances and simulate handshake completion
	serverSec := NewServerSecurity(serverKeys)
	serverSec.clientTransient, _ = GenerateKeyPair()
	serverSec.serverTransient, _ = GenerateKeyPair()
	
	clientSec := NewClientSecurity(clientKeys, serverKeys.Public)
	// Use the same transient keys so they can communicate
	clientSec.clientTransient = serverSec.clientTransient
	clientSec.serverTransient = serverSec.serverTransient
	
	// Test message to encrypt
	message := []byte("Hello, CURVE ZMQ!")
	
	// Client encrypts message
	var encrypted bytes.Buffer
	n, err := clientSec.Encrypt(&encrypted, message)
	if err != nil {
		t.Fatalf("Failed to encrypt message: %v", err)
	}
	
	if n != encrypted.Len() {
		t.Errorf("Encrypt returned wrong byte count: got %d, want %d", n, encrypted.Len())
	}
	
	// Server decrypts message
	var decrypted bytes.Buffer
	n, err = serverSec.Decrypt(&decrypted, encrypted.Bytes())
	if err != nil {
		t.Fatalf("Failed to decrypt message: %v", err)
	}
	
	if n != decrypted.Len() {
		t.Errorf("Decrypt returned wrong byte count: got %d, want %d", n, decrypted.Len())
	}
	
	// Verify decrypted message matches original
	if !bytes.Equal(message, decrypted.Bytes()) {
		t.Errorf("Decrypted message does not match original:\ngot:  %q\nwant: %q", decrypted.Bytes(), message)
	}
}

func TestBytesEqual(t *testing.T) {
	a := []byte{1, 2, 3, 4}
	b := []byte{1, 2, 3, 4}
	c := []byte{1, 2, 3, 5}
	d := []byte{1, 2, 3}
	
	if !bytesEqual(a, b) {
		t.Error("Equal byte slices reported as not equal")
	}
	
	if bytesEqual(a, c) {
		t.Error("Different byte slices reported as equal")
	}
	
	if bytesEqual(a, d) {
		t.Error("Different length byte slices reported as equal")
	}
}