// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example Majordomo worker with CURVE security
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
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
	if len(os.Args) != 3 {
		log.Fatalf("Usage: %s <service_name> <server_public_key>", os.Args[0])
		log.Fatalf("Key can be in hex (64 chars) or Z85 (40 chars) format")
	}
	
	serviceName := majordomo.ServiceName(os.Args[1])
	serverPubKeyStr := os.Args[2]
	
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
	log.Printf("Worker public key (Z85): %s", clientPubZ85)
	log.Printf("Worker public key (hex): %x", clientKeys.Public)
	
	// Create request handler
	handler := func(request []byte) ([]byte, error) {
		log.Printf("Processing request: %s", string(request))
		
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		
		// Echo back with timestamp
		response := fmt.Sprintf("Secure Echo from %s at %s: %s", 
			serviceName, 
			time.Now().Format(time.RFC3339), 
			string(request))
		
		return []byte(response), nil
	}
	
	// Create secure worker with CURVE security
	worker, err := majordomo.NewWorkerWithCURVE(serviceName, "tcp://127.0.0.1:5555", handler, 
		clientKeys, serverPubKey, nil)
	if err != nil {
		log.Fatalf("Failed to create secure worker: %v", err)
	}
	
	// Start worker
	err = worker.Start()
	if err != nil {
		log.Fatalf("Failed to start worker: %v", err)
	}
	
	log.Printf("Secure Majordomo worker started for service: %s", serviceName)
	
	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	// Start stats reporting
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				stats := worker.GetStats()
				statsJSON, _ := json.MarshalIndent(stats, "", "  ")
				log.Printf("Worker stats:\n%s", statsJSON)
			case <-sigCh:
				return
			}
		}
	}()
	
	// Wait for shutdown signal
	<-sigCh
	log.Printf("Shutting down worker...")
	
	// Stop worker
	err = worker.Stop()
	if err != nil {
		log.Printf("Error stopping worker: %v", err)
	}
	
	log.Printf("Secure worker stopped")
}