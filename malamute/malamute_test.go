// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"testing"
	"time"
)

// Test protocol message marshaling/unmarshaling
func TestMessageMarshaling(t *testing.T) {
	t.Run("ConnectionOpen", func(t *testing.T) {
		original := &ConnectionOpenMessage{
			ClientID:   "test-client",
			ClientType: ClientTypeGeneral,
			Attributes: map[string]string{"key": "value"},
		}
		
		data, err := original.Marshal()
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		
		var decoded ConnectionOpenMessage
		if err := decoded.Unmarshal(data); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		
		if decoded.ClientID != original.ClientID {
			t.Errorf("ClientID mismatch: got %s, want %s", decoded.ClientID, original.ClientID)
		}
		
		if decoded.ClientType != original.ClientType {
			t.Errorf("ClientType mismatch: got %d, want %d", decoded.ClientType, original.ClientType)
		}
	})
	
	t.Run("StreamWrite", func(t *testing.T) {
		original := &StreamWriteMessage{
			Stream:     "test.stream",
			Subject:    "test.subject",
			Attributes: map[string]string{"priority": "high"},
			Body:       []byte("test message body"),
		}
		
		data, err := original.Marshal()
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		
		var decoded StreamWriteMessage
		if err := decoded.Unmarshal(data); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		
		if decoded.Stream != original.Stream {
			t.Errorf("Stream mismatch: got %s, want %s", decoded.Stream, original.Stream)
		}
		
		if decoded.Subject != original.Subject {
			t.Errorf("Subject mismatch: got %s, want %s", decoded.Subject, original.Subject)
		}
		
		if string(decoded.Body) != string(original.Body) {
			t.Errorf("Body mismatch: got %s, want %s", decoded.Body, original.Body)
		}
	})
	
	t.Run("ServiceRequest", func(t *testing.T) {
		original := &ServiceRequestMessage{
			Service:    "echo.service",
			Method:     "echo",
			Tracker:    "req-123",
			Timeout:    5000,
			Attributes: map[string]string{"retry": "3"},
			Body:       []byte("echo this message"),
		}
		
		data, err := original.Marshal()
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		
		var decoded ServiceRequestMessage
		if err := decoded.Unmarshal(data); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		
		if decoded.Service != original.Service {
			t.Errorf("Service mismatch: got %s, want %s", decoded.Service, original.Service)
		}
		
		if decoded.Method != original.Method {
			t.Errorf("Method mismatch: got %s, want %s", decoded.Method, original.Method)
		}
		
		if decoded.Tracker != original.Tracker {
			t.Errorf("Tracker mismatch: got %s, want %s", decoded.Tracker, original.Tracker)
		}
	})
}

// Test credit controller
func TestCreditController(t *testing.T) {
	controller := NewCreditController(1000)
	controller.Start()
	defer controller.Stop()
	
	t.Run("RequestCredit", func(t *testing.T) {
		response, err := controller.RequestCredit("client1", 100, 0)
		if err != nil {
			t.Fatalf("RequestCredit failed: %v", err)
		}
		
		if response.Granted <= 0 {
			t.Errorf("No credit granted: %d", response.Granted)
		}
		
		if response.Available != response.Granted {
			t.Errorf("Available credit mismatch: got %d, want %d", response.Available, response.Granted)
		}
	})
	
	t.Run("UseCredit", func(t *testing.T) {
		// First request credit
		response, err := controller.RequestCredit("client2", 200, 0)
		if err != nil {
			t.Fatalf("RequestCredit failed: %v", err)
		}
		
		// Use some credit
		err = controller.UseCredit("client2", 50)
		if err != nil {
			t.Fatalf("UseCredit failed: %v", err)
		}
		
		// Check remaining credit
		credit := controller.GetAvailableCredit("client2")
		expected := response.Granted - 50
		if credit != expected {
			t.Errorf("Available credit mismatch: got %d, want %d", credit, expected)
		}
	})
	
	t.Run("ConfirmCredit", func(t *testing.T) {
		// First request and use credit
		controller.RequestCredit("client3", 100, 0)
		controller.UseCredit("client3", 30)
		
		// Return some credit
		err := controller.ConfirmCredit("client3", 20, "completed")
		if err != nil {
			t.Fatalf("ConfirmCredit failed: %v", err)
		}
	})
}

// Test stream manager
func TestStreamManager(t *testing.T) {
	manager := NewStreamManager(10, time.Hour)
	manager.Start()
	defer manager.Stop()
	
	t.Run("WriteMessage", func(t *testing.T) {
		err := manager.WriteMessage("test.stream", "test.subject", "client1", []byte("test message"), nil)
		if err != nil {
			t.Fatalf("WriteMessage failed: %v", err)
		}
		
		// Verify stream was created
		streams := manager.GetStreams()
		if len(streams) != 1 {
			t.Errorf("Expected 1 stream, got %d", len(streams))
		}
		
		stream, exists := streams["test.stream"]
		if !exists {
			t.Fatal("Stream not found")
		}
		
		if len(stream.Messages) != 1 {
			t.Errorf("Expected 1 message, got %d", len(stream.Messages))
		}
	})
	
	t.Run("Subscribe", func(t *testing.T) {
		err := manager.Subscribe("client1", "test.stream", "*", nil)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}
		
		// Verify subscription was created
		stream, err := manager.GetStream("test.stream")
		if err != nil {
			t.Fatalf("GetStream failed: %v", err)
		}
		
		if len(stream.Subscribers) != 1 {
			t.Errorf("Expected 1 subscriber, got %d", len(stream.Subscribers))
		}
		
		_, exists := stream.Subscribers["client1"]
		if !exists {
			t.Error("Subscriber not found")
		}
	})
	
	t.Run("Unsubscribe", func(t *testing.T) {
		err := manager.Unsubscribe("client1", "test.stream")
		if err != nil {
			t.Fatalf("Unsubscribe failed: %v", err)
		}
		
		stream, err := manager.GetStream("test.stream")
		if err != nil {
			t.Fatalf("GetStream failed: %v", err)
		}
		
		if len(stream.Subscribers) != 0 {
			t.Errorf("Expected 0 subscribers, got %d", len(stream.Subscribers))
		}
	})
}

// Test mailbox manager
func TestMailboxManager(t *testing.T) {
	manager := NewMailboxManager(100, time.Hour)
	manager.Start()
	defer manager.Stop()
	
	t.Run("SendMessage", func(t *testing.T) {
		err := manager.SendMessage("test.mailbox", "test subject", "sender1", "track-123", []byte("test message"), nil, time.Minute)
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}
		
		// Verify mailbox was created
		mailboxes := manager.GetMailboxes()
		if len(mailboxes) != 1 {
			t.Errorf("Expected 1 mailbox, got %d", len(mailboxes))
		}
		
		mailbox, exists := mailboxes["test.mailbox"]
		if !exists {
			t.Fatal("Mailbox not found")
		}
		
		if len(mailbox.Messages) != 1 {
			t.Errorf("Expected 1 message, got %d", len(mailbox.Messages))
		}
	})
	
	t.Run("ReceiveMessage", func(t *testing.T) {
		// First send a message
		manager.SendMessage("client1.inbox", "hello", "sender2", "track-456", []byte("hello world"), nil, time.Minute)
		
		// Receive the message
		message, err := manager.ReceiveMessage("client1", "client1.inbox", nil)
		if err != nil {
			t.Fatalf("ReceiveMessage failed: %v", err)
		}
		
		if message == nil {
			t.Fatal("No message received")
		}
		
		if message.Subject != "hello" {
			t.Errorf("Subject mismatch: got %s, want hello", message.Subject)
		}
		
		if string(message.Body) != "hello world" {
			t.Errorf("Body mismatch: got %s, want 'hello world'", message.Body)
		}
	})
	
	t.Run("CreateMailbox", func(t *testing.T) {
		err := manager.CreateMailbox("custom.mailbox", "owner1", map[string]string{"type": "priority"})
		if err != nil {
			t.Fatalf("CreateMailbox failed: %v", err)
		}
		
		mailbox, err := manager.GetMailbox("custom.mailbox")
		if err != nil {
			t.Fatalf("GetMailbox failed: %v", err)
		}
		
		if mailbox.Owner != "owner1" {
			t.Errorf("Owner mismatch: got %s, want owner1", mailbox.Owner)
		}
	})
}

// Test service manager
func TestServiceManager(t *testing.T) {
	manager := NewServiceManager(10, 30*time.Second)
	manager.Start()
	defer manager.Stop()
	
	t.Run("RegisterWorker", func(t *testing.T) {
		err := manager.RegisterWorker("echo.service", "worker1", "*", map[string]string{"version": "1.0"})
		if err != nil {
			t.Fatalf("RegisterWorker failed: %v", err)
		}
		
		// Verify service was created
		services := manager.GetServices()
		if len(services) != 1 {
			t.Errorf("Expected 1 service, got %d", len(services))
		}
		
		service, exists := services["echo.service"]
		if !exists {
			t.Fatal("Service not found")
		}
		
		if len(service.Workers) != 1 {
			t.Errorf("Expected 1 worker, got %d", len(service.Workers))
		}
		
		_, exists = service.Workers["worker1"]
		if !exists {
			t.Error("Worker not found")
		}
	})
	
	t.Run("ProcessRequest", func(t *testing.T) {
		// First register a worker
		manager.RegisterWorker("calc.service", "worker2", "add", nil)
		
		// Process a request
		err := manager.ProcessRequest("calc.service", "add", "client1", "req-789", []byte("2+2"), nil, 5*time.Second)
		if err != nil {
			t.Fatalf("ProcessRequest failed: %v", err)
		}
		
		// Verify service statistics
		service, err := manager.GetService("calc.service")
		if err != nil {
			t.Fatalf("GetService failed: %v", err)
		}
		
		// Note: RequestCount is updated in the processing loop, so we just verify the service exists
		if service.Name != "calc.service" {
			t.Errorf("Service name mismatch: got %s, want calc.service", service.Name)
		}
	})
	
	t.Run("UnregisterWorker", func(t *testing.T) {
		// First register a worker
		manager.RegisterWorker("temp.service", "temp.worker", "*", nil)
		
		// Unregister the worker
		err := manager.UnregisterWorker("temp.service", "temp.worker")
		if err != nil {
			t.Fatalf("UnregisterWorker failed: %v", err)
		}
		
		// Verify worker was removed
		services := manager.GetServices()
		if service, exists := services["temp.service"]; exists {
			if len(service.Workers) != 0 {
				t.Errorf("Expected 0 workers, got %d", len(service.Workers))
			}
		}
	})
}

// Test broker functionality
func TestBroker(t *testing.T) {
	config := &BrokerConfig{
		Name:               "test-broker",
		Endpoint:           "tcp://127.0.0.1:9998", // Use different port for testing
		CreditWindow:       500,
		HeartbeatInterval:  time.Second,
		ClientTimeout:      5 * time.Second,
		ServiceTimeout:     10 * time.Second,
		MaxConnections:     100,
		MaxStreams:         50,
		MaxMailboxes:       200,
		MaxServices:        20,
		StreamRetention:    time.Hour,
		MailboxRetention:   24 * time.Hour,
	}
	
	broker, err := NewBroker(config)
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}
	
	t.Run("BrokerLifecycle", func(t *testing.T) {
		// Test broker start/stop lifecycle
		err = broker.Start()
		if err != nil {
			// Skip if we can't bind to the test port
			t.Skipf("Could not start broker (port may be in use): %v", err)
		}
		
		// Let broker run briefly
		time.Sleep(100 * time.Millisecond)
		
		// Get statistics
		stats := broker.GetStatistics()
		if stats == nil {
			t.Error("Statistics should not be nil")
		}
		
		// Stop broker
		broker.Stop()
		
		// Verify broker stopped
		if broker.state != 0 {
			t.Errorf("Broker state should be 0 (stopped), got %d", broker.state)
		}
	})
}

// Test client functionality
func TestClient(t *testing.T) {
	config := &ClientConfig{
		ClientID:          "test-client-123",
		BrokerEndpoint:    "tcp://127.0.0.1:9999",
		HeartbeatInterval: time.Second,
		Timeout:           2 * time.Second,
		CreditWindow:      100,
		MaxRetries:        2,
		RetryInterval:     500 * time.Millisecond,
	}
	
	client, err := NewMalamuteClient(config)
	if err != nil {
		t.Fatalf("NewMalamuteClient failed: %v", err)
	}
	
	t.Run("ClientLifecycle", func(t *testing.T) {
		// Test client creation and configuration
		if client.clientID != "test-client-123" {
			t.Errorf("Client ID mismatch: got %s, want test-client-123", client.clientID)
		}
		
		if client.state != 0 {
			t.Errorf("Initial state should be 0 (disconnected), got %d", client.state)
		}
		
		credit := client.GetCredit()
		if credit != 0 {
			t.Errorf("Initial credit should be 0, got %d", credit)
		}
	})
	
	t.Run("PublisherInterface", func(t *testing.T) {
		publisher := client.Publisher()
		if publisher == nil {
			t.Fatal("Publisher should not be nil")
		}
		
		if publisher.client != client {
			t.Error("Publisher client reference incorrect")
		}
		
		if client.clientType != ClientTypePublisher {
			t.Errorf("Client type should be Publisher (%d), got %d", ClientTypePublisher, client.clientType)
		}
	})
	
	t.Run("SubscriberInterface", func(t *testing.T) {
		subscriber := client.Subscriber()
		if subscriber == nil {
			t.Fatal("Subscriber should not be nil")
		}
		
		if subscriber.client != client {
			t.Error("Subscriber client reference incorrect")
		}
		
		if client.clientType != ClientTypeSubscriber {
			t.Errorf("Client type should be Subscriber (%d), got %d", ClientTypeSubscriber, client.clientType)
		}
	})
	
	t.Run("MailboxInterface", func(t *testing.T) {
		mailbox := client.Mailbox("test.address")
		if mailbox == nil {
			t.Fatal("Mailbox should not be nil")
		}
		
		if mailbox.address != "test.address" {
			t.Errorf("Mailbox address mismatch: got %s, want test.address", mailbox.address)
		}
	})
	
	t.Run("WorkerInterface", func(t *testing.T) {
		worker := client.Worker("test.service", "test.*")
		if worker == nil {
			t.Fatal("Worker should not be nil")
		}
		
		if worker.service != "test.service" {
			t.Errorf("Worker service mismatch: got %s, want test.service", worker.service)
		}
		
		if worker.pattern != "test.*" {
			t.Errorf("Worker pattern mismatch: got %s, want test.*", worker.pattern)
		}
		
		if client.clientType != ClientTypeWorker {
			t.Errorf("Client type should be Worker (%d), got %d", ClientTypeWorker, client.clientType)
		}
	})
	
	t.Run("ServiceClientInterface", func(t *testing.T) {
		serviceClient := client.ServiceClient()
		if serviceClient == nil {
			t.Fatal("ServiceClient should not be nil")
		}
		
		if serviceClient.client != client {
			t.Error("ServiceClient client reference incorrect")
		}
		
		if client.clientType != ClientTypeRequester {
			t.Errorf("Client type should be Requester (%d), got %d", ClientTypeRequester, client.clientType)
		}
	})
	
	// Note: We don't test actual connection here since that would require a running broker
	// Integration tests would handle broker-client communication
}

// Test security mechanisms
func TestSecurity(t *testing.T) {
	t.Run("NullSecurity", func(t *testing.T) {
		security := NewNullSecurity()
		if security == nil {
			t.Fatal("NullSecurity should not be nil")
		}
		
		mechanism := security.GetMechanism()
		if mechanism != zmq4.NullSecurity {
			t.Errorf("Mechanism mismatch: got %d, want %d", mechanism, zmq4.NullSecurity)
		}
		
		creds := security.GetCredentials()
		if len(creds) != 0 {
			t.Errorf("NULL security should have no credentials, got %d", len(creds))
		}
	})
	
	t.Run("PlainSecurity", func(t *testing.T) {
		security := NewPlainSecurity("testuser", "testpass")
		if security == nil {
			t.Fatal("PlainSecurity should not be nil")
		}
		
		mechanism := security.GetMechanism()
		if mechanism != zmq4.PlainSecurity {
			t.Errorf("Mechanism mismatch: got %d, want %d", mechanism, zmq4.PlainSecurity)
		}
		
		creds := security.GetCredentials()
		if creds["username"] != "testuser" {
			t.Errorf("Username mismatch: got %s, want testuser", creds["username"])
		}
		
		if creds["password"] != "testpass" {
			t.Errorf("Password mismatch: got %s, want testpass", creds["password"])
		}
	})
	
	t.Run("CurveSecurity", func(t *testing.T) {
		// Use sample CURVE keys (40 character hex strings)
		serverKey := "1234567890123456789012345678901234567890"
		publicKey := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
		secretKey := "9876543210987654321098765432109876543210"
		
		security := NewCurveSecurity(serverKey, publicKey, secretKey)
		if security == nil {
			t.Fatal("CurveSecurity should not be nil")
		}
		
		mechanism := security.GetMechanism()
		if mechanism != zmq4.CurveSecurity {
			t.Errorf("Mechanism mismatch: got %d, want %d", mechanism, zmq4.CurveSecurity)
		}
		
		creds := security.GetCredentials()
		if creds["server_key"] != serverKey {
			t.Errorf("Server key mismatch: got %s, want %s", creds["server_key"], serverKey)
		}
		
		if creds["public_key"] != publicKey {
			t.Errorf("Public key mismatch: got %s, want %s", creds["public_key"], publicKey)
		}
		
		if creds["secret_key"] != secretKey {
			t.Errorf("Secret key mismatch: got %s, want %s", creds["secret_key"], secretKey)
		}
	})
}

// Test security configuration
func TestSecurityConfig(t *testing.T) {
	t.Run("NullConfig", func(t *testing.T) {
		config := &SecurityConfig{Mechanism: "null"}
		security, err := CreateSecurityFromConfig(config)
		if err != nil {
			t.Fatalf("CreateSecurityFromConfig failed: %v", err)
		}
		
		if security.GetMechanism() != zmq4.NullSecurity {
			t.Error("Expected NULL security mechanism")
		}
	})
	
	t.Run("PlainConfig", func(t *testing.T) {
		config := &SecurityConfig{
			Mechanism:     "plain",
			PlainUsername: "testuser",
			PlainPassword: "testpass",
		}
		security, err := CreateSecurityFromConfig(config)
		if err != nil {
			t.Fatalf("CreateSecurityFromConfig failed: %v", err)
		}
		
		if security.GetMechanism() != zmq4.PlainSecurity {
			t.Error("Expected PLAIN security mechanism")
		}
		
		creds := security.GetCredentials()
		if creds["username"] != "testuser" {
			t.Errorf("Username mismatch: got %s, want testuser", creds["username"])
		}
	})
	
	t.Run("CurveConfig", func(t *testing.T) {
		config := &SecurityConfig{
			Mechanism:      "curve",
			CurveServerKey: "1234567890123456789012345678901234567890",
			CurvePublicKey: "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
			CurveSecretKey: "9876543210987654321098765432109876543210",
		}
		security, err := CreateSecurityFromConfig(config)
		if err != nil {
			t.Fatalf("CreateSecurityFromConfig failed: %v", err)
		}
		
		if security.GetMechanism() != zmq4.CurveSecurity {
			t.Error("Expected CURVE security mechanism")
		}
	})
	
	t.Run("InvalidConfig", func(t *testing.T) {
		config := &SecurityConfig{Mechanism: "invalid"}
		_, err := CreateSecurityFromConfig(config)
		if err == nil {
			t.Error("Expected error for invalid mechanism")
		}
	})
	
	t.Run("ValidateConfig", func(t *testing.T) {
		// Valid NULL config
		err := ValidateSecurityConfig(&SecurityConfig{Mechanism: "null"})
		if err != nil {
			t.Errorf("NULL config should be valid: %v", err)
		}
		
		// Valid PLAIN config
		err = ValidateSecurityConfig(&SecurityConfig{
			Mechanism:     "plain",
			PlainUsername: "user",
		})
		if err != nil {
			t.Errorf("PLAIN config should be valid: %v", err)
		}
		
		// Invalid PLAIN config (no username)
		err = ValidateSecurityConfig(&SecurityConfig{
			Mechanism: "plain",
		})
		if err == nil {
			t.Error("PLAIN config without username should be invalid")
		}
		
		// Valid CURVE config
		err = ValidateSecurityConfig(&SecurityConfig{
			Mechanism:      "curve",
			CurveServerKey: "1234567890123456789012345678901234567890",
			CurvePublicKey: "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
			CurveSecretKey: "9876543210987654321098765432109876543210",
		})
		if err != nil {
			t.Errorf("CURVE config should be valid: %v", err)
		}
		
		// Invalid CURVE config (short keys)
		err = ValidateSecurityConfig(&SecurityConfig{
			Mechanism:      "curve",
			CurveServerKey: "short",
			CurvePublicKey: "keys",
			CurveSecretKey: "invalid",
		})
		if err == nil {
			t.Error("CURVE config with short keys should be invalid")
		}
	})
}

// Benchmark tests
func BenchmarkMessageMarshaling(b *testing.B) {
	msg := &StreamWriteMessage{
		Stream:     "benchmark.stream",
		Subject:    "benchmark.subject",
		Attributes: map[string]string{"priority": "normal"},
		Body:       []byte("This is a benchmark message for performance testing"),
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		data, _ := msg.Marshal()
		var decoded StreamWriteMessage
		decoded.Unmarshal(data)
	}
}

func BenchmarkCreditRequest(b *testing.B) {
	controller := NewCreditController(10000)
	controller.Start()
	defer controller.Stop()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		clientID := fmt.Sprintf("client-%d", i%100) // Simulate 100 clients
		controller.RequestCredit(clientID, 10, 0)
	}
}

// Helper function to create test context with timeout
func createTestContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}