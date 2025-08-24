// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"bytes"
	"testing"
	"time"
)

func TestProtocolConstants(t *testing.T) {
	if ProtocolVersion != 2 {
		t.Errorf("Expected protocol version 2, got %d", ProtocolVersion)
	}
	
	if ProtocolSignature1 != 0xAA || ProtocolSignature2 != 0xA1 {
		t.Errorf("Expected protocol signature [0xAA, 0xA1], got [0x%02x, 0x%02x]", 
			ProtocolSignature1, ProtocolSignature2)
	}
	
	if BeaconPort != 5670 {
		t.Errorf("Expected beacon port 5670, got %d", BeaconPort)
	}
	
	if UUIDSize != 16 {
		t.Errorf("Expected UUID size 16, got %d", UUIDSize)
	}
}

func TestMessageMarshalUnmarshal(t *testing.T) {
	// Test basic message structure
	msg := &Message{
		Signature:  [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:    ProtocolVersion,
		Type:       MessageTypeHello,
		Sequence:   12345,
		SenderUUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Body:       []byte("test body"),
	}
	
	// Marshal message
	data, err := msg.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}
	
	// Unmarshal message
	var unmarshaled Message
	err = unmarshaled.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}
	
	// Verify fields
	if unmarshaled.Version != msg.Version {
		t.Errorf("Expected version %d, got %d", msg.Version, unmarshaled.Version)
	}
	if unmarshaled.Type != msg.Type {
		t.Errorf("Expected type %d, got %d", msg.Type, unmarshaled.Type)
	}
	if unmarshaled.Sequence != msg.Sequence {
		t.Errorf("Expected sequence %d, got %d", msg.Sequence, unmarshaled.Sequence)
	}
	if unmarshaled.SenderUUID != msg.SenderUUID {
		t.Errorf("Expected UUID %v, got %v", msg.SenderUUID, unmarshaled.SenderUUID)
	}
	if !bytes.Equal(unmarshaled.Body, msg.Body) {
		t.Errorf("Expected body %v, got %v", msg.Body, unmarshaled.Body)
	}
}

func TestHelloMessageMarshalUnmarshal(t *testing.T) {
	hello := &HelloMessage{
		Version:  2,
		Sequence: 1,
		Endpoint: "tcp://192.168.1.100:5555",
		Groups:   []string{"group1", "group2"},
		Status:   1,
		Name:     "test-node",
		Headers: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}
	
	// Marshal HELLO message
	data, err := hello.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal HELLO message: %v", err)
	}
	
	// Unmarshal HELLO message
	var unmarshaled HelloMessage
	err = unmarshaled.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal HELLO message: %v", err)
	}
	
	// Verify fields
	if unmarshaled.Version != hello.Version {
		t.Errorf("Expected version %d, got %d", hello.Version, unmarshaled.Version)
	}
	if unmarshaled.Sequence != hello.Sequence {
		t.Errorf("Expected sequence %d, got %d", hello.Sequence, unmarshaled.Sequence)
	}
	if unmarshaled.Endpoint != hello.Endpoint {
		t.Errorf("Expected endpoint %s, got %s", hello.Endpoint, unmarshaled.Endpoint)
	}
	if len(unmarshaled.Groups) != len(hello.Groups) {
		t.Errorf("Expected %d groups, got %d", len(hello.Groups), len(unmarshaled.Groups))
	}
	for i, group := range hello.Groups {
		if unmarshaled.Groups[i] != group {
			t.Errorf("Expected group %s, got %s", group, unmarshaled.Groups[i])
		}
	}
	if unmarshaled.Status != hello.Status {
		t.Errorf("Expected status %d, got %d", hello.Status, unmarshaled.Status)
	}
	if unmarshaled.Name != hello.Name {
		t.Errorf("Expected name %s, got %s", hello.Name, unmarshaled.Name)
	}
	if len(unmarshaled.Headers) != len(hello.Headers) {
		t.Errorf("Expected %d headers, got %d", len(hello.Headers), len(unmarshaled.Headers))
	}
	for key, value := range hello.Headers {
		if unmarshaled.Headers[key] != value {
			t.Errorf("Expected header %s=%s, got %s", key, value, unmarshaled.Headers[key])
		}
	}
}

func TestWhisperMessageMarshalUnmarshal(t *testing.T) {
	whisper := &WhisperMessage{
		Content: [][]byte{
			[]byte("frame1"),
			[]byte("frame2"),
			[]byte("frame3"),
		},
	}
	
	// Marshal WHISPER message
	data, err := whisper.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal WHISPER message: %v", err)
	}
	
	// Unmarshal WHISPER message
	var unmarshaled WhisperMessage
	err = unmarshaled.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal WHISPER message: %v", err)
	}
	
	// Verify content
	if len(unmarshaled.Content) != len(whisper.Content) {
		t.Errorf("Expected %d frames, got %d", len(whisper.Content), len(unmarshaled.Content))
	}
	for i, frame := range whisper.Content {
		if !bytes.Equal(unmarshaled.Content[i], frame) {
			t.Errorf("Expected frame %d: %v, got %v", i, frame, unmarshaled.Content[i])
		}
	}
}

func TestShoutMessageMarshalUnmarshal(t *testing.T) {
	shout := &ShoutMessage{
		Group: "test-group",
		Content: [][]byte{
			[]byte("message frame 1"),
			[]byte("message frame 2"),
		},
	}
	
	// Marshal SHOUT message
	data, err := shout.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal SHOUT message: %v", err)
	}
	
	// Unmarshal SHOUT message
	var unmarshaled ShoutMessage
	err = unmarshaled.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal SHOUT message: %v", err)
	}
	
	// Verify fields
	if unmarshaled.Group != shout.Group {
		t.Errorf("Expected group %s, got %s", shout.Group, unmarshaled.Group)
	}
	if len(unmarshaled.Content) != len(shout.Content) {
		t.Errorf("Expected %d frames, got %d", len(shout.Content), len(unmarshaled.Content))
	}
	for i, frame := range shout.Content {
		if !bytes.Equal(unmarshaled.Content[i], frame) {
			t.Errorf("Expected frame %d: %v, got %v", i, frame, unmarshaled.Content[i])
		}
	}
}

func TestJoinLeaveMessageMarshalUnmarshal(t *testing.T) {
	// Test JOIN message
	join := &JoinMessage{
		Group:  "test-group",
		Status: 1,
	}
	
	data, err := join.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal JOIN message: %v", err)
	}
	
	var unmarshaledJoin JoinMessage
	err = unmarshaledJoin.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal JOIN message: %v", err)
	}
	
	if unmarshaledJoin.Group != join.Group {
		t.Errorf("Expected group %s, got %s", join.Group, unmarshaledJoin.Group)
	}
	if unmarshaledJoin.Status != join.Status {
		t.Errorf("Expected status %d, got %d", join.Status, unmarshaledJoin.Status)
	}
	
	// Test LEAVE message
	leave := &LeaveMessage{
		Group:  "test-group",
		Status: 0,
	}
	
	data, err = leave.Marshal()
	if err != nil {
		t.Fatalf("Failed to marshal LEAVE message: %v", err)
	}
	
	var unmarshaledLeave LeaveMessage
	err = unmarshaledLeave.Unmarshal(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal LEAVE message: %v", err)
	}
	
	if unmarshaledLeave.Group != leave.Group {
		t.Errorf("Expected group %s, got %s", leave.Group, unmarshaledLeave.Group)
	}
	if unmarshaledLeave.Status != leave.Status {
		t.Errorf("Expected status %d, got %d", leave.Status, unmarshaledLeave.Status)
	}
}

func TestInvalidMessageHandling(t *testing.T) {
	// Test message too short
	var msg Message
	err := msg.Unmarshal([]byte{0xAA, 0xA1}) // Too short
	if err == nil {
		t.Error("Expected error for message too short")
	}
	
	// Test invalid signature
	err = msg.Unmarshal([]byte{0xFF, 0xFF, 0x02, 0x01, 0x00, 0x01, 
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	if err == nil {
		t.Error("Expected error for invalid signature")
	}
}

func TestMessageLimits(t *testing.T) {
	// Test message with endpoint too long
	hello := &HelloMessage{
		Version:  2,
		Sequence: 1,
		Endpoint: string(make([]byte, 300)), // Too long
		Groups:   []string{},
		Status:   1,
		Name:     "test",
		Headers:  map[string]string{},
	}
	
	_, err := hello.Marshal()
	if err == nil {
		t.Error("Expected error for endpoint too long")
	}
	
	// Test message with too many groups
	hello.Endpoint = "tcp://test:5555"
	hello.Groups = make([]string, 300) // Too many groups
	for i := range hello.Groups {
		hello.Groups[i] = "group"
	}
	
	_, err = hello.Marshal()
	if err == nil {
		t.Error("Expected error for too many groups")
	}
}

func BenchmarkMessageMarshal(b *testing.B) {
	msg := &Message{
		Signature:  [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:    ProtocolVersion,
		Type:       MessageTypeHello,
		Sequence:   12345,
		SenderUUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Body:       []byte("test body"),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := msg.Marshal()
		if err != nil {
			b.Fatalf("Marshal failed: %v", err)
		}
	}
}

func BenchmarkMessageUnmarshal(b *testing.B) {
	msg := &Message{
		Signature:  [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:    ProtocolVersion,
		Type:       MessageTypeHello,
		Sequence:   12345,
		SenderUUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Body:       []byte("test body"),
	}
	
	data, err := msg.Marshal()
	if err != nil {
		b.Fatalf("Marshal failed: %v", err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var unmarshaled Message
		err := unmarshaled.Unmarshal(data)
		if err != nil {
			b.Fatalf("Unmarshal failed: %v", err)
		}
	}
}