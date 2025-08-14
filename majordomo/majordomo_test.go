// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package majordomo

import (
	"strings"
	"testing"
	"time"
)

func TestServiceName(t *testing.T) {
	// Test valid service names
	validNames := []ServiceName{"echo", "calculator", "file-service", "service.with.dots"}
	for _, name := range validNames {
		if err := name.Validate(); err != nil {
			t.Errorf("Valid service name %q failed validation: %v", name, err)
		}
	}
	
	// Test invalid service names
	invalidNames := []ServiceName{"", ServiceName(strings.Repeat("x", 256))}
	for _, name := range invalidNames {
		if err := name.Validate(); err == nil {
			t.Errorf("Invalid service name %q passed validation", name)
		}
	}
}

func TestMessageFormatting(t *testing.T) {
	// Test client request formatting
	service := ServiceName("echo")
	body := []byte("Hello, World!")
	
	msg := NewClientRequest(service, body)
	frames := msg.FormatClientRequest()
	
	expectedFrames := [][]byte{
		{},                      // Empty frame
		[]byte(ClientProtocol),  // Protocol
		[]byte(service),         // Service
		body,                    // Body
	}
	
	if len(frames) != len(expectedFrames) {
		t.Fatalf("Client request frame count mismatch: got %d, want %d", len(frames), len(expectedFrames))
	}
	
	for i, frame := range frames {
		if string(frame) != string(expectedFrames[i]) {
			t.Errorf("Client request frame %d mismatch: got %q, want %q", i, frame, expectedFrames[i])
		}
	}
	
	// Test worker message formatting
	workerMsg := NewWorkerMessage(WorkerReady, nil, []byte(service))
	workerFrames := workerMsg.FormatWorkerMessage()
	
	expectedWorkerFrames := [][]byte{
		{},                      // Empty frame
		[]byte(WorkerProtocol),  // Protocol
		[]byte(WorkerReady),     // Command
	}
	
	if len(workerFrames) != len(expectedWorkerFrames) {
		t.Fatalf("Worker message frame count mismatch: got %d, want %d", len(workerFrames), len(expectedWorkerFrames))
	}
	
	for i, frame := range workerFrames[:3] {
		if string(frame) != string(expectedWorkerFrames[i]) {
			t.Errorf("Worker message frame %d mismatch: got %q, want %q", i, frame, expectedWorkerFrames[i])
		}
	}
}

func TestMessageParsing(t *testing.T) {
	// Test client message parsing
	frames := [][]byte{
		{},                     // Empty frame
		[]byte(ClientProtocol), // Protocol
		[]byte("echo"),         // Service
		[]byte("test data"),    // Body
	}
	
	msg, err := ParseClientMessage(frames)
	if err != nil {
		t.Fatalf("Failed to parse client message: %v", err)
	}
	
	if msg.Service != "echo" {
		t.Errorf("Service mismatch: got %s, want echo", msg.Service)
	}
	
	if string(msg.Body) != "test data" {
		t.Errorf("Body mismatch: got %q, want %q", msg.Body, "test data")
	}
	
	// Test worker message parsing
	workerFrames := [][]byte{
		{},                     // Empty frame
		[]byte(WorkerProtocol), // Protocol
		[]byte(WorkerReady),    // Command
		[]byte("echo"),         // Service for READY
	}
	
	workerMsg, err := ParseWorkerMessage(workerFrames)
	if err != nil {
		t.Fatalf("Failed to parse worker message: %v", err)
	}
	
	if workerMsg.Command != WorkerReady {
		t.Errorf("Command mismatch: got %s, want %s", workerMsg.Command, WorkerReady)
	}
	
	if workerMsg.Service != "echo" {
		t.Errorf("Service mismatch: got %s, want echo", workerMsg.Service)
	}
}

func TestInvalidMessageParsing(t *testing.T) {
	// Test invalid client messages
	invalidClientMessages := [][][]byte{
		{},                                                                    // Too short
		{{}, []byte("WRONG")},                                                // Wrong protocol
		{{}, []byte(ClientProtocol)},                                         // Missing service
		{[]byte("x"), []byte(ClientProtocol), []byte("echo")},               // Non-empty first frame
	}
	
	for i, frames := range invalidClientMessages {
		_, err := ParseClientMessage(frames)
		if err == nil {
			t.Errorf("Invalid client message %d should have failed parsing", i)
		}
	}
	
	// Test invalid worker messages
	invalidWorkerMessages := [][][]byte{
		{},                                                   // Too short
		{{}, []byte("WRONG")},                               // Wrong protocol
		{{}, []byte(WorkerProtocol)},                        // Missing command
		{{}, []byte(WorkerProtocol), []byte("INVALID")},     // Invalid command
	}
	
	for i, frames := range invalidWorkerMessages {
		_, err := ParseWorkerMessage(frames)
		if err == nil {
			t.Errorf("Invalid worker message %d should have failed parsing", i)
		}
	}
}

func TestBrokerBasicOperations(t *testing.T) {
	// Test broker creation
	broker := NewBroker("tcp://127.0.0.1:5555", nil)
	if broker == nil {
		t.Fatal("Failed to create broker")
	}
	
	// Test configuration
	broker.SetHeartbeat(5, 1*time.Second)
	broker.SetRequestTimeout(10 * time.Second)
	
	// Test stats (should be empty initially)
	stats := broker.GetStats()
	if stats["total_requests"].(uint64) != 0 {
		t.Error("Initial broker stats should show zero requests")
	}
}

func TestClientBasicOperations(t *testing.T) {
	// Test client creation
	client := NewClient("tcp://127.0.0.1:5555", nil)
	if client == nil {
		t.Fatal("Failed to create client")
	}
	
	// Test stats (should be empty initially)
	stats := client.GetStats()
	if stats["total_requests"].(uint64) != 0 {
		t.Error("Initial client stats should show zero requests")
	}
	
	// Test with custom options
	options := &ClientOptions{
		Timeout:   5 * time.Second,
		Retries:   2,
		LogErrors: false,
	}
	
	client2 := NewClient("tcp://127.0.0.1:5556", options)
	if client2 == nil {
		t.Fatal("Failed to create client with options")
	}
}

func TestWorkerBasicOperations(t *testing.T) {
	// Test worker creation
	service := ServiceName("echo")
	handler := func(request []byte) ([]byte, error) {
		return request, nil // Echo back the request
	}
	
	worker, err := NewWorker(service, "tcp://127.0.0.1:5555", handler, nil)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}
	
	if worker.GetService() != service {
		t.Error("Worker service mismatch")
	}
	
	if worker.IsRunning() {
		t.Error("Worker should not be running initially")
	}
	
	if worker.IsConnected() {
		t.Error("Worker should not be connected initially")
	}
	
	// Test stats (should be empty initially)
	stats := worker.GetStats()
	if stats["total_requests"].(uint64) != 0 {
		t.Error("Initial worker stats should show zero requests")
	}
	
	// Test invalid worker creation
	_, err = NewWorker("", "tcp://127.0.0.1:5555", handler, nil)
	if err == nil {
		t.Error("Should fail to create worker with empty service name")
	}
	
	_, err = NewWorker(service, "tcp://127.0.0.1:5555", nil, nil)
	if err == nil {
		t.Error("Should fail to create worker with nil handler")
	}
}

func TestAsyncClientBasicOperations(t *testing.T) {
	// Test async client creation
	client := NewAsyncClient("tcp://127.0.0.1:5555", nil)
	if client == nil {
		t.Fatal("Failed to create async client")
	}
	
	// Test stats (should be empty initially)
	stats := client.GetStats()
	if stats["total_requests"].(uint64) != 0 {
		t.Error("Initial async client stats should show zero requests")
	}
	
	if stats["pending_requests"].(int) != 0 {
		t.Error("Initial async client should have no pending requests")
	}
}

// TestProtocolConstants verifies MDP protocol constants match specification
func TestProtocolConstants(t *testing.T) {
	if ClientProtocol != "MDPC01" {
		t.Errorf("Client protocol mismatch: got %s, want MDPC01", ClientProtocol)
	}
	
	if WorkerProtocol != "MDPW01" {
		t.Errorf("Worker protocol mismatch: got %s, want MDPW01", WorkerProtocol)
	}
	
	expectedCommands := map[string]string{
		"WorkerReady":      "\001",
		"WorkerRequest":    "\002",
		"WorkerReply":      "\003",
		"WorkerHeartbeat":  "\004",
		"WorkerDisconnect": "\005",
	}
	
	actualCommands := map[string]string{
		"WorkerReady":      WorkerReady,
		"WorkerRequest":    WorkerRequest,
		"WorkerReply":      WorkerReply,
		"WorkerHeartbeat":  WorkerHeartbeat,
		"WorkerDisconnect": WorkerDisconnect,
	}
	
	for name, expected := range expectedCommands {
		actual := actualCommands[name]
		if actual != expected {
			t.Errorf("Command %s mismatch: got %q, want %q", name, actual, expected)
		}
	}
}

// TestMessageAge tests message aging functionality
func TestMessageAge(t *testing.T) {
	msg := NewClientRequest("test", []byte("data"))
	
	// Should be very young
	if msg.Age() > time.Millisecond {
		t.Error("New message should have very small age")
	}
	
	// Wait a bit
	time.Sleep(10 * time.Millisecond)
	
	// Should have aged
	if msg.Age() < 5*time.Millisecond {
		t.Error("Message should have aged")
	}
	
	// Test expiration  
	ttl := 20 * time.Millisecond
	if msg.IsExpired(ttl) {
		t.Error("Message should not be expired yet")
	}
	
	time.Sleep(ttl + 5*time.Millisecond)
	
	if !msg.IsExpired(ttl) {
		t.Error("Message should be expired now")
	}
}

// Integration test - requires actual broker to be running
func TestIntegration(t *testing.T) {
	t.Skip("Integration test requires running broker - enable manually for testing")
	
	// This test would start a broker, worker, and client, then verify they work together
	endpoint := "tcp://127.0.0.1:15555"
	
	// Start broker
	broker := NewBroker(endpoint, nil)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()
	
	time.Sleep(100 * time.Millisecond) // Let broker start
	
	// Start worker
	worker, err := NewWorker("echo", endpoint, func(request []byte) ([]byte, error) {
		return append([]byte("ECHO: "), request...), nil
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}
	
	err = worker.Start()
	if err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	defer worker.Stop()
	
	time.Sleep(100 * time.Millisecond) // Let worker connect
	
	// Test client request
	client := NewClient(endpoint, nil)
	err = client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Disconnect()
	
	reply, err := client.Request("echo", []byte("Hello, World!"))
	if err != nil {
		t.Fatalf("Client request failed: %v", err)
	}
	
	expected := "ECHO: Hello, World!"
	if string(reply) != expected {
		t.Errorf("Reply mismatch: got %q, want %q", reply, expected)
	}
	
	// Verify stats
	brokerStats := broker.GetStats()
	if brokerStats["total_requests"].(uint64) != 1 {
		t.Error("Broker should show 1 request processed")
	}
	
	workerStats := worker.GetStats()
	if workerStats["total_requests"].(uint64) != 1 {
		t.Error("Worker should show 1 request processed")
	}
	
	clientStats := client.GetStats()
	if clientStats["total_requests"].(uint64) != 1 {
		t.Error("Client should show 1 request sent")
	}
}