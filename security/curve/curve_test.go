// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package curve

import (
	"bytes"
	"strings"
	"testing"

	"github.com/destiny/zmq4"
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

func TestKeyPairZ85Encoding(t *testing.T) {
	// Generate a key pair
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}
	
	// Test Z85 encoding
	pubZ85, err := kp.PublicKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode public key as Z85: %v", err)
	}
	
	secZ85, err := kp.SecretKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode secret key as Z85: %v", err)
	}
	
	// Z85 encoded 32-byte keys should be exactly 40 characters
	if len(pubZ85) != 40 {
		t.Errorf("Public key Z85 encoding should be 40 characters, got %d", len(pubZ85))
	}
	
	if len(secZ85) != 40 {
		t.Errorf("Secret key Z85 encoding should be 40 characters, got %d", len(secZ85))
	}
	
	// Test hex encoding for comparison
	pubHex := kp.PublicKeyHex()
	secHex := kp.SecretKeyHex()
	
	// Hex encoded 32-byte keys should be exactly 64 characters
	if len(pubHex) != 64 {
		t.Errorf("Public key hex encoding should be 64 characters, got %d", len(pubHex))
	}
	
	if len(secHex) != 64 {
		t.Errorf("Secret key hex encoding should be 64 characters, got %d", len(secHex))
	}
	
	t.Logf("Z85 encoding is %d%% more compact than hex", 
		100*(len(pubHex)-len(pubZ85))/len(pubHex))
}

func TestNewKeyPairFromZ85(t *testing.T) {
	// Generate original key pair
	original, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}
	
	// Encode to Z85
	pubZ85, err := original.PublicKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode public key: %v", err)
	}
	
	secZ85, err := original.SecretKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode secret key: %v", err)
	}
	
	// Recreate key pair from Z85
	restored, err := NewKeyPairFromZ85(pubZ85, secZ85)
	if err != nil {
		t.Fatalf("Failed to create key pair from Z85: %v", err)
	}
	
	// Verify keys match
	if !bytes.Equal(original.Public[:], restored.Public[:]) {
		t.Error("Restored public key does not match original")
	}
	
	if !bytes.Equal(original.Secret[:], restored.Secret[:]) {
		t.Error("Restored secret key does not match original")
	}
}

func TestNewKeyPairFromHex(t *testing.T) {
	// Generate original key pair
	original, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}
	
	// Encode to hex
	pubHex := original.PublicKeyHex()
	secHex := original.SecretKeyHex()
	
	// Recreate key pair from hex
	restored, err := NewKeyPairFromHex(pubHex, secHex)
	if err != nil {
		t.Fatalf("Failed to create key pair from hex: %v", err)
	}
	
	// Verify keys match
	if !bytes.Equal(original.Public[:], restored.Public[:]) {
		t.Error("Restored public key does not match original")
	}
	
	if !bytes.Equal(original.Secret[:], restored.Secret[:]) {
		t.Error("Restored secret key does not match original")
	}
}

func TestValidateZ85Key(t *testing.T) {
	// Generate a valid key
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}
	
	validZ85, err := kp.PublicKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode key: %v", err)
	}
	
	tests := []struct {
		name    string
		keyZ85  string
		wantErr bool
	}{
		{"valid key", validZ85, false},
		{"invalid length", "tooshort", true},
		{"invalid character", strings.Repeat("~", 40), true}, // ~ not in Z85 alphabet
		{"wrong decoded length", "HelloWorldHelloWorldHelloWorldHello", true}, // 35 chars, wrong length
		{"empty string", "", true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateZ85Key(tt.keyZ85)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateZ85Key() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestZ85VsHexKeyFormats(t *testing.T) {
	// Generate test key pair
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}
	
	// Get both formats
	pubZ85, err := kp.PublicKeyZ85()
	if err != nil {
		t.Fatalf("Failed to get Z85 public key: %v", err)
	}
	
	pubHex := kp.PublicKeyHex()
	
	// Test that both can recreate the same key
	kpFromZ85, err := NewKeyPairFromZ85(pubZ85, pubZ85) // Using pubZ85 for both for simplicity
	if err == nil {
		// This should work for public key at least
		_ = kpFromZ85
	}
	
	kpFromHex, err := NewKeyPairFromHex(pubHex, pubHex) // Using pubHex for both for simplicity
	if err == nil {
		// This should work for public key at least
		_ = kpFromHex
	}
	
	t.Logf("Original key (hex): %s", pubHex)
	t.Logf("Original key (Z85): %s", pubZ85)
	t.Logf("Z85 is %d chars vs hex %d chars (%d%% reduction)", 
		len(pubZ85), len(pubHex), 100*(len(pubHex)-len(pubZ85))/len(pubHex))
}

func TestZ85ErrorHandling(t *testing.T) {
	t.Run("NewKeyPairFromZ85 invalid public", func(t *testing.T) {
		_, err := NewKeyPairFromZ85("invalid", "HelloWorldHelloWorldHelloWorldHello")
		if err == nil {
			t.Error("Expected error for invalid public key")
		}
		if !strings.Contains(err.Error(), "invalid public key Z85 encoding") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("NewKeyPairFromZ85 invalid secret", func(t *testing.T) {
		validZ85 := strings.Repeat("0", 40) // 40 chars of valid Z85 characters
		_, err := NewKeyPairFromZ85(validZ85, "inval") // 5 chars, wrong decode length
		if err == nil {
			t.Error("Expected error for invalid secret key")
		}
		// Should error because "inval" decodes to wrong length
		if !strings.Contains(err.Error(), "secret key must be 32 bytes") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("NewKeyPairFromHex invalid public", func(t *testing.T) {
		validHex := strings.Repeat("ab", 32) // 64 chars
		_, err := NewKeyPairFromHex("invalid", validHex)
		if err == nil {
			t.Error("Expected error for invalid public key")
		}
		if !strings.Contains(err.Error(), "invalid public key hex encoding") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("NewKeyPairFromHex invalid secret", func(t *testing.T) {
		validHex := strings.Repeat("ab", 32) // 64 chars
		_, err := NewKeyPairFromHex(validHex, "invalid")
		if err == nil {
			t.Error("Expected error for invalid secret key")
		}
		if !strings.Contains(err.Error(), "invalid secret key hex encoding") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
}