// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testutil provides testing utilities for zmq4 protocols.
package testutil

import (
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"
)

var portCounter int64 = 20000

// GetAvailablePort returns an available TCP port for testing
func GetAvailablePort() (int, error) {
	// Try to find an available port starting from a base range
	basePort := atomic.AddInt64(&portCounter, 1)
	
	for i := 0; i < 100; i++ {
		port := int(basePort) + i
		if port > 65535 {
			port = 20000 + (port % 45535)
		}
		
		if isPortAvailable(port) {
			return port, nil
		}
	}
	
	return 0, fmt.Errorf("no available ports found in range")
}

// isPortAvailable checks if a TCP port is available for binding
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// GetTestEndpoint returns a test endpoint with an available port
func GetTestEndpoint() (string, error) {
	port, err := GetAvailablePort()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tcp://127.0.0.1:%d", port), nil
}

// GetUDPBeaconPort returns an available UDP port for beacon testing
func GetUDPBeaconPort() (int, error) {
	basePort := atomic.AddInt64(&portCounter, 1)
	
	for i := 0; i < 100; i++ {
		port := int(basePort) + i
		if port > 65535 {
			port = 20000 + (port % 45535)
		}
		
		if isUDPPortAvailable(port) {
			return port, nil
		}
	}
	
	return 0, fmt.Errorf("no available UDP ports found")
}

// isUDPPortAvailable checks if a UDP port is available
func isUDPPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// GetLocalInterfaces returns available network interfaces for testing
func GetLocalInterfaces() ([]net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	
	var available []net.Interface
	for _, iface := range interfaces {
		// Skip loopback and down interfaces for beacon testing
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			available = append(available, iface)
		}
	}
	
	return available, nil
}

// GetLoopbackInterface returns the loopback interface for testing
func GetLoopbackInterface() (*net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 && iface.Flags&net.FlagUp != 0 {
			return &iface, nil
		}
	}
	
	return nil, fmt.Errorf("no loopback interface found")
}

// WaitForConnection waits for a connection to be established
func WaitForConnection(endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", parseAddress(endpoint))
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	return fmt.Errorf("connection timeout for endpoint %s", endpoint)
}

// parseAddress extracts the address from a ZMQ endpoint
func parseAddress(endpoint string) string {
	if len(endpoint) > 6 && endpoint[:6] == "tcp://" {
		return endpoint[6:]
	}
	return endpoint
}

// NetworkTestConfig holds network testing configuration
type NetworkTestConfig struct {
	Timeout        time.Duration
	RetryInterval  time.Duration
	MaxRetries     int
	BeaconInterval time.Duration
}

// DefaultNetworkTestConfig returns default network test configuration
func DefaultNetworkTestConfig() *NetworkTestConfig {
	return &NetworkTestConfig{
		Timeout:        30 * time.Second,
		RetryInterval:  100 * time.Millisecond,
		MaxRetries:     10,
		BeaconInterval: 1 * time.Second,
	}
}

// PortRange represents a range of ports for testing
type PortRange struct {
	Start int
	End   int
}

// GetPortsInRange returns available ports within a range
func GetPortsInRange(portRange PortRange, count int) ([]int, error) {
	var ports []int
	
	for port := portRange.Start; port <= portRange.End && len(ports) < count; port++ {
		if isPortAvailable(port) {
			ports = append(ports, port)
		}
	}
	
	if len(ports) < count {
		return nil, fmt.Errorf("only found %d available ports, needed %d", len(ports), count)
	}
	
	return ports, nil
}

// TestEndpoints holds multiple test endpoints
type TestEndpoints struct {
	TCP []string
	UDP []int
}

// NewTestEndpoints creates multiple test endpoints
func NewTestEndpoints(tcpCount, udpCount int) (*TestEndpoints, error) {
	endpoints := &TestEndpoints{}
	
	// Generate TCP endpoints
	for i := 0; i < tcpCount; i++ {
		endpoint, err := GetTestEndpoint()
		if err != nil {
			return nil, fmt.Errorf("failed to get TCP endpoint %d: %w", i, err)
		}
		endpoints.TCP = append(endpoints.TCP, endpoint)
	}
	
	// Generate UDP ports
	for i := 0; i < udpCount; i++ {
		port, err := GetUDPBeaconPort()
		if err != nil {
			return nil, fmt.Errorf("failed to get UDP port %d: %w", i, err)
		}
		endpoints.UDP = append(endpoints.UDP, port)
	}
	
	return endpoints, nil
}

// ParsePort extracts port number from endpoint
func ParsePort(endpoint string) (int, error) {
	_, portStr, err := net.SplitHostPort(parseAddress(endpoint))
	if err != nil {
		return 0, err
	}
	
	return strconv.Atoi(portStr)
}

// GetMulticastTestAddress returns a test multicast address
func GetMulticastTestAddress() string {
	return "224.0.0.251:5670" // Standard mDNS multicast for testing
}

// GetNetworkInterfaces returns available network interfaces for testing
func GetNetworkInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}