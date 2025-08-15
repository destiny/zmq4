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

	"github.com/destiny/zmq4/v25/majordomo"
)

func main() {
	// Create broker with default options (no security)
	options := majordomo.DefaultBrokerOptions()
	options.HeartbeatLiveness = 3
	options.HeartbeatInterval = 2500 * time.Millisecond
	options.RequestTimeout = 30 * time.Second
	options.LogInfo = true
	
	broker := majordomo.NewBroker("tcp://*:5555", options)
	
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