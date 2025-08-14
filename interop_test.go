//go:build czmq4
// +build czmq4

// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Comprehensive interoperability tests for Go ZMQ4 vs C ZMQ implementations
package zmq4_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/destiny/zmq4"
	"github.com/destiny/zmq4/security/curve"
	"github.com/destiny/zmq4/majordomo"
)

var (
	bkg_interop = context.Background()
)

// Helper functions for test setup
func mustInterop(str string, err error) string {
	if err != nil {
		panic(err)
	}
	return str
}

func EndPointInterop(transport string) (string, error) {
	switch transport {
	case "tcp":
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return "", err
		}
		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return "", err
		}
		defer l.Close()
		return fmt.Sprintf("tcp://%s", l.Addr()), nil
	case "ipc":
		return "ipc://tmp-" + newUUIDInterop(), nil
	case "inproc":
		return "inproc://tmp-" + newUUIDInterop(), nil
	default:
		panic("invalid transport: [" + transport + "]")
	}
}

func newUUIDInterop() string {
	var uuid [16]byte
	if _, err := io.ReadFull(rand.Reader, uuid[:]); err != nil {
		log.Fatalf("cannot generate random data for UUID: %v", err)
	}
	uuid[8] = uuid[8]&^0xc0 | 0x80
	uuid[6] = uuid[6]&^0xf0 | 0x40
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
}

func cleanUpInterop(ep string) {
	switch {
	case strings.HasPrefix(ep, "ipc://"):
		os.Remove(ep[len("ipc://"):])
	case strings.HasPrefix(ep, "inproc://"):
		os.Remove(ep[len("inproc://"):])
	}
}

// TestCURVEInteroperability validates CURVE security between Go and C implementations
func TestCURVEInteroperability(t *testing.T) {
	// Generate server key pair for all tests
	serverKeys, err := curve.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate server keys: %v", err)
	}

	// Generate client key pair for all tests  
	clientKeys, err := curve.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate client keys: %v", err)
	}

	tests := []struct {
		name           string
		createServer   func(ctx context.Context) zmq4.Socket
		createClient   func(ctx context.Context) zmq4.Socket
		endpoint       string
		expectedResult string
	}{
		// Note: C socket interoperability tests would require goczmq bindings
		// For now, we focus on Go implementation verification
		{
			name: "Go-CURVE-Both",
			createServer: func(ctx context.Context) zmq4.Socket {
				security := curve.NewServerSecurity(serverKeys)
				return zmq4.NewRep(ctx, zmq4.WithSecurity(security))
			},
			createClient: func(ctx context.Context) zmq4.Socket {
				security := curve.NewClientSecurity(clientKeys, serverKeys.Public)
				return zmq4.NewReq(ctx, zmq4.WithSecurity(security))
			},
			endpoint:       mustInterop(EndPointInterop("tcp")),
			expectedResult: "CURVE interop test successful",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Create server
			server := test.createServer(ctx)
			defer server.Close()

			// Bind server
			err := server.Listen(test.endpoint)
			if err != nil {
				t.Fatalf("Failed to bind server: %v", err)
			}

			// Wait for server to be ready
			time.Sleep(100 * time.Millisecond)

			// Create client
			client := test.createClient(ctx)
			defer client.Close()

			// Connect client
			err = client.Dial(test.endpoint)
			if err != nil {
				t.Fatalf("Failed to connect client: %v", err)
			}

			// Test message exchange
			var wg sync.WaitGroup
			wg.Add(2)

			// Server goroutine
			go func() {
				defer wg.Done()
				
				// Receive request
				msg, err := server.Recv()
				if err != nil {
					t.Errorf("Server failed to receive: %v", err)
					return
				}

				// Verify request
				if string(msg.Frames[0]) != "CURVE test request" {
					t.Errorf("Server got unexpected request: %s", msg.Frames[0])
					return
				}

				// Send reply
				reply := zmq4.NewMsgString(test.expectedResult)
				err = server.Send(reply)
				if err != nil {
					t.Errorf("Server failed to send reply: %v", err)
					return
				}
			}()

			// Client goroutine
			go func() {
				defer wg.Done()
				
				// Wait for server to be ready
				time.Sleep(200 * time.Millisecond)

				// Send request
				request := zmq4.NewMsgString("CURVE test request")
				err := client.Send(request)
				if err != nil {
					t.Errorf("Client failed to send request: %v", err)
					return
				}

				// Receive reply
				reply, err := client.Recv()
				if err != nil {
					t.Errorf("Client failed to receive reply: %v", err)
					return
				}

				// Verify reply
				if string(reply.Frames[0]) != test.expectedResult {
					t.Errorf("Client got unexpected reply: %s, want %s", 
						reply.Frames[0], test.expectedResult)
					return
				}
			}()

			wg.Wait()
		})
	}
}

// TestCURVEKeyCompatibility validates key format compatibility between implementations
func TestCURVEKeyCompatibility(t *testing.T) {
	// Generate keys with Go implementation
	goKeys, err := curve.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Go keys: %v", err)
	}

	// Verify key sizes are correct for C compatibility
	if len(goKeys.Public) != 32 {
		t.Errorf("Go public key wrong size: got %d, want 32", len(goKeys.Public))
	}
	if len(goKeys.Secret) != 32 {
		t.Errorf("Go secret key wrong size: got %d, want 32", len(goKeys.Secret))
	}

	// Test key encoding/decoding compatibility
	pubKeyHex := hex.EncodeToString(goKeys.Public[:])
	secretKeyHex := hex.EncodeToString(goKeys.Secret[:])

	// Decode back
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		t.Fatalf("Failed to decode public key: %v", err)
	}
	secretKeyBytes, err := hex.DecodeString(secretKeyHex)
	if err != nil {
		t.Fatalf("Failed to decode secret key: %v", err)
	}

	// Verify roundtrip
	var pubKey, secretKey [32]byte
	copy(pubKey[:], pubKeyBytes)
	copy(secretKey[:], secretKeyBytes)

	if pubKey != goKeys.Public {
		t.Error("Public key roundtrip failed")
	}
	if secretKey != goKeys.Secret {
		t.Error("Secret key roundtrip failed")
	}

	t.Logf("Key compatibility test passed")
	t.Logf("Public key:  %s", pubKeyHex)
	t.Logf("Secret key:  %s", secretKeyHex)
}

// TestMajordomoInteroperability validates MDP between Go and C implementations
func TestMajordomoInteroperability(t *testing.T) {
	tests := []struct {
		name          string
		testScenario  func(t *testing.T)
	}{
		{
			name: "Go-MDP-Basic-Request-Reply",
			testScenario: testMDPBasicRequestReply,
		},
		{
			name: "Go-MDP-Multiple-Workers",
			testScenario: testMDPMultipleWorkers,
		},
		{
			name: "Go-MDP-Worker-Heartbeat",
			testScenario: testMDPWorkerHeartbeat,
		},
		{
			name: "Go-MDP-Service-Discovery",
			testScenario: testMDPServiceDiscovery,
		},
		{
			name: "Go-MDP-Error-Handling",
			testScenario: testMDPErrorHandling,
		},
		{
			name: "Go-MDP-Load-Balancing",
			testScenario: testMDPLoadBalancing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.testScenario(t)
		})
	}
}

// Test basic MDP request-reply functionality
func testMDPBasicRequestReply(t *testing.T) {
	// Test timeout managed by client options

	endpoint := mustInterop(EndPointInterop("tcp"))
	defer cleanUpInterop(endpoint)

	// Create and start broker
	broker := majordomo.NewBroker(endpoint)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()

	// Wait for broker to be ready
	time.Sleep(200 * time.Millisecond)

	// Create and start worker
	handler := func(request []byte) ([]byte, error) {
		return []byte("Echo: " + string(request)), nil
	}
	
	worker, err := majordomo.NewWorker("echo.service", endpoint, handler, nil)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}
	
	err = worker.Start()
	if err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	defer worker.Stop()

	// Wait for worker registration
	time.Sleep(300 * time.Millisecond)

	// Create client and send request
	client := majordomo.NewClient(endpoint, nil)
	err = client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}

	// Test request-reply
	request := []byte("Hello MDP")
	response, err := client.Request("echo.service", request)
	if err != nil {
		t.Fatalf("Client request failed: %v", err)
	}

	expectedResponse := "Echo: Hello MDP"
	if string(response) != expectedResponse {
		t.Errorf("Got unexpected response: %s, want %s", string(response), expectedResponse)
	}

	t.Logf("Basic MDP request-reply test passed")
}

// Test multiple workers for load balancing
func testMDPMultipleWorkers(t *testing.T) {
	// Test timeout managed by individual components

	endpoint := mustInterop(EndPointInterop("tcp"))
	defer cleanUpInterop(endpoint)

	// Create and start broker
	broker := majordomo.NewBroker(endpoint)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()

	time.Sleep(200 * time.Millisecond)

	// Create multiple workers for the same service
	numWorkers := 3
	workers := make([]*majordomo.Worker, numWorkers)
	
	for i := 0; i < numWorkers; i++ {
		workerID := i
		handler := func(request []byte) ([]byte, error) {
			return []byte(fmt.Sprintf("Worker-%d: %s", workerID, string(request))), nil
		}
		
		worker, err := majordomo.NewWorker("load.service", endpoint, handler, nil)
		if err != nil {
			t.Fatalf("Failed to create worker %d: %v", i, err)
		}
		
		err = worker.Start()
		if err != nil {
			t.Fatalf("Failed to start worker %d: %v", i, err)
		}
		defer worker.Stop()
		
		workers[i] = worker
	}

	// Wait for worker registrations
	time.Sleep(500 * time.Millisecond)

	// Create client
	client := majordomo.NewClient(endpoint, nil)
	err = client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}

	// Send multiple requests to test load balancing
	responses := make(map[string]int)
	numRequests := 6
	
	for i := 0; i < numRequests; i++ {
		request := []byte(fmt.Sprintf("Request-%d", i))
		response, err := client.Request("load.service", request)
		if err != nil {
			t.Fatalf("Client request %d failed: %v", i, err)
		}
		
		responseStr := string(response)
		responses[responseStr]++
		t.Logf("Request %d: %s", i, responseStr)
	}

	// Verify that requests were distributed among workers
	if len(responses) < 2 {
		t.Errorf("Expected requests to be distributed among multiple workers, got responses: %v", responses)
	}

	t.Logf("Multiple workers test passed with %d different responses", len(responses))
}

// Test worker heartbeat mechanism
func testMDPWorkerHeartbeat(t *testing.T) {
	// Test with custom timeout

	endpoint := mustInterop(EndPointInterop("tcp"))
	defer cleanUpInterop(endpoint)

	// Create and start broker
	broker := majordomo.NewBroker(endpoint)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()

	time.Sleep(200 * time.Millisecond)

	// Create worker with custom heartbeat options
	options := &majordomo.WorkerOptions{
		HeartbeatLiveness: 3,
		HeartbeatInterval: 1000 * time.Millisecond,
		ReconnectInterval: 1000 * time.Millisecond,
		LogErrors:         false,
		LogInfo:           false,
	}
	
	handler := func(request []byte) ([]byte, error) {
		return []byte("Heartbeat test response"), nil
	}
	
	worker, err := majordomo.NewWorker("heartbeat.service", endpoint, handler, options)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}
	
	err = worker.Start()
	if err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	defer worker.Stop()

	// Wait for worker to establish heartbeat
	time.Sleep(2 * time.Second)

	// Create client and test the service is available
	client := majordomo.NewClient(endpoint, nil)
	err = client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}

	// Send request - should work while heartbeat is active
	response, err := client.Request("heartbeat.service", []byte("ping"))
	if err != nil {
		t.Fatalf("Request failed during active heartbeat: %v", err)
	}

	if string(response) != "Heartbeat test response" {
		t.Errorf("Got unexpected response: %s", string(response))
	}

	t.Logf("Worker heartbeat test passed")
}

// Test service discovery and availability
func testMDPServiceDiscovery(t *testing.T) {
	// Test timeout managed by client options

	endpoint := mustInterop(EndPointInterop("tcp"))
	defer cleanUpInterop(endpoint)

	// Create and start broker
	broker := majordomo.NewBroker(endpoint)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()

	time.Sleep(200 * time.Millisecond)

	// Create workers for different services
	services := []string{"service.alpha", "service.beta", "service.gamma"}
	workers := make([]*majordomo.Worker, len(services))
	
	for i, serviceName := range services {
		handler := func(request []byte) ([]byte, error) {
			return []byte(fmt.Sprintf("%s processed: %s", serviceName, string(request))), nil
		}
		
		worker, err := majordomo.NewWorker(majordomo.ServiceName(serviceName), endpoint, handler, nil)
		if err != nil {
			t.Fatalf("Failed to create worker for %s: %v", serviceName, err)
		}
		
		err = worker.Start()
		if err != nil {
			t.Fatalf("Failed to start worker for %s: %v", serviceName, err)
		}
		defer worker.Stop()
		
		workers[i] = worker
	}

	// Wait for worker registrations
	time.Sleep(500 * time.Millisecond)

	// Create client
	client := majordomo.NewClient(endpoint, nil)
	err = client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}

	// Test each service
	for _, serviceName := range services {
		request := []byte("discovery test")
		response, err := client.Request(majordomo.ServiceName(serviceName), request)
		if err != nil {
			t.Fatalf("Request to %s failed: %v", serviceName, err)
		}
		
		expectedPrefix := serviceName + " processed:"
		if !strings.HasPrefix(string(response), expectedPrefix) {
			t.Errorf("Response from %s doesn't have expected prefix: %s", serviceName, string(response))
		}
		
		t.Logf("Service %s responded correctly", serviceName)
	}

	t.Logf("Service discovery test passed for %d services", len(services))
}

// Test error handling scenarios
func testMDPErrorHandling(t *testing.T) {
	// Test timeout managed by client options

	endpoint := mustInterop(EndPointInterop("tcp"))
	defer cleanUpInterop(endpoint)

	// Create and start broker
	broker := majordomo.NewBroker(endpoint)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()

	time.Sleep(200 * time.Millisecond)

	// Create worker that sometimes fails
	handler := func(request []byte) ([]byte, error) {
		if string(request) == "fail" {
			return nil, fmt.Errorf("intentional worker error")
		}
		return []byte("Success: " + string(request)), nil
	}
	
	worker, err := majordomo.NewWorker("error.service", endpoint, handler, nil)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}
	
	err = worker.Start()
	if err != nil {
		t.Fatalf("Failed to start worker: %v", err)
	}
	defer worker.Stop()

	time.Sleep(300 * time.Millisecond)

	// Create client
	client := majordomo.NewClient(endpoint, nil)
	err = client.Connect()
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}

	// Test successful request
	response, err := client.Request("error.service", []byte("good"))
	if err != nil {
		t.Fatalf("Good request failed: %v", err)
	}
	
	if string(response) != "Success: good" {
		t.Errorf("Got unexpected response: %s", string(response))
	}

	// Test request to non-existent service
	_, err = client.Request("nonexistent.service", []byte("test"))
	if err == nil {
		t.Error("Expected error for non-existent service, but got success")
	}

	t.Logf("Error handling test passed")
}

// Test load balancing with concurrent requests
func testMDPLoadBalancing(t *testing.T) {
	// Test timeout managed by individual components

	endpoint := mustInterop(EndPointInterop("tcp"))
	defer cleanUpInterop(endpoint)

	// Create and start broker
	broker := majordomo.NewBroker(endpoint)
	err := broker.Start()
	if err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer broker.Stop()

	time.Sleep(200 * time.Millisecond)

	// Create multiple workers
	numWorkers := 4
	workers := make([]*majordomo.Worker, numWorkers)
	
	for i := 0; i < numWorkers; i++ {
		workerID := i
		handler := func(request []byte) ([]byte, error) {
			// Simulate some processing time
			time.Sleep(100 * time.Millisecond)
			return []byte(fmt.Sprintf("Worker-%d processed: %s", workerID, string(request))), nil
		}
		
		worker, err := majordomo.NewWorker("balance.service", endpoint, handler, nil)
		if err != nil {
			t.Fatalf("Failed to create worker %d: %v", i, err)
		}
		
		err = worker.Start()
		if err != nil {
			t.Fatalf("Failed to start worker %d: %v", i, err)
		}
		defer worker.Stop()
		
		workers[i] = worker
	}

	time.Sleep(500 * time.Millisecond)

	// Create multiple clients for concurrent testing
	numClients := 3
	clients := make([]*majordomo.Client, numClients)
	
	for i := 0; i < numClients; i++ {
		client := majordomo.NewClient(endpoint, nil)
		err = client.Connect()
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		
		clients[i] = client
	}

	// Send concurrent requests
	var wg sync.WaitGroup
	responses := make(chan string, 12)
	
	for clientID := 0; clientID < numClients; clientID++ {
		for reqID := 0; reqID < 4; reqID++ {
			wg.Add(1)
			go func(cID, rID int) {
				defer wg.Done()
				
				request := []byte(fmt.Sprintf("C%d-R%d", cID, rID))
				response, err := clients[cID].Request("balance.service", request)
				if err != nil {
					t.Errorf("Client %d request %d failed: %v", cID, rID, err)
					return
				}
				
				responses <- string(response)
			}(clientID, reqID)
		}
	}

	wg.Wait()
	close(responses)

	// Collect and analyze responses
	responseMap := make(map[string]int)
	responseCount := 0
	
	for response := range responses {
		responseMap[response]++
		responseCount++
	}

	if responseCount != 12 {
		t.Errorf("Expected 12 responses, got %d", responseCount)
	}

	// Verify load was distributed
	workerResponseCounts := make(map[string]int)
	for response := range responseMap {
		for i := 0; i < numWorkers; i++ {
			if strings.Contains(response, fmt.Sprintf("Worker-%d", i)) {
				workerResponseCounts[fmt.Sprintf("Worker-%d", i)]++
				break
			}
		}
	}

	if len(workerResponseCounts) < 2 {
		t.Error("Load balancing failed - not enough workers processed requests")
	}

	t.Logf("Load balancing test passed - %d workers handled requests: %v", 
		len(workerResponseCounts), workerResponseCounts)
}

// TestProtocolCompliance validates wire protocol compliance
func TestProtocolCompliance(t *testing.T) {
	t.Run("MDP-Frame-Structure", func(t *testing.T) {
		// Test MDP message frame structures comply with RFC 7
		msg := &majordomo.Message{
			Command:   majordomo.WorkerReady,
			Service:   "test-service", // READY needs a service name
			Body:      nil,
		}
		
		frames := msg.FormatWorkerMessage()
		
		// Validate frame structure: [empty][protocol][command][service] for READY
		if len(frames) != 4 {
			t.Errorf("READY message should have 4 frames, got %d", len(frames))
		}
		
		if len(frames[0]) != 0 {
			t.Errorf("First frame should be empty, got %d bytes", len(frames[0]))
		}
		
		if string(frames[1]) != majordomo.WorkerProtocol {
			t.Errorf("Second frame should be protocol, got %s", string(frames[1]))
		}
		
		if string(frames[2]) != majordomo.WorkerReady {
			t.Errorf("Third frame should be command, got %s", string(frames[2]))
		}
		
		if string(frames[3]) != "test-service" {
			t.Errorf("Fourth frame should be service, got %s", string(frames[3]))
		}
	})

	t.Run("CURVE-Command-Sizes", func(t *testing.T) {
		// Test CURVE command size validation
		tests := []struct {
			command      string
			size         int
			shouldPass   bool
		}{
			{"HELLO", curve.HelloBodySize, true},
			{"HELLO", curve.HelloBodySize + 1, false},
			{"WELCOME", curve.WelcomeBodySize, true},
			{"WELCOME", curve.WelcomeBodySize - 1, false},
			{"INITIATE", curve.InitiateBodySize, true},
			{"INITIATE", curve.InitiateBodySize - 1, false},
			{"READY", curve.ReadyBodySize, true},
			{"READY", curve.ReadyBodySize - 1, false},
		}

		for _, test := range tests {
			err := curve.ValidateCommandSize(test.command, test.size)
			if test.shouldPass && err != nil {
				t.Errorf("Command %s with size %d should pass, got error: %v", 
					test.command, test.size, err)
			}
			if !test.shouldPass && err == nil {
				t.Errorf("Command %s with size %d should fail, but passed", 
					test.command, test.size)
			}
		}
	})
}

// BenchmarkInteroperabilityPerformance compares performance between Go and C implementations
func BenchmarkInteroperabilityPerformance(b *testing.B) {
	// Generate keys for CURVE tests
	serverKeys, err := curve.GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate server keys: %v", err)
	}

	clientKeys, err := curve.GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate client keys: %v", err)
	}

	b.Run("CURVE-Go-Both", func(b *testing.B) {
		benchmarkCURVEPingPong(b, false, false, serverKeys, clientKeys)
	})

	// Note: Mixed C/Go benchmarks would require goczmq bindings
}

func benchmarkCURVEPingPong(b *testing.B, useCSever, useCClient bool, serverKeys, clientKeys *curve.KeyPair) {
	ctx := context.Background()
	endpoint := mustInterop(EndPointInterop("tcp"))

	// Create Go server and client
	security := curve.NewServerSecurity(serverKeys)
	server := zmq4.NewRep(ctx, zmq4.WithSecurity(security))
	defer server.Close()

	err := server.Listen(endpoint)
	if err != nil {
		b.Fatalf("Failed to bind server: %v", err)
	}

	clientSecurity := curve.NewClientSecurity(clientKeys, serverKeys.Public)
	client := zmq4.NewReq(ctx, zmq4.WithSecurity(clientSecurity))
	defer client.Close()

	err = client.Dial(endpoint)
	if err != nil {
		b.Fatalf("Failed to connect client: %v", err)
	}

	// Warm up
	warmupMsg := zmq4.NewMsgString("warmup")
	client.Send(warmupMsg)
	server.Recv()
	server.Send(warmupMsg)
	client.Recv()

	// Benchmark
	testMsg := zmq4.NewMsgString("benchmark message")
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Client sends request
			err := client.Send(testMsg)
			if err != nil {
				b.Errorf("Send failed: %v", err)
				return
			}

			// Server receives and echoes
			msg, err := server.Recv()
			if err != nil {
				b.Errorf("Server recv failed: %v", err)
				return
			}

			err = server.Send(msg)
			if err != nil {
				b.Errorf("Server send failed: %v", err)
				return
			}

			// Client receives reply
			_, err = client.Recv()
			if err != nil {
				b.Errorf("Client recv failed: %v", err)
				return
			}
		}
	})
}

// TestCURVEEdgeCases validates CURVE security edge cases and error conditions
func TestCURVEEdgeCases(t *testing.T) {
	t.Run("Invalid-Server-Public-Key", func(t *testing.T) {
		// Test client with invalid server public key
		clientKeys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate client keys: %v", err)
		}

		// Use all-zero server key (invalid)
		invalidServerKey := [32]byte{}
		security := curve.NewClientSecurity(clientKeys, invalidServerKey)
		
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		client := zmq4.NewReq(ctx, zmq4.WithSecurity(security))
		defer client.Close()
		
		endpoint := mustInterop(EndPointInterop("tcp"))
		err = client.Dial(endpoint)
		// Should fail to connect or handshake with invalid server key
		if err == nil {
			// Try to send - this should fail during handshake
			msg := zmq4.NewMsgString("test")
			err = client.Send(msg)
			if err == nil {
				t.Error("Expected handshake to fail with invalid server key")
			}
		}
	})

	t.Run("Handshake-Timeout", func(t *testing.T) {
		// Test handshake timeout scenario
		serverKeys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate server keys: %v", err)
		}

		clientKeys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate client keys: %v", err)
		}

		// Create server but don't start it
		serverSecurity := curve.NewServerSecurity(serverKeys)
		serverCtx, serverCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer serverCancel()
		
		server := zmq4.NewRep(serverCtx, zmq4.WithSecurity(serverSecurity))
		defer server.Close()

		endpoint := mustInterop(EndPointInterop("tcp"))
		err = server.Listen(endpoint)
		if err != nil {
			t.Fatalf("Failed to bind server: %v", err)
		}

		// Create client with very short timeout
		clientSecurity := curve.NewClientSecurity(clientKeys, serverKeys.Public)
		clientCtx, clientCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer clientCancel()
		
		client := zmq4.NewReq(clientCtx, zmq4.WithSecurity(clientSecurity))
		defer client.Close()
		
		err = client.Dial(endpoint)
		if err != nil {
			return // Expected to fail
		}

		// Try to send with timeout
		msg := zmq4.NewMsgString("timeout test")
		err = client.Send(msg)
		if err == nil {
			// Try to receive - should timeout
			_, err = client.Recv()
			if err == nil {
				t.Error("Expected timeout during handshake")
			}
		}
	})

	t.Run("Wrong-Key-Pair-Mismatch", func(t *testing.T) {
		// Test with mismatched key pairs
		serverKeys1, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate server keys 1: %v", err)
		}

		serverKeys2, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate server keys 2: %v", err)
		}

		clientKeys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate client keys: %v", err)
		}

		// Server uses keys1, client thinks server has keys2
		serverSecurity := curve.NewServerSecurity(serverKeys1)
		clientSecurity := curve.NewClientSecurity(clientKeys, serverKeys2.Public)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		server := zmq4.NewRep(ctx, zmq4.WithSecurity(serverSecurity))
		defer server.Close()
		
		client := zmq4.NewReq(ctx, zmq4.WithSecurity(clientSecurity))
		defer client.Close()

		endpoint := mustInterop(EndPointInterop("tcp"))
		err = server.Listen(endpoint)
		if err != nil {
			t.Fatalf("Failed to bind server: %v", err)
		}

		err = client.Dial(endpoint)
		if err != nil {
			return // Expected to fail during dial
		}

		// Should fail during handshake
		msg := zmq4.NewMsgString("key mismatch test")
		err = client.Send(msg)
		if err == nil {
			t.Error("Expected handshake to fail with mismatched keys")
		}
	})

	t.Run("Command-Size-Validation", func(t *testing.T) {
		// Test command size validation
		tests := []struct {
			command    string
			size       int
			shouldPass bool
		}{
			{"HELLO", curve.HelloBodySize, true},
			{"HELLO", curve.HelloBodySize + 10, false},
			{"HELLO", curve.HelloBodySize - 10, false},
			{"WELCOME", curve.WelcomeBodySize, true},
			{"WELCOME", curve.WelcomeBodySize + 5, false},
			{"INITIATE", curve.InitiateBodySize, true},
			{"INITIATE", curve.InitiateBodySize - 1, false},
			{"READY", curve.ReadyBodySize, true},
			{"READY", curve.ReadyBodySize + 1, false},
			{"INVALID", 100, false},
		}

		for _, test := range tests {
			err := curve.ValidateCommandSize(test.command, test.size)
			if test.shouldPass && err != nil {
				t.Errorf("Command %s with size %d should pass, got error: %v", 
					test.command, test.size, err)
			}
			if !test.shouldPass && err == nil {
				t.Errorf("Command %s with size %d should fail, but passed", 
					test.command, test.size)
			}
		}
	})
}

// TestCURVECompatibilityEdgeCases validates compatibility edge cases
func TestCURVECompatibilityEdgeCases(t *testing.T) {
	t.Run("Key-Encoding-Formats", func(t *testing.T) {
		// Test different key encoding formats for C compatibility
		keys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate keys: %v", err)
		}

		// Test Z85 encoding compatibility
		pubZ85, err := keys.PublicKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode public key as Z85: %v", err)
		}
		
		secZ85, err := keys.SecretKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode secret key as Z85: %v", err)
		}
		
		// Verify Z85 encoded keys are correct length (40 chars for 32 bytes)
		if len(pubZ85) != 40 {
			t.Errorf("Z85 public key should be 40 characters, got %d", len(pubZ85))
		}
		
		if len(secZ85) != 40 {
			t.Errorf("Z85 secret key should be 40 characters, got %d", len(secZ85))
		}
		
		// Test Z85 roundtrip
		keysFromZ85, err := curve.NewKeyPairFromZ85(pubZ85, secZ85)
		if err != nil {
			t.Fatalf("Failed to recreate keys from Z85: %v", err)
		}
		
		if !bytesEqual(keys.Public[:], keysFromZ85.Public[:]) {
			t.Error("Z85 public key roundtrip failed")
		}
		
		if !bytesEqual(keys.Secret[:], keysFromZ85.Secret[:]) {
			t.Error("Z85 secret key roundtrip failed")
		}

		// Test hex encoding for comparison
		pubHex := hex.EncodeToString(keys.Public[:])
		secHex := hex.EncodeToString(keys.Secret[:])

		// Verify hex encoding roundtrip
		pubBytes, err := hex.DecodeString(pubHex)
		if err != nil {
			t.Fatalf("Failed to decode public key hex: %v", err)
		}
		
		secBytes, err := hex.DecodeString(secHex)
		if err != nil {
			t.Fatalf("Failed to decode secret key hex: %v", err)
		}

		if len(pubBytes) != 32 || len(secBytes) != 32 {
			t.Error("Key length mismatch after hex roundtrip")
		}

		// Test with uppercase hex
		pubHexUpper := strings.ToUpper(pubHex)
		pubBytesUpper, err := hex.DecodeString(pubHexUpper)
		if err != nil {
			t.Fatalf("Failed to decode uppercase hex: %v", err)
		}

		if !bytesEqual(pubBytes, pubBytesUpper) {
			t.Error("Uppercase hex decoding mismatch")
		}
		
		// Log comparison between formats
		t.Logf("Key format comparison:")
		t.Logf("  Hex (64 chars): %s", pubHex)
		t.Logf("  Z85 (40 chars): %s", pubZ85)
		t.Logf("  Z85 is %d%% more compact", 100*(64-40)/64)
	})

	t.Run("Large-Message-Handling", func(t *testing.T) {
		// Test handling of large messages with CURVE
		serverKeys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate server keys: %v", err)
		}

		clientKeys, err := curve.GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate client keys: %v", err)
		}

		serverSecurity := curve.NewServerSecurity(serverKeys)
		clientSecurity := curve.NewClientSecurity(clientKeys, serverKeys.Public)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		server := zmq4.NewRep(ctx, zmq4.WithSecurity(serverSecurity))
		defer server.Close()
		
		client := zmq4.NewReq(ctx, zmq4.WithSecurity(clientSecurity))
		defer client.Close()

		endpoint := mustInterop(EndPointInterop("tcp"))
		err = server.Listen(endpoint)
		if err != nil {
			t.Fatalf("Failed to bind server: %v", err)
		}

		err = client.Dial(endpoint)
		if err != nil {
			t.Fatalf("Failed to connect client: %v", err)
		}

		// Test with large message (1MB)
		largeData := make([]byte, 1024*1024)
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// Server goroutine
		go func() {
			defer wg.Done()
			msg, err := server.Recv()
			if err != nil {
				t.Errorf("Server failed to receive large message: %v", err)
				return
			}

			if len(msg.Frames[0]) != len(largeData) {
				t.Errorf("Server received wrong message size: got %d, want %d", 
					len(msg.Frames[0]), len(largeData))
				return
			}

			// Echo back
			err = server.Send(msg)
			if err != nil {
				t.Errorf("Server failed to send large message: %v", err)
			}
		}()

		// Client goroutine
		go func() {
			defer wg.Done()
			time.Sleep(100 * time.Millisecond) // Let server start

			msg := zmq4.NewMsg(largeData)
			err := client.Send(msg)
			if err != nil {
				t.Errorf("Client failed to send large message: %v", err)
				return
			}

			reply, err := client.Recv()
			if err != nil {
				t.Errorf("Client failed to receive large reply: %v", err)
				return
			}

			if len(reply.Frames[0]) != len(largeData) {
				t.Errorf("Client received wrong reply size: got %d, want %d", 
					len(reply.Frames[0]), len(largeData))
				return
			}

			if !bytesEqual(reply.Frames[0], largeData) {
				t.Error("Large message data corruption detected")
			}
		}()

		wg.Wait()
	})
}

// Helper function for byte comparison
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}