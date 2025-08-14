// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example Majordomo client with CURVE security
package main

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/destiny/zmq4/majordomo"
	"github.com/destiny/zmq4/security/curve"
)

// isHex checks if a string contains only hexadecimal characters
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func main() {
	if len(os.Args) != 4 {
		log.Fatalf("Usage: %s <service_name> <message> <server_public_key>", os.Args[0])
		log.Fatalf("Key can be in hex (64 chars) or Z85 (40 chars) format")
	}
	
	serviceName := majordomo.ServiceName(os.Args[1])
	message := os.Args[2]
	serverPubKeyStr := os.Args[3]
	
	// Parse server public key from command line (auto-detect format)
	var serverPubKey [32]byte
	var err error
	
	if len(serverPubKeyStr) == 64 && isHex(serverPubKeyStr) {
		// Hex format
		serverPubKeyBytes, err := hex.DecodeString(serverPubKeyStr)
		if err != nil {
			log.Fatalf("Invalid server public key hex: %v", err)
		}
		if len(serverPubKeyBytes) != 32 {
			log.Fatalf("Server public key must be 32 bytes, got %d", len(serverPubKeyBytes))
		}
		copy(serverPubKey[:], serverPubKeyBytes)
		log.Printf("Using hex-encoded server public key")
	} else if len(serverPubKeyStr) == 40 {
		// Z85 format
		err := curve.ValidateZ85Key(serverPubKeyStr)
		if err != nil {
			log.Fatalf("Invalid server public key Z85: %v", err)
		}
		
		// Create temporary keypair to decode
		tempKp, err := majordomo.LoadCURVEKeysFromZ85(serverPubKeyStr, serverPubKeyStr)
		if err != nil {
			log.Fatalf("Failed to decode Z85 server public key: %v", err)
		}
		serverPubKey = tempKp.Public
		log.Printf("Using Z85-encoded server public key")
	} else {
		log.Fatalf("Invalid server public key format. Expected 64-char hex or 40-char Z85, got %d chars", len(serverPubKeyStr))
	}
	
	// Generate client key pair for CURVE security
	clientKeys, err := majordomo.GenerateCURVEKeys()
	if err != nil {
		log.Fatalf("Failed to generate client keys: %v", err)
	}
	
	// Display client keys
	clientPubZ85, _ := clientKeys.PublicKeyZ85()
	log.Printf("Client public key (Z85): %s", clientPubZ85)
	log.Printf("Client public key (hex): %x", clientKeys.Public)
	
	// Create secure client with CURVE security
	client, err := majordomo.NewClientWithCURVE("tcp://127.0.0.1:5555", clientKeys, serverPubKey, nil)
	if err != nil {
		log.Fatalf("Failed to create secure client: %v", err)
	}
	
	// Connect to broker
	err = client.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to broker: %v", err)
	}
	defer client.Disconnect()
	
	log.Printf("Connected to secure Majordomo broker")
	
	// Send request
	log.Printf("Sending request to service %s: %s", serviceName, message)
	
	start := time.Now()
	reply, err := client.Request(serviceName, []byte(message))
	duration := time.Since(start)
	
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	
	log.Printf("Reply received in %v: %s", duration, string(reply))
	
	// Show client stats
	stats := client.GetStats()
	statsJSON, _ := json.MarshalIndent(stats, "", "  ")
	log.Printf("Client stats:\n%s", statsJSON)
}