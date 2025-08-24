// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"testing"
	"time"
)

func TestEventCreation(t *testing.T) {
	// Test ENTER event
	headers := map[string]string{"key": "value"}
	enterEvent := NewEnterEvent("uuid1", "peer1", "tcp://192.168.1.1:5555", headers)
	
	if enterEvent.Type != EventTypeEnter {
		t.Errorf("Expected type %s, got %s", EventTypeEnter, enterEvent.Type)
	}
	if enterEvent.PeerUUID != "uuid1" {
		t.Errorf("Expected UUID uuid1, got %s", enterEvent.PeerUUID)
	}
	if enterEvent.PeerName != "peer1" {
		t.Errorf("Expected name peer1, got %s", enterEvent.PeerName)
	}
	if enterEvent.PeerAddr != "tcp://192.168.1.1:5555" {
		t.Errorf("Expected addr tcp://192.168.1.1:5555, got %s", enterEvent.PeerAddr)
	}
	if enterEvent.Headers["key"] != "value" {
		t.Errorf("Expected header value, got %s", enterEvent.Headers["key"])
	}
	
	// Test EXIT event
	exitEvent := NewExitEvent("uuid1", "peer1", "tcp://192.168.1.1:5555")
	
	if exitEvent.Type != EventTypeExit {
		t.Errorf("Expected type %s, got %s", EventTypeExit, exitEvent.Type)
	}
	
	// Test JOIN event
	joinEvent := NewJoinEvent("uuid1", "peer1", "group1")
	
	if joinEvent.Type != EventTypeJoin {
		t.Errorf("Expected type %s, got %s", EventTypeJoin, joinEvent.Type)
	}
	if joinEvent.Group != "group1" {
		t.Errorf("Expected group group1, got %s", joinEvent.Group)
	}
	
	// Test LEAVE event
	leaveEvent := NewLeaveEvent("uuid1", "peer1", "group1")
	
	if leaveEvent.Type != EventTypeLeave {
		t.Errorf("Expected type %s, got %s", EventTypeLeave, leaveEvent.Type)
	}
	
	// Test WHISPER event
	message := [][]byte{[]byte("frame1"), []byte("frame2")}
	whisperEvent := NewWhisperEvent("uuid1", "peer1", message)
	
	if whisperEvent.Type != EventTypeWhisper {
		t.Errorf("Expected type %s, got %s", EventTypeWhisper, whisperEvent.Type)
	}
	if len(whisperEvent.Message) != 2 {
		t.Errorf("Expected 2 message frames, got %d", len(whisperEvent.Message))
	}
	
	// Test SHOUT event
	shoutEvent := NewShoutEvent("uuid1", "peer1", "group1", message)
	
	if shoutEvent.Type != EventTypeShout {
		t.Errorf("Expected type %s, got %s", EventTypeShout, shoutEvent.Type)
	}
	if shoutEvent.Group != "group1" {
		t.Errorf("Expected group group1, got %s", shoutEvent.Group)
	}
}

func TestEventChannel(t *testing.T) {
	ec := NewEventChannel(10)
	
	// Start event channel
	ec.Start()
	defer ec.Close()
	
	// Subscribe to events
	listener1 := ec.Subscribe(5)
	listener2 := ec.Subscribe(5)
	
	// Publish an event
	event := NewEnterEvent("uuid1", "peer1", "tcp://192.168.1.1:5555", nil)
	ec.Publish(event)
	
	// Check both listeners receive the event
	timeout := time.After(1 * time.Second)
	
	select {
	case receivedEvent := <-listener1:
		if receivedEvent.Type != EventTypeEnter {
			t.Errorf("Expected ENTER event, got %s", receivedEvent.Type)
		}
	case <-timeout:
		t.Error("Timeout waiting for event on listener1")
	}
	
	select {
	case receivedEvent := <-listener2:
		if receivedEvent.Type != EventTypeEnter {
			t.Errorf("Expected ENTER event, got %s", receivedEvent.Type)
		}
	case <-timeout:
		t.Error("Timeout waiting for event on listener2")
	}
}

func TestEventChannelMultipleEvents(t *testing.T) {
	ec := NewEventChannel(50)
	ec.Start()
	defer ec.Close()
	
	listener := ec.Subscribe(50)
	
	// Publish multiple events
	events := []*Event{
		NewEnterEvent("uuid1", "peer1", "addr1", nil),
		NewJoinEvent("uuid1", "peer1", "group1"),
		NewShoutEvent("uuid1", "peer1", "group1", [][]byte{[]byte("hello")}),
		NewLeaveEvent("uuid1", "peer1", "group1"),
		NewExitEvent("uuid1", "peer1", "addr1"),
	}
	
	for _, event := range events {
		ec.Publish(event)
	}
	
	// Receive all events
	receivedEvents := make([]*Event, 0, len(events))
	timeout := time.After(2 * time.Second)
	
	for i := 0; i < len(events); i++ {
		select {
		case event := <-listener:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			t.Fatalf("Timeout waiting for event %d", i)
		}
	}
	
	// Verify event order and types
	expectedTypes := []string{EventTypeEnter, EventTypeJoin, EventTypeShout, EventTypeLeave, EventTypeExit}
	for i, event := range receivedEvents {
		if event.Type != expectedTypes[i] {
			t.Errorf("Expected event %d type %s, got %s", i, expectedTypes[i], event.Type)
		}
	}
}

func TestEventChannelClose(t *testing.T) {
	ec := NewEventChannel(10)
	ec.Start()
	
	listener := ec.Subscribe(5)
	
	// Publish an event
	event := NewEnterEvent("uuid1", "peer1", "addr1", nil)
	ec.Publish(event)
	
	// Close the event channel
	ec.Close()
	ec.Wait()
	
	// Verify listener channel is closed
	timeout := time.After(1 * time.Second)
	receivedCount := 0
	
	for {
		select {
		case event, ok := <-listener:
			if !ok {
				// Channel closed, this is expected
				return
			}
			receivedCount++
			if event.Type != EventTypeEnter {
				t.Errorf("Expected ENTER event, got %s", event.Type)
			}
		case <-timeout:
			if receivedCount == 0 {
				t.Error("Should have received at least one event before close")
			}
			return
		}
	}
}

func TestEventChannelBufferOverflow(t *testing.T) {
	// Create event channel with small buffer
	ec := NewEventChannel(2)
	ec.Start()
	defer ec.Close()
	
	// Create listener with small buffer
	listener := ec.Subscribe(1)
	
	// Publish more events than buffers can handle
	for i := 0; i < 10; i++ {
		event := NewEnterEvent("uuid1", "peer1", "addr1", nil)
		ec.Publish(event)
	}
	
	// Should still receive some events (exact number depends on timing)
	timeout := time.After(100 * time.Millisecond)
	receivedCount := 0
	
	for {
		select {
		case <-listener:
			receivedCount++
		case <-timeout:
			// We should have received at least one event
			if receivedCount == 0 {
				t.Error("Should have received at least one event")
			}
			return
		}
	}
}

func TestEventTimestamp(t *testing.T) {
	before := time.Now()
	event := NewEnterEvent("uuid1", "peer1", "addr1", nil)
	after := time.Now()
	
	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Errorf("Event timestamp %v is not between %v and %v", 
			event.Timestamp, before, after)
	}
}

func BenchmarkEventCreation(b *testing.B) {
	headers := map[string]string{"key": "value"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewEnterEvent("uuid1", "peer1", "addr1", headers)
	}
}

func BenchmarkEventChannelPublish(b *testing.B) {
	ec := NewEventChannel(1000)
	ec.Start()
	defer ec.Close()
	
	event := NewEnterEvent("uuid1", "peer1", "addr1", nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ec.Publish(event)
	}
}

func BenchmarkEventChannelSubscribe(b *testing.B) {
	ec := NewEventChannel(1000)
	ec.Start()
	defer ec.Close()
	
	// Publish events in background
	go func() {
		for i := 0; i < b.N; i++ {
			event := NewEnterEvent("uuid1", "peer1", "addr1", nil)
			ec.Publish(event)
		}
	}()
	
	listener := ec.Subscribe(1000)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		<-listener
	}
}