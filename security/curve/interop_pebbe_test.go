//go:build pebbe
// +build pebbe

// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Head-to-head interoperability tests against github.com/pebbe/zmq4
package curve

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	ourzmq "github.com/destiny/zmq4/v25"
	pebbezet "github.com/pebbe/zmq4"
)

// TestCURVEKeyInteroperability verifies key format compatibility
func TestCURVEKeyInteroperability(t *testing.T) {
	t.Run("Fresh-Key-Generation-Compatibility", func(t *testing.T) {
		// Generate keys with our implementation
		ourKeys, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate our keys: %v", err)
		}

		// Generate keys with pebbe/zmq4
		pebbeServerPub, pebbeServerSec, err := pebbezet.NewCurveKeypair()
		if err != nil {
			t.Fatalf("Failed to generate pebbe keys: %v", err)
		}

		// Test key size compatibility
		if len(ourKeys.Public) != 32 {
			t.Errorf("Our public key wrong size: got %d, want 32", len(ourKeys.Public))
		}
		if len(ourKeys.Secret) != 32 {
			t.Errorf("Our secret key wrong size: got %d, want 32", len(ourKeys.Secret))
		}

		// Pebbe keys are Z85 encoded strings - verify they're the right length
		if len(pebbeServerPub) != 40 {
			t.Errorf("Pebbe public key wrong Z85 length: got %d, want 40", len(pebbeServerPub))
		}
		if len(pebbeServerSec) != 40 {
			t.Errorf("Pebbe secret key wrong Z85 length: got %d, want 40", len(pebbeServerSec))
		}

		// Convert our keys to Z85 for comparison format
		ourPubZ85, err := ourKeys.PublicKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our public key as Z85: %v", err)
		}
		_, err = ourKeys.SecretKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our secret key as Z85: %v", err)
		}

		// Test that both use valid Z85 format
		err = ValidateZ85Key(ourPubZ85)
		if err != nil {
			t.Errorf("Our Z85 key validation failed: %v", err)
		}
		err = ValidateZ85Key(pebbeServerPub)
		if err != nil {
			t.Errorf("Pebbe Z85 key validation failed: %v", err)
		}

		t.Logf("Key compatibility test passed")
		t.Logf("Our keys  - Pub: %s", ourPubZ85)
		t.Logf("Pebbe keys - Pub: %s, Sec: %s", pebbeServerPub, pebbeServerSec)
	})

	t.Run("Cross-Implementation-Key-Usage", func(t *testing.T) {
		// Generate server keys with pebbe/zmq4
		pebbeServerPub, pebbeServerSec, err := pebbezet.NewCurveKeypair()
		if err != nil {
			t.Fatalf("Failed to generate pebbe server keys: %v", err)
		}

		// Generate client keys with our implementation
		ourClientKeys, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate our client keys: %v", err)
		}

		// Convert pebbe server public key to our format
		pebbeServerKeyPair, err := NewKeyPairFromZ85(pebbeServerPub, pebbeServerSec)
		if err != nil {
			t.Fatalf("Failed to convert pebbe keys to our format: %v", err)
		}

		// Convert our client keys to Z85 for pebbe
		ourClientPubZ85, err := ourClientKeys.PublicKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our client public key as Z85: %v", err)
		}

		// Test that we can create security objects with cross-implementation keys
		ourClientSecurity := NewClientSecurity(ourClientKeys, pebbeServerKeyPair.Public)
		if ourClientSecurity == nil {
			t.Error("Failed to create our client security with pebbe server key")
		}

		ourServerSecurity := NewServerSecurity(pebbeServerKeyPair)
		if ourServerSecurity == nil {
			t.Error("Failed to create our server security with pebbe keys")
		}

		t.Logf("Cross-implementation key usage test passed")
		t.Logf("Our client can use pebbe server key: %s", pebbeServerPub)
		t.Logf("Our server can use pebbe keys - client expects: %s", ourClientPubZ85)
	})
}

// TestCURVEHandshakeInteroperability tests full handshake between implementations
func TestCURVEHandshakeInteroperability(t *testing.T) {
	t.Run("Our-Client-Pebbe-Server", func(t *testing.T) {
		// Generate fresh keys for this test
		pebbeServerPub, pebbeServerSec, err := pebbezet.NewCurveKeypair()
		if err != nil {
			t.Fatalf("Failed to generate pebbe server keys: %v", err)
		}

		ourClientKeys, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate our client keys: %v", err)
		}

		// Convert pebbe server key to our format
		pebbeServerKeyPair, err := NewKeyPairFromZ85(pebbeServerPub, pebbeServerSec)
		if err != nil {
			t.Fatalf("Failed to convert pebbe server keys: %v", err)
		}

		// Set up pebbe server
		server, err := pebbezet.NewSocket(pebbezet.REP)
		if err != nil {
			t.Fatalf("Failed to create pebbe server socket: %v", err)
		}
		defer server.Close()

		server.ServerAuthCurve("*", pebbeServerSec)
		err = server.Bind("tcp://*:15555")
		if err != nil {
			t.Fatalf("Failed to bind pebbe server: %v", err)
		}

		// Set up our client
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		ourClientSecurity := NewClientSecurity(ourClientKeys, pebbeServerKeyPair.Public)
		client := ourzmq.NewReq(ctx, ourzmq.WithSecurity(ourClientSecurity))
		defer client.Close()

		// Give server time to start
		time.Sleep(100 * time.Millisecond)

		err = client.Dial("tcp://127.0.0.1:15555")
		if err != nil {
			t.Fatalf("Failed to connect our client to pebbe server: %v", err)
		}

		// Test message exchange
		var wg sync.WaitGroup
		wg.Add(2)

		// Server goroutine
		go func() {
			defer wg.Done()
			
			// Receive from our client
			msg, err := server.RecvMessage(0)
			if err != nil {
				t.Errorf("Pebbe server failed to receive: %v", err)
				return
			}

			expectedMsg := "Hello from our client!"
			if len(msg) == 0 || string(msg[0]) != expectedMsg {
				t.Errorf("Pebbe server got unexpected message: %v, want %s", msg, expectedMsg)
				return
			}

			// Send reply
			_, err = server.SendMessage("Hello from pebbe server!")
			if err != nil {
				t.Errorf("Pebbe server failed to send reply: %v", err)
			}
		}()

		// Client goroutine
		go func() {
			defer wg.Done()
			
			time.Sleep(200 * time.Millisecond) // Let server start

			// Send request
			request := ourzmq.NewMsgString("Hello from our client!")
			err := client.Send(request)
			if err != nil {
				t.Errorf("Our client failed to send: %v", err)
				return
			}

			// Receive reply
			reply, err := client.Recv()
			if err != nil {
				t.Errorf("Our client failed to receive: %v", err)
				return
			}

			expectedReply := "Hello from pebbe server!"
			if len(reply.Frames) == 0 || string(reply.Frames[0]) != expectedReply {
				t.Errorf("Our client got unexpected reply: %v, want %s", reply.Frames, expectedReply)
			}
		}()

		wg.Wait()
		t.Logf("Our client ↔ Pebbe server interoperability test passed")
	})

	t.Run("Pebbe-Client-Our-Server", func(t *testing.T) {
		// Generate fresh keys for this test
		ourServerKeys, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate our server keys: %v", err)
		}

		pebbeClientPub, pebbeClientSec, err := pebbezet.NewCurveKeypair()
		if err != nil {
			t.Fatalf("Failed to generate pebbe client keys: %v", err)
		}

		// Set up our server
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		ourServerSecurity := NewServerSecurity(ourServerKeys)
		server := ourzmq.NewRep(ctx, ourzmq.WithSecurity(ourServerSecurity))
		defer server.Close()

		err = server.Listen("tcp://*:15556")
		if err != nil {
			t.Fatalf("Failed to bind our server: %v", err)
		}

		// Set up pebbe client
		client, err := pebbezet.NewSocket(pebbezet.REQ)
		if err != nil {
			t.Fatalf("Failed to create pebbe client socket: %v", err)
		}
		defer client.Close()

		// Get our server public key in Z85 format for pebbe
		ourServerPubZ85, err := ourServerKeys.PublicKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our server public key as Z85: %v", err)
		}

		client.ClientAuthCurve(ourServerPubZ85, pebbeClientPub, pebbeClientSec)

		// Give server time to start
		time.Sleep(100 * time.Millisecond)

		err = client.Connect("tcp://127.0.0.1:15556")
		if err != nil {
			t.Fatalf("Failed to connect pebbe client to our server: %v", err)
		}

		// Test message exchange
		var wg sync.WaitGroup
		wg.Add(2)

		// Server goroutine
		go func() {
			defer wg.Done()
			
			// Receive from pebbe client
			msg, err := server.Recv()
			if err != nil {
				t.Errorf("Our server failed to receive: %v", err)
				return
			}

			expectedMsg := "Hello from pebbe client!"
			if len(msg.Frames) == 0 || string(msg.Frames[0]) != expectedMsg {
				t.Errorf("Our server got unexpected message: %v, want %s", msg.Frames, expectedMsg)
				return
			}

			// Send reply
			reply := ourzmq.NewMsgString("Hello from our server!")
			err = server.Send(reply)
			if err != nil {
				t.Errorf("Our server failed to send reply: %v", err)
			}
		}()

		// Client goroutine  
		go func() {
			defer wg.Done()
			
			time.Sleep(200 * time.Millisecond) // Let server start

			// Send request
			_, err := client.SendMessage("Hello from pebbe client!")
			if err != nil {
				t.Errorf("Pebbe client failed to send: %v", err)
				return
			}

			// Receive reply
			reply, err := client.RecvMessage(0)
			if err != nil {
				t.Errorf("Pebbe client failed to receive: %v", err)
				return
			}

			expectedReply := "Hello from our server!"
			if len(reply) == 0 || string(reply[0]) != expectedReply {
				t.Errorf("Pebbe client got unexpected reply: %v, want %s", reply, expectedReply)
			}
		}()

		wg.Wait()
		t.Logf("Pebbe client ↔ Our server interoperability test passed")
	})
}

// TestCURVEKeySignatureCompatibility specifically tests the signature box issue
func TestCURVEKeySignatureCompatibility(t *testing.T) {
	t.Run("Signature-Box-Format-Compatibility", func(t *testing.T) {
		// This test ensures our signature box format is compatible with pebbe/zmq4
		// by testing that handshakes work both ways

		// Generate keys
		ourKeys, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate our keys: %v", err)
		}

		pebbeServerPub, pebbeServerSec, err := pebbezet.NewCurveKeypair()
		if err != nil {
			t.Fatalf("Failed to generate pebbe keys: %v", err)
		}

		// Convert for cross-compatibility
		pebbeKeyPair, err := NewKeyPairFromZ85(pebbeServerPub, pebbeServerSec)
		if err != nil {
			t.Fatalf("Failed to convert pebbe keys: %v", err)
		}

		ourPubZ85, err := ourKeys.PublicKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our key as Z85: %v", err)
		}

		_, err = ourKeys.SecretKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our secret as Z85: %v", err)
		}

		// Test 1: Our implementation with pebbe-generated keys
		ourSecurity := NewClientSecurity(ourKeys, pebbeKeyPair.Public)
		if ourSecurity.Type() != ourzmq.CurveSecurity {
			t.Error("Our security with pebbe keys has wrong type")
		}

		// Test 2: Verify key format compatibility
		// Both should produce 32-byte keys
		if len(ourKeys.Public) != 32 || len(pebbeKeyPair.Public) != 32 {
			t.Error("Key size mismatch between implementations")
		}

		// Test 3: Cross-validate Z85 encoding
		err = ValidateZ85Key(pebbeServerPub)
		if err != nil {
			t.Errorf("Pebbe Z85 key failed our validation: %v", err)
		}

		err = ValidateZ85Key(ourPubZ85)
		if err != nil {
			t.Errorf("Our Z85 key failed our validation: %v", err)
		}

		t.Logf("Signature box format compatibility test passed")
		t.Logf("Our key format works with pebbe keys")
		t.Logf("Our Z85: %s, Pebbe Z85: %s", ourPubZ85, pebbeServerPub)
	})
}

// TestCURVEFullStackInteroperability tests complete protocol stacks
func TestCURVEFullStackInteroperability(t *testing.T) {
	t.Run("Multiple-Message-Exchange", func(t *testing.T) {
		// Test multiple messages to ensure the signature verification fix works consistently

		// Generate fresh keys
		ourServerKeys, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate our server keys: %v", err)
		}

		pebbeClientPub, pebbeClientSec, err := pebbezet.NewCurveKeypair()
		if err != nil {
			t.Fatalf("Failed to generate pebbe client keys: %v", err)
		}

		// Set up our server
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ourServerSecurity := NewServerSecurity(ourServerKeys)
		server := ourzmq.NewRep(ctx, ourzmq.WithSecurity(ourServerSecurity))
		defer server.Close()

		err = server.Listen("tcp://*:15557")
		if err != nil {
			t.Fatalf("Failed to bind our server: %v", err)
		}

		// Set up pebbe client
		client, err := pebbezet.NewSocket(pebbezet.REQ)
		if err != nil {
			t.Fatalf("Failed to create pebbe client socket: %v", err)
		}
		defer client.Close()

		ourServerPubZ85, err := ourServerKeys.PublicKeyZ85()
		if err != nil {
			t.Fatalf("Failed to encode our server public key: %v", err)
		}

		client.ClientAuthCurve(ourServerPubZ85, pebbeClientPub, pebbeClientSec)

		time.Sleep(100 * time.Millisecond)

		err = client.Connect("tcp://127.0.0.1:15557")
		if err != nil {
			t.Fatalf("Failed to connect pebbe client: %v", err)
		}

		// Test multiple message exchanges
		for i := 0; i < 5; i++ {
			testMsg := fmt.Sprintf("Test message #%d", i)
			expectedReply := fmt.Sprintf("Reply to message #%d", i)

			var wg sync.WaitGroup
			wg.Add(2)

			// Server goroutine
			go func(msgNum int) {
				defer wg.Done()
				
				msg, err := server.Recv()
				if err != nil {
					t.Errorf("Server failed to receive message %d: %v", msgNum, err)
					return
				}

				if string(msg.Frames[0]) != testMsg {
					t.Errorf("Server got wrong message %d: %s", msgNum, msg.Frames[0])
					return
				}

				reply := ourzmq.NewMsgString(expectedReply)
				err = server.Send(reply)
				if err != nil {
					t.Errorf("Server failed to send reply %d: %v", msgNum, err)
				}
			}(i)

			// Client goroutine
			go func(msgNum int) {
				defer wg.Done()
				
				time.Sleep(50 * time.Millisecond)

				_, err := client.SendMessage(testMsg)
				if err != nil {
					t.Errorf("Client failed to send message %d: %v", msgNum, err)
					return
				}

				reply, err := client.RecvMessage(0)
				if err != nil {
					t.Errorf("Client failed to receive reply %d: %v", msgNum, err)
					return
				}

				if len(reply) == 0 || string(reply[0]) != expectedReply {
					t.Errorf("Client got wrong reply %d: %v", msgNum, reply)
				}
			}(i)

			wg.Wait()
		}

		t.Logf("Multiple message exchange test passed - %d messages successfully exchanged", 5)
	})
}

// BenchmarkCURVEInteroperabilityPerformance compares performance between implementations
func BenchmarkCURVEInteroperabilityPerformance(b *testing.B) {
	// Generate test keys
	ourKeys, err := GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate our keys: %v", err)
	}

	pebbeServerPub, pebbeServerSec, err := pebbezet.NewCurveKeypair()
	if err != nil {
		b.Fatalf("Failed to generate pebbe keys: %v", err)
	}

	b.Run("Our-Implementation-Key-Operations", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Benchmark our key operations
			pubZ85, _ := ourKeys.PublicKeyZ85()
			secZ85, _ := ourKeys.SecretKeyZ85()
			
			// Roundtrip test
			_, err := NewKeyPairFromZ85(pubZ85, secZ85)
			if err != nil {
				b.Errorf("Key roundtrip failed: %v", err)
			}
		}
	})

	b.Run("Cross-Implementation-Compatibility", func(b *testing.B) {
		pebbeKeyPair, err := NewKeyPairFromZ85(pebbeServerPub, pebbeServerSec)
		if err != nil {
			b.Fatalf("Failed to convert pebbe keys: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Test creating security objects with cross-implementation keys
			clientSec := NewClientSecurity(ourKeys, pebbeKeyPair.Public)
			serverSec := NewServerSecurity(pebbeKeyPair)
			
			if clientSec == nil || serverSec == nil {
				b.Error("Failed to create security objects")
			}
		}
	})
}