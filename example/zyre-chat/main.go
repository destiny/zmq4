// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Zyre Chat Example - Demonstrates ZRE peer-to-peer chat functionality
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/destiny/zmq4/v25/zyre"
)

var (
	name     = flag.String("name", "", "Node name (default: auto-generated)")
	group    = flag.String("group", "default", "Chat group to join")
	port     = flag.Uint("port", 0, "Port to use (0 = auto)")
	verbose  = flag.Bool("verbose", false, "Verbose output")
	security = flag.String("security", "null", "Security mechanism: null, plain, curve")
	username = flag.String("username", "", "Username for PLAIN security")
	password = flag.String("password", "", "Password for PLAIN security")
)

func main() {
	flag.Parse()

	fmt.Println("=== Zyre Chat Example ===")
	fmt.Printf("Group: %s\n", *group)
	fmt.Printf("Security: %s\n", *security)
	
	// Create security configuration
	var secConfig *zyre.SecurityConfig
	switch *security {
	case "plain":
		if *username == "" || *password == "" {
			log.Fatal("PLAIN security requires -username and -password")
		}
		secConfig = &zyre.SecurityConfig{
			Type:     zyre.SecurityTypePlain,
			Username: *username,
			Password: *password,
		}
	case "curve":
		// Generate CURVE keys for this session
		publicKey, secretKey, err := generateCurveKeys()
		if err != nil {
			log.Fatalf("Failed to generate CURVE keys: %v", err)
		}
		fmt.Printf("Our public key: %s\n", publicKey)
		secConfig = &zyre.SecurityConfig{
			Type:      zyre.SecurityTypeCurve,
			PublicKey: publicKey,
			SecretKey: secretKey,
		}
	default:
		secConfig = &zyre.SecurityConfig{
			Type: zyre.SecurityTypeNull,
		}
	}

	// Create node configuration
	config := &zyre.NodeConfig{
		Name:     *name,
		Port:     uint16(*port),
		Interval: 1000 * time.Millisecond,
		Security: secConfig,
		Headers: map[string]string{
			"app":     "zyre-chat",
			"version": "1.0",
		},
	}

	// Create and start Zyre node
	node, err := zyre.NewNode(config)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}

	fmt.Printf("Starting node: %s (%s)\n", node.Name(), node.UUID())

	err = node.Start()
	if err != nil {
		log.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop()

	// Join the chat group
	err = node.Join(*group)
	if err != nil {
		log.Fatalf("Failed to join group %s: %v", *group, err)
	}

	fmt.Printf("Joined group: %s\n", *group)
	fmt.Println("Type messages to send, /help for commands, /quit to exit")

	// Start event handler
	events := node.Events()
	go handleEvents(events, *verbose)

	// Handle user input
	handleUserInput(node, *group)
}

func handleEvents(events <-chan *zyre.Event, verbose bool) {
	for event := range events {
		switch event.Type {
		case zyre.EventTypeEnter:
			fmt.Printf("\n[%s] %s (%s) joined the network\n", 
				event.Timestamp.Format("15:04:05"),
				event.PeerName, 
				event.PeerUUID[:8])
			if verbose && len(event.Headers) > 0 {
				fmt.Printf("  Headers: %v\n", event.Headers)
			}

		case zyre.EventTypeExit:
			fmt.Printf("\n[%s] %s (%s) left the network\n", 
				event.Timestamp.Format("15:04:05"),
				event.PeerName, 
				event.PeerUUID[:8])

		case zyre.EventTypeJoin:
			fmt.Printf("\n[%s] %s (%s) joined group '%s'\n", 
				event.Timestamp.Format("15:04:05"),
				event.PeerName, 
				event.PeerUUID[:8], 
				event.Group)

		case zyre.EventTypeLeave:
			fmt.Printf("\n[%s] %s (%s) left group '%s'\n", 
				event.Timestamp.Format("15:04:05"),
				event.PeerName, 
				event.PeerUUID[:8], 
				event.Group)

		case zyre.EventTypeShout:
			if len(event.Message) > 0 {
				message := string(event.Message[0])
				fmt.Printf("\n[%s] <%s@%s> %s\n", 
					event.Timestamp.Format("15:04:05"),
					event.PeerName, 
					event.Group, 
					message)
			}

		case zyre.EventTypeWhisper:
			if len(event.Message) > 0 {
				message := string(event.Message[0])
				fmt.Printf("\n[%s] <%s> (whisper) %s\n", 
					event.Timestamp.Format("15:04:05"),
					event.PeerName, 
					message)
			}

		default:
			if verbose {
				fmt.Printf("\n[%s] Unknown event: %s\n", 
					event.Timestamp.Format("15:04:05"), 
					event.Type)
			}
		}
		fmt.Print("> ")
	}
}

func handleUserInput(node *zyre.Node, group string) {
	scanner := bufio.NewScanner(os.Stdin)
	
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		node.Stop()
		os.Exit(0)
	}()

	fmt.Print("> ")
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		
		if input == "" {
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(input, "/") {
			handleCommand(node, group, input)
		} else {
			// Send message to group
			message := [][]byte{[]byte(input)}
			err := node.Shout(group, message)
			if err != nil {
				fmt.Printf("Failed to send message: %v\n", err)
			}
		}
		
		fmt.Print("> ")
	}
}

func handleCommand(node *zyre.Node, currentGroup, input string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}

	command := parts[0]
	
	switch command {
	case "/help":
		printHelp()
		
	case "/quit", "/exit":
		fmt.Println("Goodbye!")
		node.Stop()
		os.Exit(0)
		
	case "/peers":
		peers := node.Peers()
		fmt.Printf("Known peers (%d):\n", len(peers))
		for uuid, peer := range peers {
			status := "disconnected"
			if peer.Connected {
				status = "connected"
			}
			fmt.Printf("  %s: %s (%s) - %s\n", 
				uuid[:8], peer.Name, peer.Endpoint, status)
		}
		
	case "/groups":
		// This would require additional API methods to list groups
		fmt.Println("Groups feature not implemented yet")
		
	case "/join":
		if len(parts) < 2 {
			fmt.Println("Usage: /join <group_name>")
			return
		}
		groupName := parts[1]
		err := node.Join(groupName)
		if err != nil {
			fmt.Printf("Failed to join group %s: %v\n", groupName, err)
		} else {
			fmt.Printf("Joined group: %s\n", groupName)
		}
		
	case "/leave":
		if len(parts) < 2 {
			fmt.Println("Usage: /leave <group_name>")
			return
		}
		groupName := parts[1]
		err := node.Leave(groupName)
		if err != nil {
			fmt.Printf("Failed to leave group %s: %v\n", groupName, err)
		} else {
			fmt.Printf("Left group: %s\n", groupName)
		}
		
	case "/whisper":
		if len(parts) < 3 {
			fmt.Println("Usage: /whisper <peer_uuid> <message>")
			return
		}
		peerUUID := parts[1]
		message := strings.Join(parts[2:], " ")
		
		err := node.Whisper(peerUUID, [][]byte{[]byte(message)})
		if err != nil {
			fmt.Printf("Failed to whisper to %s: %v\n", peerUUID, err)
		} else {
			fmt.Printf("Whispered to %s: %s\n", peerUUID[:8], message)
		}
		
	case "/name":
		if len(parts) < 2 {
			fmt.Printf("Current name: %s\n", node.Name())
			return
		}
		newName := strings.Join(parts[1:], " ")
		node.SetName(newName)
		fmt.Printf("Name changed to: %s\n", newName)
		
	case "/uuid":
		fmt.Printf("Our UUID: %s\n", node.UUID())
		
	case "/security":
		secMgr := node.Security()
		config := secMgr.GetConfig()
		fmt.Printf("Security type: %d\n", config.Type)
		if config.PublicKey != "" {
			fmt.Printf("Public key: %s\n", config.PublicKey)
		}
		
	default:
		fmt.Printf("Unknown command: %s (type /help for help)\n", command)
	}
}

func printHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  /help          - Show this help")
	fmt.Println("  /quit          - Exit the chat")
	fmt.Println("  /peers         - List known peers")
	fmt.Println("  /groups        - List groups")
	fmt.Println("  /join <group>  - Join a group")
	fmt.Println("  /leave <group> - Leave a group")
	fmt.Println("  /whisper <uuid> <msg> - Send private message")
	fmt.Println("  /name [name]   - Show/set node name")
	fmt.Println("  /uuid          - Show our UUID")
	fmt.Println("  /security      - Show security info")
	fmt.Println()
	fmt.Println("To send a message to the current group, just type it and press Enter.")
}

// generateCurveKeys generates a CURVE keypair for this example
func generateCurveKeys() (string, string, error) {
	// This is a placeholder - in a real implementation, you would use
	// the security manager to generate keys
	return "placeholder-public-key", "placeholder-secret-key", nil
}