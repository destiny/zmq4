// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example Majordomo worker
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/destiny/zmq4/v25/majordomo"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <service_name>", os.Args[0])
	}
	
	serviceName := majordomo.ServiceName(os.Args[1])
	
	// Create request handler
	handler := func(request []byte) ([]byte, error) {
		log.Printf("Processing request: %s", string(request))
		
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		
		// Echo back with timestamp
		response := fmt.Sprintf("Echo from %s at %s: %s", 
			serviceName, 
			time.Now().Format(time.RFC3339), 
			string(request))
		
		return []byte(response), nil
	}
	
	// Create worker with custom options
	options := &majordomo.WorkerOptions{
		HeartbeatLiveness: 3,
		HeartbeatInterval: 2500 * time.Millisecond,
		ReconnectInterval: 2500 * time.Millisecond,
		LogErrors:         true,
		LogInfo:           true,
	}
	
	worker, err := majordomo.NewWorker(serviceName, "tcp://127.0.0.1:5555", handler, options)
	if err != nil {
		log.Fatalf("Failed to create worker: %v", err)
	}
	
	// Start worker
	err = worker.Start()
	if err != nil {
		log.Fatalf("Failed to start worker: %v", err)
	}
	
	log.Printf("Majordomo worker started for service: %s", serviceName)
	
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
	
	log.Printf("Worker stopped")
}