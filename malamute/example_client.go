// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	
	"github.com/destiny/zmq4/malamute"
)

func main() {
	fmt.Println("Malamute Service Client & Mailbox Example")
	fmt.Println("========================================")
	
	// Create client configuration
	config := &malamute.ClientConfig{
		ClientID:          "enterprise-client",
		BrokerEndpoint:    "tcp://localhost:9999",
		HeartbeatInterval: 5 * time.Second,
		Timeout:           10 * time.Second,
		CreditWindow:      1000,
		MaxRetries:        3,
		RetryInterval:     1 * time.Second,
		Verbose:           true,
	}
	
	// Create security configuration
	securityConfig := &malamute.SecurityConfig{
		Mechanism: "null", // Use null for simplicity
	}
	
	// Create secure client
	client, err := malamute.ApplyClientSecurity(config, securityConfig)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	
	// Connect to broker
	fmt.Println("Connecting to broker...")
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect to broker: %v", err)
	}
	
	fmt.Printf("Connected to broker at %s\n", config.BrokerEndpoint)
	
	// Get service client and mailbox interfaces
	serviceClient := client.ServiceClient()
	mailbox := client.Mailbox("enterprise-client.inbox")
	
	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Service request examples
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		
		requestCounter := 0
		
		for {
			select {
			case <-ticker.C:
				requestCounter++
				
				// Demonstrate different service calls
				switch requestCounter % 6 {
				case 1:
					// Math service - addition
					mathRequest := map[string]interface{}{
						"operation":  "add",
						"a":          float64(10 + requestCounter),
						"b":          float64(5),
						"request_id": fmt.Sprintf("req-%d", requestCounter),
					}
					jsonData, _ := json.Marshal(mathRequest)
					
					tracker := fmt.Sprintf("math-add-%d", requestCounter)
					err := serviceClient.Request("math.service", "math.add", tracker, jsonData, 10*time.Second)
					if err != nil {
						log.Printf("Failed to send math request: %v", err)
					} else {
						fmt.Printf("🧮 Sent math request: add %.0f + %.0f (tracker: %s)\n", 
							mathRequest["a"], mathRequest["b"], tracker)
					}
					
				case 2:
					// Math service - division
					mathRequest := map[string]interface{}{
						"operation":  "divide",
						"a":          float64(100),
						"b":          float64(requestCounter),
						"request_id": fmt.Sprintf("req-%d", requestCounter),
					}
					jsonData, _ := json.Marshal(mathRequest)
					
					tracker := fmt.Sprintf("math-div-%d", requestCounter)
					err := serviceClient.Request("math.service", "math.divide", tracker, jsonData, 10*time.Second)
					if err != nil {
						log.Printf("Failed to send math request: %v", err)
					} else {
						fmt.Printf("🧮 Sent math request: divide %.0f / %.0f (tracker: %s)\n", 
							mathRequest["a"], mathRequest["b"], tracker)
					}
					
				case 3:
					// String service - uppercase
					stringRequest := map[string]string{
						"text": fmt.Sprintf("hello world %d", requestCounter),
					}
					jsonData, _ := json.Marshal(stringRequest)
					
					tracker := fmt.Sprintf("string-upper-%d", requestCounter)
					err := serviceClient.Request("string.service", "string.upper", tracker, jsonData, 10*time.Second)
					if err != nil {
						log.Printf("Failed to send string request: %v", err)
					} else {
						fmt.Printf("📝 Sent string request: uppercase '%s' (tracker: %s)\n", 
							stringRequest["text"], tracker)
					}
					
				case 4:
					// String service - reverse
					text := fmt.Sprintf("malamute%d", requestCounter)
					jsonData := fmt.Sprintf(`{"text": "%s"}`, text)
					
					tracker := fmt.Sprintf("string-reverse-%d", requestCounter)
					err := serviceClient.Request("string.service", "string.reverse", tracker, []byte(jsonData), 10*time.Second)
					if err != nil {
						log.Printf("Failed to send string request: %v", err)
					} else {
						fmt.Printf("📝 Sent string request: reverse '%s' (tracker: %s)\n", text, tracker)
					}
					
				case 5:
					// Simple math format
					simpleRequest := fmt.Sprintf("%d * 7", requestCounter)
					tracker := fmt.Sprintf("simple-math-%d", requestCounter)
					
					err := serviceClient.Request("math.service", "math.multiply", tracker, []byte(simpleRequest), 10*time.Second)
					if err != nil {
						log.Printf("Failed to send simple math request: %v", err)
					} else {
						fmt.Printf("🧮 Sent simple math request: '%s' (tracker: %s)\n", simpleRequest, tracker)
					}
					
				case 0:
					// Error case - division by zero
					mathRequest := map[string]interface{}{
						"operation":  "divide",
						"a":          float64(42),
						"b":          float64(0), // Division by zero
						"request_id": fmt.Sprintf("req-%d", requestCounter),
					}
					jsonData, _ := json.Marshal(mathRequest)
					
					tracker := fmt.Sprintf("math-error-%d", requestCounter)
					err := serviceClient.Request("math.service", "math.divide", tracker, jsonData, 10*time.Second)
					if err != nil {
						log.Printf("Failed to send math request: %v", err)
					} else {
						fmt.Printf("🧮 Sent error test request: divide by zero (tracker: %s)\n", tracker)
					}
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Mailbox messaging examples
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		
		messageCounter := 0
		
		for {
			select {
			case <-ticker.C:
				messageCounter++
				
				// Send messages to different mailboxes
				targets := []string{
					"admin.inbox",
					"notifications.queue",
					"enterprise-client.inbox", // Self-message
				}
				
				for _, target := range targets {
					message := fmt.Sprintf(`{
	"from": "enterprise-client",
	"to": "%s",
	"subject": "Test Message %d",
	"body": "This is a test message sent via Malamute mailbox system.",
	"timestamp": "%s",
	"priority": "normal"
}`, target, messageCounter, time.Now().Format(time.RFC3339))
					
					targetMailbox := client.Mailbox(target)
					tracker := fmt.Sprintf("msg-%s-%d", target, messageCounter)
					
					err := targetMailbox.SendWithTracker(
						fmt.Sprintf("Test Message %d", messageCounter),
						[]byte(message),
						tracker,
						30*time.Second,
					)
					
					if err != nil {
						log.Printf("Failed to send mailbox message to %s: %v", target, err)
					} else {
						fmt.Printf("📧 Sent mailbox message to %s (tracker: %s)\n", target, tracker)
					}
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Statistics and monitoring
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				fmt.Printf("\n📊 Client Statistics:\n")
				fmt.Printf("  Client ID: %s\n", config.ClientID)
				fmt.Printf("  Connected to: %s\n", config.BrokerEndpoint)
				fmt.Printf("  Credit: %d\n", client.GetCredit())
				fmt.Printf("  Services used: math.service, string.service\n")
				fmt.Printf("  Mailbox: %s\n", mailbox.address)
				fmt.Printf("  Uptime: %s\n", time.Since(time.Now().Add(-45*time.Second)).String())
				
				// Request more credit if needed
				if client.GetCredit() < 100 {
					fmt.Printf("  ⚠️  Requesting more credit...\n")
					if err := client.RequestCredit(500); err != nil {
						log.Printf("Failed to request credit: %v", err)
					} else {
						fmt.Printf("  ✅ Credit requested\n")
					}
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	fmt.Println("Service client and mailbox started:")
	fmt.Println("  🔧 Making service requests to:")
	fmt.Println("    - math.service (add, subtract, multiply, divide)")
	fmt.Println("    - string.service (upper, lower, length, reverse)")
	fmt.Println("  📧 Sending mailbox messages to various addresses")
	fmt.Println("  📊 Monitoring credit and connection status")
	fmt.Println("\nPress Ctrl+C to stop the client...")
	
	// Wait for shutdown signal
	<-sigChan
	
	fmt.Println("\nShutting down client...")
	client.Disconnect()
	fmt.Println("Client stopped gracefully")
}