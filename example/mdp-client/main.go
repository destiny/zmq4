// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example Majordomo client
package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/destiny/zmq4/v25/majordomo"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("Usage: %s <service_name> <message>", os.Args[0])
	}
	
	serviceName := majordomo.ServiceName(os.Args[1])
	message := os.Args[2]
	
	// Create client with custom options
	options := &majordomo.ClientOptions{
		Timeout:   10 * time.Second,
		Retries:   3,
		LogErrors: true,
	}
	
	client := majordomo.NewClient("tcp://127.0.0.1:5555", options)
	
	// Connect to broker
	err := client.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to broker: %v", err)
	}
	defer client.Disconnect()
	
	log.Printf("Connected to Majordomo broker")
	
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