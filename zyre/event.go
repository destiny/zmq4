// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"time"
)

// Event represents a ZRE network event that occurred
type Event struct {
	Type      string            // Event type (ENTER, EXIT, JOIN, LEAVE, WHISPER, SHOUT)
	PeerUUID  string            // UUID of the peer that generated this event
	PeerName  string            // Name of the peer
	PeerAddr  string            // Network address of the peer
	Headers   map[string]string // Peer headers (for ENTER events)
	Group     string            // Group name (for JOIN, LEAVE, SHOUT events)
	Message   [][]byte          // Message content (for WHISPER, SHOUT events)
	Timestamp time.Time         // When the event occurred
}

// EventChannel manages event distribution using Go channels
type EventChannel struct {
	events    chan *Event
	listeners []chan *Event
	closed    chan struct{}
	closing   bool
}

// NewEventChannel creates a new event channel system
func NewEventChannel(bufferSize int) *EventChannel {
	return &EventChannel{
		events:    make(chan *Event, bufferSize),
		listeners: make([]chan *Event, 0),
		closed:    make(chan struct{}),
		closing:   false,
	}
}

// Subscribe returns a channel that will receive copies of all events
func (ec *EventChannel) Subscribe(bufferSize int) <-chan *Event {
	listener := make(chan *Event, bufferSize)
	ec.listeners = append(ec.listeners, listener)
	return listener
}

// Publish sends an event to all subscribers
func (ec *EventChannel) Publish(event *Event) {
	if ec.closing {
		return
	}
	
	select {
	case ec.events <- event:
		// Event queued successfully
	default:
		// Channel full, drop event to prevent blocking
	}
}

// Start begins the event distribution loop
func (ec *EventChannel) Start() {
	go func() {
		defer func() {
			// Close all listener channels when shutting down
			for _, listener := range ec.listeners {
				close(listener)
			}
			close(ec.closed)
		}()
		
		for {
			select {
			case event := <-ec.events:
				// Distribute event to all listeners
				for i, listener := range ec.listeners {
					select {
					case listener <- event:
						// Event sent successfully
					default:
						// Listener channel full, remove it to prevent memory leaks
						ec.listeners = append(ec.listeners[:i], ec.listeners[i+1:]...)
					}
				}
			case <-ec.closed:
				return
			}
		}
	}()
}

// Close stops the event channel and closes all subscriptions
func (ec *EventChannel) Close() {
	if !ec.closing {
		ec.closing = true
		close(ec.events)
	}
}

// Wait blocks until the event channel is fully closed
func (ec *EventChannel) Wait() {
	<-ec.closed
}

// NewEnterEvent creates an ENTER event
func NewEnterEvent(peerUUID, peerName, peerAddr string, headers map[string]string) *Event {
	return &Event{
		Type:      EventTypeEnter,
		PeerUUID:  peerUUID,
		PeerName:  peerName,
		PeerAddr:  peerAddr,
		Headers:   headers,
		Timestamp: time.Now(),
	}
}

// NewExitEvent creates an EXIT event
func NewExitEvent(peerUUID, peerName, peerAddr string) *Event {
	return &Event{
		Type:      EventTypeExit,
		PeerUUID:  peerUUID,
		PeerName:  peerName,
		PeerAddr:  peerAddr,
		Timestamp: time.Now(),
	}
}

// NewJoinEvent creates a JOIN event
func NewJoinEvent(peerUUID, peerName, group string) *Event {
	return &Event{
		Type:      EventTypeJoin,
		PeerUUID:  peerUUID,
		PeerName:  peerName,
		Group:     group,
		Timestamp: time.Now(),
	}
}

// NewLeaveEvent creates a LEAVE event
func NewLeaveEvent(peerUUID, peerName, group string) *Event {
	return &Event{
		Type:      EventTypeLeave,
		PeerUUID:  peerUUID,
		PeerName:  peerName,
		Group:     group,
		Timestamp: time.Now(),
	}
}

// NewWhisperEvent creates a WHISPER event
func NewWhisperEvent(peerUUID, peerName string, message [][]byte) *Event {
	return &Event{
		Type:      EventTypeWhisper,
		PeerUUID:  peerUUID,
		PeerName:  peerName,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// NewShoutEvent creates a SHOUT event
func NewShoutEvent(peerUUID, peerName, group string, message [][]byte) *Event {
	return &Event{
		Type:      EventTypeShout,
		PeerUUID:  peerUUID,
		PeerName:  peerName,
		Group:     group,
		Message:   message,
		Timestamp: time.Now(),
	}
}