// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Zyre Discovery Example - Demonstrates ZRE peer discovery and monitoring
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/destiny/zmq4/v25/zyre"
)

var (
	name     = flag.String("name", "", "Node name (default: auto-generated)")
	port     = flag.Uint("port", 0, "Port to use (0 = auto)")
	interval = flag.Duration("interval", 1*time.Second, "Beacon interval")
	silent   = flag.Bool("silent", false, "Silent mode (no beacon broadcasting)")
	monitor  = flag.Bool("monitor", false, "Monitor mode (show detailed statistics)")
)

func main() {
	flag.Parse()

	fmt.Println("=== Zyre Discovery Example ===")
	
	// Create node configuration
	nodePort := uint16(*port)
	if *silent {
		nodePort = 0 // Silent mode
	}
	
	config := &zyre.NodeConfig{
		Name:     *name,
		Port:     nodePort,
		Interval: *interval,
		Headers: map[string]string{
			"app":      "zyre-discovery",
			"version":  "1.0",
			"purpose":  "discovery-demo",
		},
	}

	// Create and start Zyre node
	node, err := zyre.NewNode(config)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}

	if *silent {
		fmt.Printf("Starting in SILENT mode - listening only\n")
	} else {
		fmt.Printf("Starting node with beacon every %v\n", *interval)
	}
	
	fmt.Printf("Node: %s (%s)\n", node.Name(), node.UUID())

	err = node.Start()
	if err != nil {
		log.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start event monitoring
	events := node.Events()
	peerMap := make(map[string]*PeerInfo)
	
	if *monitor {
		go monitorStatistics(node, peerMap)
	}

	fmt.Println("Monitoring network for peer discoveries...")
	fmt.Println("Press Ctrl+C to exit")

	for {
		select {
		case event := <-events:
			handleDiscoveryEvent(event, peerMap)

		case <-sigChan:
			fmt.Println("\nShutting down...")
			printFinalSummary(peerMap)
			return
		}
	}
}

type PeerInfo struct {
	UUID      string
	Name      string
	Addr      string
	FirstSeen time.Time
	LastSeen  time.Time
	Headers   map[string]string
	Groups    []string
	Active    bool
}

func handleDiscoveryEvent(event *zyre.Event, peerMap map[string]*PeerInfo) {
	timestamp := event.Timestamp.Format("15:04:05.000")
	
	switch event.Type {
	case zyre.EventTypeEnter:
		peer := &PeerInfo{
			UUID:      event.PeerUUID,
			Name:      event.PeerName,
			Addr:      event.PeerAddr,
			FirstSeen: event.Timestamp,
			LastSeen:  event.Timestamp,
			Headers:   make(map[string]string),
			Groups:    make([]string, 0),
			Active:    true,
		}
		
		// Copy headers
		for k, v := range event.Headers {
			peer.Headers[k] = v
		}
		
		peerMap[event.PeerUUID] = peer
		
		fmt.Printf("[%s] ENTER: %s (%s) at %s\n", 
			timestamp, event.PeerName, event.PeerUUID[:8], event.PeerAddr)
		
		// Show interesting headers
		if app, ok := event.Headers["app"]; ok {
			fmt.Printf("         App: %s", app)
			if version, ok := event.Headers["version"]; ok {
				fmt.Printf(" v%s", version)
			}
			fmt.Println()
		}
		
		if purpose, ok := event.Headers["purpose"]; ok {
			fmt.Printf("         Purpose: %s\n", purpose)
		}

	case zyre.EventTypeExit:
		if peer, exists := peerMap[event.PeerUUID]; exists {
			peer.Active = false
			duration := event.Timestamp.Sub(peer.FirstSeen)
			fmt.Printf("[%s] EXIT:  %s (%s) after %v\n", 
				timestamp, event.PeerName, event.PeerUUID[:8], duration)
		} else {
			fmt.Printf("[%s] EXIT:  %s (%s) [unknown peer]\n", 
				timestamp, event.PeerName, event.PeerUUID[:8])
		}

	case zyre.EventTypeJoin:
		if peer, exists := peerMap[event.PeerUUID]; exists {
			peer.Groups = append(peer.Groups, event.Group)
			peer.LastSeen = event.Timestamp
		}
		fmt.Printf("[%s] JOIN:  %s (%s) joined '%s'\n", 
			timestamp, event.PeerName, event.PeerUUID[:8], event.Group)

	case zyre.EventTypeLeave:
		if peer, exists := peerMap[event.PeerUUID]; exists {
			// Remove group from list
			for i, group := range peer.Groups {
				if group == event.Group {
					peer.Groups = append(peer.Groups[:i], peer.Groups[i+1:]...)
					break
				}
			}
			peer.LastSeen = event.Timestamp
		}
		fmt.Printf("[%s] LEAVE: %s (%s) left '%s'\n", 
			timestamp, event.PeerName, event.PeerUUID[:8], event.Group)

	case zyre.EventTypeShout:
		if peer, exists := peerMap[event.PeerUUID]; exists {
			peer.LastSeen = event.Timestamp
		}
		if len(event.Message) > 0 {
			fmt.Printf("[%s] SHOUT: %s (%s) to '%s': %s\n", 
				timestamp, event.PeerName, event.PeerUUID[:8], 
				event.Group, string(event.Message[0]))
		}

	case zyre.EventTypeWhisper:
		if peer, exists := peerMap[event.PeerUUID]; exists {
			peer.LastSeen = event.Timestamp
		}
		if len(event.Message) > 0 {
			fmt.Printf("[%s] WHISPER: %s (%s): %s\n", 
				timestamp, event.PeerName, event.PeerUUID[:8], 
				string(event.Message[0]))
		}
	}
}

func monitorStatistics(node *zyre.Node, peerMap map[string]*PeerInfo) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fmt.Println("\n=== STATISTICS ===")
		
		activePeers := 0
		totalPeers := len(peerMap)
		groupCount := make(map[string]int)
		
		for _, peer := range peerMap {
			if peer.Active {
				activePeers++
				for _, group := range peer.Groups {
					groupCount[group]++
				}
			}
		}
		
		fmt.Printf("Peers: %d active, %d total\n", activePeers, totalPeers)
		
		if len(groupCount) > 0 {
			fmt.Printf("Groups:\n")
			for group, count := range groupCount {
				fmt.Printf("  %s: %d members\n", group, count)
			}
		}
		
		// Show our node info
		peers := node.Peers()
		connectedPeers := 0
		for _, peer := range peers {
			if peer.Connected {
				connectedPeers++
			}
		}
		fmt.Printf("Our connections: %d/%d peers\n", connectedPeers, len(peers))
		
		fmt.Println("================\n")
	}
}

func printFinalSummary(peerMap map[string]*PeerInfo) {
	fmt.Println("\n=== FINAL SUMMARY ===")
	
	activePeers := 0
	totalPeers := len(peerMap)
	
	fmt.Printf("Total peers discovered: %d\n", totalPeers)
	
	if totalPeers > 0 {
		fmt.Println("\nPeer details:")
		for uuid, peer := range peerMap {
			status := "OFFLINE"
			if peer.Active {
				status = "ONLINE"
				activePeers++
			}
			
			duration := peer.LastSeen.Sub(peer.FirstSeen)
			fmt.Printf("  %s (%s): %s [%s]\n", 
				peer.Name, uuid[:8], status, peer.Addr)
			fmt.Printf("    Duration: %v\n", duration)
			
			if len(peer.Groups) > 0 {
				fmt.Printf("    Groups: %v\n", peer.Groups)
			}
			
			if len(peer.Headers) > 0 {
				fmt.Printf("    Headers: %v\n", peer.Headers)
			}
		}
	}
	
	fmt.Printf("\nFinal count: %d active, %d total\n", activePeers, totalPeers)
	fmt.Println("===================")
}