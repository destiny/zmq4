// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package majordomo

import (
	"testing"
)

func TestCURVESecurity(t *testing.T) {
	// Test key generation
	serverKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	_, err = GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	// Test Z85 encoding/decoding
	serverPubZ85, err := serverKeys.PublicKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode server public key as Z85: %v", err)
	}
	
	serverSecZ85, err := serverKeys.SecretKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode server secret key as Z85: %v", err)
	}
	
	// Test loading keys from Z85
	loadedKeys, err := LoadCURVEKeysFromZ85(serverPubZ85, serverSecZ85)
	if err != nil {
		t.Fatalf("Failed to load keys from Z85: %v", err)
	}
	
	if loadedKeys.Public != serverKeys.Public {
		t.Error("Loaded public key doesn't match original")
	}
	
	if loadedKeys.Secret != serverKeys.Secret {
		t.Error("Loaded secret key doesn't match original")
	}
	
	// Test hex encoding/decoding  
	serverPubHex := serverKeys.PublicKeyHex()
	serverSecHex := serverKeys.SecretKeyHex()
	
	loadedKeysHex, err := LoadCURVEKeysFromHex(serverPubHex, serverSecHex)
	if err != nil {
		t.Fatalf("Failed to load keys from hex: %v", err)
	}
	
	if loadedKeysHex.Public != serverKeys.Public {
		t.Error("Loaded public key from hex doesn't match original")
	}
	
	if loadedKeysHex.Secret != serverKeys.Secret {
		t.Error("Loaded secret key from hex doesn't match original")
	}
	
	// Test key validation
	err = ValidateCURVEKey(serverPubZ85)
	if err != nil {
		t.Errorf("Valid Z85 key failed validation: %v", err)
	}
	
	// Test invalid key
	err = ValidateCURVEKey("invalid")
	if err == nil {
		t.Error("Invalid Z85 key passed validation")
	}
}

func TestCURVESecurityConfig(t *testing.T) {
	// Generate keys
	serverKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	clientKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	// Test server security config
	serverConfig := NewCURVEServerSecurity(serverKeys)
	if serverConfig.ServerKeys != serverKeys {
		t.Error("Server config doesn't contain correct server keys")
	}
	
	serverSecurity, err := serverConfig.ToZMQSecurity(true)
	if err != nil {
		t.Errorf("Failed to create server security: %v", err)
	}
	
	if serverSecurity.Type() != "CURVE" {
		t.Errorf("Expected CURVE security type, got %s", serverSecurity.Type())
	}
	
	// Test client security config
	clientConfig := NewCURVEClientSecurity(clientKeys, serverKeys.Public)
	if clientConfig.ClientKeys != clientKeys {
		t.Error("Client config doesn't contain correct client keys")
	}
	
	if clientConfig.ServerPubKey != serverKeys.Public {
		t.Error("Client config doesn't contain correct server public key")
	}
	
	clientSecurity, err := clientConfig.ToZMQSecurity(false)
	if err != nil {
		t.Errorf("Failed to create client security: %v", err)
	}
	
	if clientSecurity.Type() != "CURVE" {
		t.Errorf("Expected CURVE security type, got %s", clientSecurity.Type())
	}
}

func TestBrokerWithCURVE(t *testing.T) {
	// Test broker creation with CURVE security
	serverKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	broker, err := NewBrokerWithCURVE("tcp://127.0.0.1:15555", serverKeys, nil)
	if err != nil {
		t.Fatalf("Failed to create CURVE broker: %v", err)
	}
	
	if broker == nil {
		t.Fatal("CURVE broker is nil")
	}
	
	// Test that security is properly configured
	if broker.options.Security == nil {
		t.Error("Broker security is not configured")
	}
	
	if broker.options.Security.Type() != "CURVE" {
		t.Errorf("Expected CURVE security, got %s", broker.options.Security.Type())
	}
}

func TestWorkerWithCURVE(t *testing.T) {
	// Test worker creation with CURVE security
	serverKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	clientKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	handler := func(request []byte) ([]byte, error) {
		return request, nil
	}
	
	worker, err := NewWorkerWithCURVE("test", "tcp://127.0.0.1:15555", handler, 
		clientKeys, serverKeys.Public, nil)
	if err != nil {
		t.Fatalf("Failed to create CURVE worker: %v", err)
	}
	
	if worker == nil {
		t.Fatal("CURVE worker is nil")
	}
	
	// Test that security is properly configured
	if worker.options.Security == nil {
		t.Error("Worker security is not configured")
	}
	
	if worker.options.Security.Type() != "CURVE" {
		t.Errorf("Expected CURVE security, got %s", worker.options.Security.Type())
	}
}

func TestClientWithCURVE(t *testing.T) {
	// Test client creation with CURVE security
	serverKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	clientKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	client, err := NewClientWithCURVE("tcp://127.0.0.1:15555", clientKeys, 
		serverKeys.Public, nil)
	if err != nil {
		t.Fatalf("Failed to create CURVE client: %v", err)
	}
	
	if client == nil {
		t.Fatal("CURVE client is nil")
	}
	
	// Test that security is properly configured
	if client.options.Security == nil {
		t.Error("Client security is not configured")
	}
	
	if client.options.Security.Type() != "CURVE" {
		t.Errorf("Expected CURVE security, got %s", client.options.Security.Type())
	}
}

func TestAsyncClientWithCURVE(t *testing.T) {
	// Test async client creation with CURVE security
	serverKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}
	
	clientKeys, err := GenerateCURVEKeys()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}
	
	client, err := NewAsyncClientWithCURVE("tcp://127.0.0.1:15555", clientKeys, 
		serverKeys.Public, nil)
	if err != nil {
		t.Fatalf("Failed to create CURVE async client: %v", err)
	}
	
	if client == nil {
		t.Fatal("CURVE async client is nil")
	}
	
	// Test that security is properly configured
	if client.options.Security == nil {
		t.Error("Async client security is not configured")
	}
	
	if client.options.Security.Type() != "CURVE" {
		t.Errorf("Expected CURVE security, got %s", client.options.Security.Type())
	}
}