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

// isHex checks if a string contains only hexadecimal characters
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <server_public_key>", os.Args[0])
		log.Fatalf("Key can be in hex (64 chars) or Z85 (40 chars) format")
	}
	
	// Parse server public key from command line (auto-detect format)
	serverPubKeyStr := os.Args[1]
	var serverPubKey [32]byte
	var err error
	
	if len(serverPubKeyStr) == 64 && isHex(serverPubKeyStr) {
		// Hex format
		serverPubKeyBytes, err := hex.DecodeString(serverPubKeyStr)
		if err != nil {
			log.Fatalf("Invalid server public key hex: %v", err)
		}
		if len(serverPubKeyBytes) != 32 {
			log.Fatalf("Server public key must be 32 bytes, got %d", len(serverPubKeyBytes))
		}
		copy(serverPubKey[:], serverPubKeyBytes)
		log.Printf("Using hex-encoded server public key")
	} else if len(serverPubKeyStr) == 40 {
		// Z85 format
		err := curve.ValidateZ85Key(serverPubKeyStr)
		if err != nil {
			log.Fatalf("Invalid server public key Z85: %v", err)
		}
		
		// Create temporary keypair to decode
		tempKp, err := curve.NewKeyPairFromZ85(serverPubKeyStr, serverPubKeyStr)
		if err != nil {
			log.Fatalf("Failed to decode Z85 server public key: %v", err)
		}
		serverPubKey = tempKp.Public
		log.Printf("Using Z85-encoded server public key")
	} else {
		log.Fatalf("Invalid server public key format. Expected 64-char hex or 40-char Z85, got %d chars", len(serverPubKeyStr))
	}
	
	// Generate client key pair
	clientKeys, err := curve.GenerateKeyPair()
	if err != nil {
		log.Fatalf("Failed to generate client keys: %v", err)
	}
	
	// Display client keys in both formats
	clientPubZ85, _ := clientKeys.PublicKeyZ85()
	log.Printf("Client public key (hex): %x", clientKeys.Public)
	log.Printf("Client public key (Z85): %s", clientPubZ85)
	
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