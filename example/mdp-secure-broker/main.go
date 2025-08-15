// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example Majordomo broker with CURVE security
package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/destiny/zmq4/v25/majordomo"
)

func main() {
	// Generate server key pair for CURVE security
	serverKeys, err := majordomo.GenerateCURVEKeys()
	if err != nil {
		log.Fatalf("Failed to generate server keys: %v", err)
	}
	
	// Display server public key for clients
	pubZ85, _ := serverKeys.PublicKeyZ85()
	log.Printf("Server public key (Z85): %s", pubZ85)
	log.Printf("Server public key (hex): %x", serverKeys.Public)
	log.Printf("Clients need this server public key to connect securely")
	
	// Create secure broker with CURVE security
	broker, err := majordomo.NewBrokerWithCURVE("tcp://*:5555", serverKeys, nil)
	if err != nil {
		log.Fatalf("Failed to create secure broker: %v", err)
	}
	
	// Start broker
	err = broker.Start()
	if err != nil {
		log.Fatalf("Failed to start broker: %v", err)
	}
	
	log.Printf("Secure Majordomo broker started on tcp://*:5555")
	
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
				stats := broker.GetStats()
				statsJSON, _ := json.MarshalIndent(stats, "", "  ")
				log.Printf("Broker stats:\n%s", statsJSON)
			case <-sigCh:
				return
			}
		}
	}()
	
	// Wait for shutdown signal
	<-sigCh
	log.Printf("Shutting down broker...")
	
	// Stop broker
	err = broker.Stop()
	if err != nil {
		log.Printf("Error stopping broker: %v", err)
	}
	
	log.Printf("Secure broker stopped")
}