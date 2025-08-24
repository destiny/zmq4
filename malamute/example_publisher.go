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
	fmt.Println("Malamute Stream Publisher Example")
	fmt.Println("================================")
	
	// Create client configuration
	config := &malamute.ClientConfig{
		ClientID:          "financial-publisher",
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
	
	// Get publisher interface
	publisher := client.Publisher()
	
	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Request initial credit
	fmt.Println("Requesting credit from broker...")
	if err := client.RequestCredit(500); err != nil {
		log.Printf("Failed to request credit: %v", err)
	}
	
	// Publishing goroutine
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		
		priceCounter := 100.0
		
		for {
			select {
			case <-ticker.C:
				// Check credit before publishing
				credit := client.GetCredit()
				if credit < 10 {
					fmt.Printf("Low credit (%d), requesting more...\n", credit)
					if err := client.RequestCredit(100); err != nil {
						log.Printf("Failed to request credit: %v", err)
					}
				}
				
				// Publish stock prices to different subjects
				stocks := []struct {
					symbol string
					change float64
				}{
					{"AAPL", 0.5},
					{"GOOGL", -0.8},
					{"MSFT", 1.2},
					{"TSLA", -2.1},
					{"AMZN", 0.3},
				}
				
				for _, stock := range stocks {
					priceCounter += stock.change
					
					// Create message with attributes
					attributes := map[string]string{
						"symbol":    stock.symbol,
						"timestamp": fmt.Sprintf("%d", time.Now().UnixNano()),
						"market":    "NYSE",
						"priority":  "normal",
					}
					
					message := fmt.Sprintf(`{
	"symbol": "%s",
	"price": %.2f,
	"change": %.2f,
	"volume": %d,
	"timestamp": "%s"
}`, stock.symbol, priceCounter, stock.change, 
						1000+int(time.Now().UnixNano()%10000),
						time.Now().Format("2006-01-02T15:04:05Z"))
					
					subject := fmt.Sprintf("market.stocks.%s", stock.symbol)
					
					err := publisher.PublishWithAttributes("market.stream", subject, []byte(message), attributes)
					if err != nil {
						log.Printf("Failed to publish %s: %v", stock.symbol, err)
					} else {
						fmt.Printf("Published %s: $%.2f (change: %.2f)\n", 
							stock.symbol, priceCounter, stock.change)
					}
					
					// Small delay between publishes
					time.Sleep(100 * time.Millisecond)
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// News publishing goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		
		newsItems := []string{
			"Market opens higher on positive economic data",
			"Tech sector shows strong growth in Q4",
			"Federal Reserve maintains interest rates",
			"New regulatory changes affecting financial sector",
			"Major merger announcement drives stock prices",
		}
		
		newsCounter := 0
		
		for {
			select {
			case <-ticker.C:
				newsItem := newsItems[newsCounter%len(newsItems)]
				newsCounter++
				
				attributes := map[string]string{
					"category":  "financial",
					"priority":  "high",
					"timestamp": fmt.Sprintf("%d", time.Now().UnixNano()),
					"source":    "MarketNews",
				}
				
				message := fmt.Sprintf(`{
	"headline": "%s",
	"content": "Full news content would go here...",
	"category": "financial",
	"timestamp": "%s",
	"id": "news-%d"
}`, newsItem, time.Now().Format("2006-01-02T15:04:05Z"), newsCounter)
				
				err := publisher.PublishWithAttributes("news.stream", "news.financial", []byte(message), attributes)
				if err != nil {
					log.Printf("Failed to publish news: %v", err)
				} else {
					fmt.Printf("Published news: %s\n", newsItem)
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Credit monitoring goroutine
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				credit := client.GetCredit()
				fmt.Printf("Current credit: %d\n", credit)
				
			case <-sigChan:
				return
			}
		}
	}()
	
	fmt.Println("Publisher started. Publishing to streams:")
	fmt.Println("  - market.stream: Real-time stock prices")
	fmt.Println("  - news.stream: Financial news updates")
	fmt.Println("\nPress Ctrl+C to stop publishing...")
	
	// Wait for shutdown signal
	<-sigChan
	
	fmt.Println("\nShutting down publisher...")
	client.Disconnect()
	fmt.Println("Publisher stopped gracefully")
}