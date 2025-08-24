// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	
	"github.com/destiny/zmq4/malamute"
)

func main() {
	fmt.Println("Starting Malamute Enterprise Messaging Broker")
	fmt.Println("=============================================")
	
	// Create broker configuration
	config := &malamute.BrokerConfig{
		Name:               "enterprise-broker",
		Endpoint:           "tcp://*:9999",
		Verbose:            true,
		CreditWindow:       2000,
		HeartbeatInterval:  5 * time.Second,
		ClientTimeout:      60 * time.Second,
		ServiceTimeout:     30 * time.Second,
		MaxConnections:     1000,
		MaxStreams:         500,
		MaxMailboxes:       10000,
		MaxServices:        200,
		StreamRetention:    24 * time.Hour,
		MailboxRetention:   7 * 24 * time.Hour,
		EnablePersistence:  false,
		DataDirectory:      "./data",
	}
	
	// Create security configuration (using CURVE for this example)
	securityConfig := &malamute.SecurityConfig{
		Mechanism: "null", // Use null for simplicity in example
	}
	
	// Create secure broker
	broker, err := malamute.ApplyBrokerSecurity(config, securityConfig)
	if err != nil {
		log.Fatalf("Failed to create broker: %v", err)
	}
	
	// Start the broker
	if err := broker.Start(); err != nil {
		log.Fatalf("Failed to start broker: %v", err)
	}
	
	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Statistics reporting goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				stats := broker.GetStatistics()
				fmt.Printf("\n=== Broker Statistics ===\n")
				fmt.Printf("Active Connections: %d\n", stats.ActiveConnections)
				fmt.Printf("Messages Received:  %d\n", stats.MessagesReceived)
				fmt.Printf("Messages Sent:      %d\n", stats.MessagesSent)
				fmt.Printf("Active Streams:     %d\n", stats.ActiveStreams)
				fmt.Printf("Active Mailboxes:   %d\n", stats.ActiveMailboxes)
				fmt.Printf("Active Services:    %d\n", stats.ActiveServices)
				fmt.Printf("Stream Messages:    %d\n", stats.StreamMessages)
				fmt.Printf("Mailbox Messages:   %d\n", stats.MailboxMessages)
				fmt.Printf("Service Requests:   %d\n", stats.ServiceRequests)
				fmt.Printf("Service Replies:    %d\n", stats.ServiceReplies)
				fmt.Printf("Credit Requests:    %d\n", stats.CreditRequests)
				fmt.Printf("Credit Confirms:    %d\n", stats.CreditConfirms)
				fmt.Printf("=======================\n")
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Client information reporting goroutine
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				clients := broker.GetClients()
				fmt.Printf("\n=== Connected Clients ===\n")
				if len(clients) == 0 {
					fmt.Println("No clients connected")
				} else {
					for id, client := range clients {
						fmt.Printf("Client: %s (Type: %d, Connected: %v)\n", 
							id, client.Type, client.ConnectedAt.Format("15:04:05"))
						fmt.Printf("  Messages Sent: %d, Received: %d\n", 
							client.MessagesSent, client.MessagesRecv)
						fmt.Printf("  Credit: %d, Last Activity: %v\n", 
							client.Credit, client.LastActivity.Format("15:04:05"))
					}
				}
				fmt.Printf("========================\n")
				
			case <-sigChan:
				return
			}
		}
	}()
	
	fmt.Printf("Broker started successfully on %s\n", config.Endpoint)
	fmt.Println("Supported patterns:")
	fmt.Println("  - PUB/SUB Streams: Real-time data distribution")
	fmt.Println("  - Mailboxes: Direct peer-to-peer messaging")
	fmt.Println("  - Services: Request-reply with load balancing")
	fmt.Println("  - Credit-based flow control for backpressure")
	fmt.Println("\nPress Ctrl+C to stop the broker...")
	
	// Wait for shutdown signal
	<-sigChan
	
	fmt.Println("\nShutting down broker...")
	broker.Stop()
	fmt.Println("Broker stopped gracefully")
}