// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example Majordomo broker
package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/destiny/zmq4/majordomo"
)

func main() {
	// Create broker
	broker := majordomo.NewBroker("tcp://*:5555")
	
	// Configure broker
	broker.SetHeartbeat(3, 2500*time.Millisecond)
	broker.SetRequestTimeout(30 * time.Second)
	
	// Start broker
	err := broker.Start()
	if err != nil {
		log.Fatalf("Failed to start broker: %v", err)
	}
	
	log.Printf("Majordomo broker started on tcp://*:5555")
	
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
	
	log.Printf("Broker stopped")
}