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
	"strconv"
	"strings"
	"syscall"
	"time"
	
	"github.com/destiny/zmq4/malamute"
)

// CalculationRequest represents a calculation request
type CalculationRequest struct {
	Operation string  `json:"operation"`
	A         float64 `json:"a"`
	B         float64 `json:"b"`
	RequestID string  `json:"request_id"`
}

// CalculationResponse represents a calculation response
type CalculationResponse struct {
	Result    float64 `json:"result"`
	Operation string  `json:"operation"`
	RequestID string  `json:"request_id"`
	Error     string  `json:"error,omitempty"`
	ProcessedAt string `json:"processed_at"`
	WorkerID  string `json:"worker_id"`
}

func main() {
	fmt.Println("Malamute Service Worker Example")
	fmt.Println("==============================")
	
	// Create client configuration
	config := &malamute.ClientConfig{
		ClientID:          "calc-worker-01",
		BrokerEndpoint:    "tcp://localhost:9999",
		HeartbeatInterval: 5 * time.Second,
		Timeout:           10 * time.Second,
		CreditWindow:      500,
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
	
	// Create service workers for different calculation services
	mathWorker := client.Worker("math.service", "math.*")
	stringWorker := client.Worker("string.service", "string.*")
	
	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Start math service worker
	fmt.Println("Starting math service worker...")
	if err := mathWorker.Start(); err != nil {
		log.Fatalf("Failed to start math worker: %v", err)
	}
	
	// Start string service worker
	fmt.Println("Starting string service worker...")
	if err := stringWorker.Start(); err != nil {
		log.Fatalf("Failed to start string worker: %v", err)
	}
	
	// Math service request processing goroutine
	go func() {
		requestCount := 0
		
		for {
			select {
			case request := <-mathWorker.Requests():
				requestCount++
				fmt.Printf("\n🧮 Math Request #%d\n", requestCount)
				fmt.Printf("Service: %s\n", request.Service)
				fmt.Printf("Method: %s\n", request.Method)
				fmt.Printf("Sender: %s\n", request.Sender)
				fmt.Printf("Tracker: %s\n", request.Tracker)
				
				// Process the math request
				result, status := processMathRequest(request.Method, string(request.Body))
				
				// Send reply
				var responseBody []byte
				if result != nil {
					responseBody, _ = json.Marshal(result)
				} else {
					errorResponse := CalculationResponse{
						Error:       "Invalid request format",
						Operation:   request.Method,
						ProcessedAt: time.Now().Format(time.RFC3339),
						WorkerID:    config.ClientID,
					}
					responseBody, _ = json.Marshal(errorResponse)
				}
				
				if err := mathWorker.Reply(request.Tracker, status, responseBody); err != nil {
					log.Printf("Failed to send math reply: %v", err)
				} else {
					fmt.Printf("✅ Sent math reply for tracker: %s\n", request.Tracker)
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// String service request processing goroutine
	go func() {
		requestCount := 0
		
		for {
			select {
			case request := <-stringWorker.Requests():
				requestCount++
				fmt.Printf("\n📝 String Request #%d\n", requestCount)
				fmt.Printf("Service: %s\n", request.Service)
				fmt.Printf("Method: %s\n", request.Method)
				fmt.Printf("Sender: %s\n", request.Sender)
				fmt.Printf("Tracker: %s\n", request.Tracker)
				
				// Process the string request
				result, status := processStringRequest(request.Method, string(request.Body))
				
				// Send reply
				responseBody := []byte(result)
				if err := stringWorker.Reply(request.Tracker, status, responseBody); err != nil {
					log.Printf("Failed to send string reply: %v", err)
				} else {
					fmt.Printf("✅ Sent string reply for tracker: %s\n", request.Tracker)
				}
				
			case <-sigChan:
				return
			}
		}
	}()
	
	// Statistics reporting goroutine
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				fmt.Printf("\n📊 Worker Statistics:\n")
				fmt.Printf("  Worker ID: %s\n", config.ClientID)
				fmt.Printf("  Services offered: math.service, string.service\n")
				fmt.Printf("  Credit: %d\n", client.GetCredit())
				fmt.Printf("  Uptime: %s\n", time.Since(time.Now().Add(-60*time.Second)).String())
				
			case <-sigChan:
				return
			}
		}
	}()
	
	fmt.Println("Service workers started and ready to process requests:")
	fmt.Println("  📊 math.service (pattern: math.*)")
	fmt.Println("    - math.add: Add two numbers")
	fmt.Println("    - math.subtract: Subtract two numbers") 
	fmt.Println("    - math.multiply: Multiply two numbers")
	fmt.Println("    - math.divide: Divide two numbers")
	fmt.Println("  📝 string.service (pattern: string.*)")
	fmt.Println("    - string.upper: Convert to uppercase")
	fmt.Println("    - string.lower: Convert to lowercase")
	fmt.Println("    - string.length: Get string length")
	fmt.Println("    - string.reverse: Reverse string")
	fmt.Println("\nPress Ctrl+C to stop the workers...")
	
	// Wait for shutdown signal
	<-sigChan
	
	fmt.Println("\nShutting down service workers...")
	client.Disconnect()
	fmt.Println("Service workers stopped gracefully")
}

// processMathRequest processes a mathematical calculation request
func processMathRequest(method, body string) (*CalculationResponse, int) {
	var req CalculationRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		// Try to parse simple format like "5 + 3"
		return processSimpleMath(method, body)
	}
	
	response := CalculationResponse{
		Operation:   req.Operation,
		RequestID:   req.RequestID,
		ProcessedAt: time.Now().Format(time.RFC3339),
		WorkerID:    "calc-worker-01",
	}
	
	switch method {
	case "math.add":
		response.Result = req.A + req.B
		return &response, 200
	case "math.subtract":
		response.Result = req.A - req.B
		return &response, 200
	case "math.multiply":
		response.Result = req.A * req.B
		return &response, 200
	case "math.divide":
		if req.B == 0 {
			response.Error = "Division by zero"
			return &response, 400
		}
		response.Result = req.A / req.B
		return &response, 200
	default:
		response.Error = "Unknown operation: " + method
		return &response, 400
	}
}

// processSimpleMath processes simple math expressions like "5 + 3"
func processSimpleMath(method, body string) (*CalculationResponse, int) {
	response := CalculationResponse{
		Operation:   method,
		ProcessedAt: time.Now().Format(time.RFC3339),
		WorkerID:    "calc-worker-01",
	}
	
	parts := strings.Fields(strings.TrimSpace(body))
	if len(parts) != 3 {
		response.Error = "Invalid format. Expected: 'number operator number'"
		return &response, 400
	}
	
	a, err1 := strconv.ParseFloat(parts[0], 64)
	operator := parts[1]
	b, err2 := strconv.ParseFloat(parts[2], 64)
	
	if err1 != nil || err2 != nil {
		response.Error = "Invalid numbers"
		return &response, 400
	}
	
	switch operator {
	case "+":
		response.Result = a + b
		return &response, 200
	case "-":
		response.Result = a - b
		return &response, 200
	case "*":
		response.Result = a * b
		return &response, 200
	case "/":
		if b == 0 {
			response.Error = "Division by zero"
			return &response, 400
		}
		response.Result = a / b
		return &response, 200
	default:
		response.Error = "Unknown operator: " + operator
		return &response, 400
	}
}

// processStringRequest processes a string manipulation request
func processStringRequest(method, body string) (string, int) {
	input := strings.TrimSpace(body)
	
	// Try to parse as JSON first
	var jsonInput struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(body), &jsonInput); err == nil {
		input = jsonInput.Text
	}
	
	result := struct {
		Result      string `json:"result"`
		Method      string `json:"method"`
		Input       string `json:"input"`
		ProcessedAt string `json:"processed_at"`
		WorkerID    string `json:"worker_id"`
	}{
		Method:      method,
		Input:       input,
		ProcessedAt: time.Now().Format(time.RFC3339),
		WorkerID:    "calc-worker-01",
	}
	
	switch method {
	case "string.upper":
		result.Result = strings.ToUpper(input)
	case "string.lower":
		result.Result = strings.ToLower(input)
	case "string.length":
		result.Result = fmt.Sprintf("%d", len(input))
	case "string.reverse":
		runes := []rune(input)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		result.Result = string(runes)
	default:
		return fmt.Sprintf(`{"error": "Unknown string operation: %s"}`, method), 400
	}
	
	jsonResult, _ := json.Marshal(result)
	return string(jsonResult), 200
}