// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"net"
	"testing"
	"time"
)

func TestBeaconCreation(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	port := uint16(5555)
	interval := 1000 * time.Millisecond
	
	beacon, err := NewBeacon(uuid, port, interval)
	if err != nil {
		t.Fatalf("Failed to create beacon: %v", err)
	}
	
	if beacon.uuid != uuid {
		t.Errorf("Expected UUID %v, got %v", uuid, beacon.uuid)
	}
	if beacon.port != port {
		t.Errorf("Expected port %d, got %d", port, beacon.port)
	}
	if beacon.interval != interval {
		t.Errorf("Expected interval %v, got %v", interval, beacon.interval)
	}
}

func TestBeaconStartStop(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	port := uint16(0) // Use port 0 to avoid conflicts
	interval := 100 * time.Millisecond
	
	beacon, err := NewBeacon(uuid, port, interval)
	if err != nil {
		t.Fatalf("Failed to create beacon: %v", err)
	}
	
	// Test start
	err = beacon.Start()
	if err != nil {
		t.Fatalf("Failed to start beacon: %v", err)
	}
	
	if !beacon.running {
		t.Error("Expected beacon to be running")
	}
	
	// Wait a short time to let it run
	time.Sleep(200 * time.Millisecond)
	
	// Test stop
	beacon.Stop()
	
	if beacon.running {
		t.Error("Expected beacon to be stopped")
	}
}

func TestBeaconSetPort(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	port := uint16(0)
	interval := 100 * time.Millisecond
	
	beacon, err := NewBeacon(uuid, port, interval)
	if err != nil {
		t.Fatalf("Failed to create beacon: %v", err)
	}
	
	err = beacon.Start()
	if err != nil {
		t.Fatalf("Failed to start beacon: %v", err)
	}
	defer beacon.Stop()
	
	// Test setting port
	newPort := uint16(6666)
	err = beacon.SetPort(newPort)
	if err != nil {
		t.Fatalf("Failed to set port: %v", err)
	}
	
	if beacon.port != newPort {
		t.Errorf("Expected port %d, got %d", newPort, beacon.port)
	}
}

func TestBeaconDiscovery(t *testing.T) {
	// Create two beacons
	uuid1 := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	uuid2 := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	interval := 100 * time.Millisecond
	
	beacon1, err := NewBeacon(uuid1, 0, interval) // Silent mode
	if err != nil {
		t.Fatalf("Failed to create beacon1: %v", err)
	}
	
	beacon2, err := NewBeacon(uuid2, 5556, interval)
	if err != nil {
		t.Fatalf("Failed to create beacon2: %v", err)
	}
	
	// Start both beacons
	err = beacon1.Start()
	if err != nil {
		t.Fatalf("Failed to start beacon1: %v", err)
	}
	defer beacon1.Stop()
	
	err = beacon2.Start()
	if err != nil {
		t.Fatalf("Failed to start beacon2: %v", err)
	}
	defer beacon2.Stop()
	
	// Get discovery channel from beacon1
	discoveries := beacon1.Discoveries()
	
	// Wait for discovery
	timeout := time.After(2 * time.Second)
	var discovery *Discovery
	
	select {
	case discovery = <-discoveries:
		// Got discovery
	case <-timeout:
		t.Fatal("Timeout waiting for discovery")
	}
	
	// Verify discovery
	if discovery.UUID != uuid2 {
		t.Errorf("Expected UUID %v, got %v", uuid2, discovery.UUID)
	}
	if discovery.Port != 5556 {
		t.Errorf("Expected port 5556, got %d", discovery.Port)
	}
}

func TestBeaconParseMessage(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	beacon, err := NewBeacon(uuid, 0, time.Second)
	if err != nil {
		t.Fatalf("Failed to create beacon: %v", err)
	}
	
	// Test valid beacon message
	validBeacon := make([]byte, BeaconSize)
	copy(validBeacon[0:3], BeaconPrefix)
	validBeacon[3] = BeaconVersion
	copy(validBeacon[4:20], uuid[:])
	validBeacon[20] = 0x15 // Port 5555 in big endian
	validBeacon[21] = 0xB3
	
	sourceIP := net.ParseIP("192.168.1.100")
	discovery := beacon.parseBeacon(validBeacon, sourceIP)
	
	if discovery == nil {
		t.Fatal("Expected valid discovery, got nil")
	}
	if discovery.UUID != uuid {
		t.Errorf("Expected UUID %v, got %v", uuid, discovery.UUID)
	}
	if discovery.Port != 5555 {
		t.Errorf("Expected port 5555, got %d", discovery.Port)
	}
	if !discovery.Addr.Equal(sourceIP) {
		t.Errorf("Expected IP %v, got %v", sourceIP, discovery.Addr)
	}
	
	// Test invalid beacon (wrong prefix)
	invalidBeacon := make([]byte, BeaconSize)
	copy(invalidBeacon[0:3], "ABC")
	invalidBeacon[3] = BeaconVersion
	
	discovery = beacon.parseBeacon(invalidBeacon, sourceIP)
	if discovery != nil {
		t.Error("Expected nil discovery for invalid beacon")
	}
	
	// Test beacon from self (should be ignored)
	selfBeacon := make([]byte, BeaconSize)
	copy(selfBeacon[0:3], BeaconPrefix)
	selfBeacon[3] = BeaconVersion
	copy(selfBeacon[4:20], beacon.uuid[:]) // Same UUID as beacon
	
	discovery = beacon.parseBeacon(selfBeacon, sourceIP)
	if discovery != nil {
		t.Error("Expected nil discovery for self beacon")
	}
	
	// Test beacon with wrong size
	shortBeacon := make([]byte, 10)
	discovery = beacon.parseBeacon(shortBeacon, sourceIP)
	if discovery != nil {
		t.Error("Expected nil discovery for short beacon")
	}
}

func TestBeaconSilentMode(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	beacon, err := NewBeacon(uuid, 0, 50*time.Millisecond) // Silent mode (port 0)
	if err != nil {
		t.Fatalf("Failed to create beacon: %v", err)
	}
	
	err = beacon.Start()
	if err != nil {
		t.Fatalf("Failed to start beacon: %v", err)
	}
	defer beacon.Stop()
	
	// In silent mode, beacon should not send broadcasts
	// This is difficult to test directly, but we can verify the port is 0
	if beacon.port != 0 {
		t.Errorf("Expected port 0 for silent mode, got %d", beacon.port)
	}
	
	// Test switching to active mode
	err = beacon.SetPort(5557)
	if err != nil {
		t.Fatalf("Failed to set active port: %v", err)
	}
	
	if beacon.port != 5557 {
		t.Errorf("Expected port 5557, got %d", beacon.port)
	}
	
	// Test switching back to silent mode
	err = beacon.SetPort(0)
	if err != nil {
		t.Fatalf("Failed to set silent mode: %v", err)
	}
	
	if beacon.port != 0 {
		t.Errorf("Expected port 0 for silent mode, got %d", beacon.port)
	}
}

func BenchmarkBeaconParsing(b *testing.B) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	beacon, err := NewBeacon([16]byte{}, 0, time.Second)
	if err != nil {
		b.Fatalf("Failed to create beacon: %v", err)
	}
	
	// Create valid beacon message
	validBeacon := make([]byte, BeaconSize)
	copy(validBeacon[0:3], BeaconPrefix)
	validBeacon[3] = BeaconVersion
	copy(validBeacon[4:20], uuid[:])
	validBeacon[20] = 0x15 // Port 5555
	validBeacon[21] = 0xB3
	
	sourceIP := net.ParseIP("192.168.1.100")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		discovery := beacon.parseBeacon(validBeacon, sourceIP)
		if discovery == nil {
			b.Fatal("Expected valid discovery")
		}
	}
}