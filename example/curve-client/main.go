// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example CURVE client demonstrating secure ZeroMQ communication
package main

import (
	"context"
	"encoding/hex"
	"log"
	"os"

	"github.com/destiny/zmq4"
	"github.com/destiny/zmq4/security/curve"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <server_public_key_hex>", os.Args[0])
	}
	
	// Parse server public key from command line
	serverPubKeyHex := os.Args[1]
	serverPubKeyBytes, err := hex.DecodeString(serverPubKeyHex)
	if err != nil {
		log.Fatalf("Invalid server public key hex: %v", err)
	}
	
	if len(serverPubKeyBytes) != 32 {
		log.Fatalf("Server public key must be 32 bytes, got %d", len(serverPubKeyBytes))
	}
	
	var serverPubKey [32]byte
	copy(serverPubKey[:], serverPubKeyBytes)
	
	// Generate client key pair
	clientKeys, err := curve.GenerateKeyPair()
	if err != nil {
		log.Fatalf("Failed to generate client keys: %v", err)
	}
	
	log.Printf("Client public key: %x", clientKeys.Public)
	
	// Create CURVE security for client
	security := curve.NewClientSecurity(clientKeys, serverPubKey)
	
	// Create context and socket
	ctx := context.Background()
	
	socket := zmq4.NewReq(ctx, zmq4.WithSecurity(security))
	defer socket.Close()
	
	// Connect to server
	err = socket.Dial("tcp://127.0.0.1:5555")
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	
	log.Printf("Connected to CURVE server")
	
	// Send request
	request := zmq4.NewMsgString("Hello from CURVE client!")
	err = socket.Send(request)
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}
	
	// Receive reply
	reply, err := socket.Recv()
	if err != nil {
		log.Fatalf("Failed to receive reply: %v", err)
	}
	
	log.Printf("Reply: %s", reply.Frames[0])
}