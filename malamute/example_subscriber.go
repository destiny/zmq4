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
	fmt.Println("Malamute Stream Subscriber Example")
	fmt.Println("=================================")
	
	// Create client configuration
	config := &malamute.ClientConfig{
		ClientID:          "trading-subscriber",
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
	
	// Get subscriber interface
	subscriber := client.Subscriber()
	
	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Subscribe to market data streams
	fmt.Println("Subscribing to market data streams...")
	
	// Subscribe to all stock prices
	if err := subscriber.Subscribe("market.stream", "market.stocks.*"); err != nil {
		log.Fatalf("Failed to subscribe to market stream: %v", err)
	}
	
	// Subscribe to financial news
	if err := subscriber.Subscribe("news.stream", "news.financial"); err != nil {
		log.Fatalf("Failed to subscribe to news stream: %v", err)
	}
	
	// Message processing goroutine
	go func() {
		messageCount := 0
		stockPrices := make(map[string]float64)
		
		for {
			select {
			case message := <-subscriber.Messages():
				messageCount++
				
				fmt.Printf("\n--- Message #%d ---\n", messageCount)
				fmt.Printf("Stream: %s\n", message.Stream)
				fmt.Printf("Subject: %s\n", message.Subject)
				fmt.Printf("Sender: %s\n", message.Sender)
				fmt.Printf("Timestamp: %s\n", time.Unix(0, message.Timestamp).Format("15:04:05.000"))
				
				// Display attributes
				if len(message.Attributes) > 0 {
					fmt.Printf("Attributes:\n")
					for key, value := range message.Attributes {
						fmt.Printf("  %s: %s\n", key, value)
					}
				}
				
				// Parse and display message content
				fmt.Printf("Content:\n%s\n", string(message.Body))
				
				// Track stock prices for portfolio monitoring
				if symbol, exists := message.Attributes["symbol"]; exists {
					// In a real application, you would parse the JSON properly
					// For simplicity, we'll just track that we received the symbol
					stockPrices[symbol] = float64(messageCount) // Placeholder
					
					fmt.Printf("\n📊 Portfolio Update:\n")
					for stock := range stockPrices {
						fmt.Printf("  %s: Tracking\n", stock)
					}
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Statistics reporting goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		
		lastMessageCount := 0
		
		for {
			select {
			case <-ticker.C:
				// In a real implementation, you would track these statistics properly
				fmt.Printf("\n📈 Subscriber Statistics:\n")
				fmt.Printf("  Credit: %d\n", client.GetCredit())
				fmt.Printf("  Streams subscribed: 2\n")
				fmt.Printf("  Connected time: %s\n", time.Since(time.Now().Add(-30*time.Second)).String())
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Credit monitoring goroutine
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				credit := client.GetCredit()
				if credit < 50 {
					fmt.Printf("⚠️  Low credit (%d), requesting more...\n", credit)
					if err := client.RequestCredit(200); err != nil {
						log.Printf("Failed to request credit: %v", err)
					} else {
						fmt.Printf("✅ Credit requested successfully\n")
					}
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	fmt.Println("Subscriber started. Listening for messages on:")
	fmt.Println("  - market.stream (pattern: market.stocks.*)")
	fmt.Println("  - news.stream (pattern: news.financial)")
	fmt.Println("\nWaiting for messages... Press Ctrl+C to stop")
	
	// Wait for shutdown signal
	<-sigChan
	
	fmt.Println("\nShutting down subscriber...")
	client.Disconnect()
	fmt.Println("Subscriber stopped gracefully")
}