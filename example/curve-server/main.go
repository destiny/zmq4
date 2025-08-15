// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example CURVE server demonstrating secure ZeroMQ communication
package main

import (
	"context"
	"log"

	"github.com/destiny/zmq4/v25"
	"github.com/destiny/zmq4/v25/security/curve"
)

func main() {
	// Generate server key pair
	serverKeys, err := curve.GenerateKeyPair()
	if err != nil {
		log.Fatalf("Failed to generate server keys: %v", err)
	}
	
	// Display keys in both hex and Z85 formats
	pubZ85, _ := serverKeys.PublicKeyZ85()
	secZ85, _ := serverKeys.SecretKeyZ85()
	
	log.Printf("Server public key (hex): %x", serverKeys.Public)
	log.Printf("Server public key (Z85): %s", pubZ85)
	log.Printf("Server secret key (hex): %x", serverKeys.Secret)
	log.Printf("Server secret key (Z85): %s", secZ85)
	
	// Create CURVE security for server
	security := curve.NewServerSecurity(serverKeys)
	
	// Create context and socket
	ctx := context.Background()
	
	socket := zmq4.NewRep(ctx, zmq4.WithSecurity(security))
	defer socket.Close()
	
	// Bind to endpoint
	err = socket.Listen("tcp://*:5555")
	if err != nil {
		log.Fatalf("Failed to bind socket: %v", err)
	}
	
	log.Printf("CURVE server listening on tcp://*:5555")
	log.Printf("Clients need server public key (hex): %x", serverKeys.Public)
	log.Printf("Clients need server public key (Z85): %s", pubZ85)
	
	for {
		// Receive request
		msg, err := socket.Recv()
		if err != nil {
			log.Printf("Failed to receive message: %v", err)
			continue
		}
		
		log.Printf("Received: %s", msg.Frames[0])
		
		// Send reply
		reply := zmq4.NewMsgString("Hello from CURVE server!")
		err = socket.Send(reply)
		if err != nil {
			log.Printf("Failed to send reply: %v", err)
		}
	}
}