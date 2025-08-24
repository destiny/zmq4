// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Group represents a ZRE group with channel-based coordination
type Group struct {
	name        string            // Group name
	members     map[string]*Peer  // Group members (UUID -> Peer)
	
	// Channel-based communication
	commands    chan groupCmd     // Group control commands
	messages    chan *Message     // Messages for this group
	
	// State management
	ctx         context.Context   // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup    // Wait group for goroutines
	mutex       sync.RWMutex      // Protects group state
}

// groupCmd represents internal group commands
type groupCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan error  // Reply channel
}

// GroupManager manages multiple groups with channel coordination
type GroupManager struct {
	groups      map[string]*Group  // Map of group name to group
	commands    chan managerCmd    // Manager control commands
	events      *EventChannel      // Event channel for notifications
	
	// State management
	ctx         context.Context    // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup     // Wait group for goroutines
	mutex       sync.RWMutex       // Protects manager state
}

// GroupMembership represents group membership information
type GroupMembership struct {
	Group   string            // Group name
	Members map[string]*Peer  // Members in the group
}

// NewGroup creates a new group
func NewGroup(name string) *Group {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Group{
		name:     name,
		members:  make(map[string]*Peer),
		commands: make(chan groupCmd, 50),
		messages: make(chan *Message, 1000),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins group operation
func (g *Group) Start() {
	g.wg.Add(1)
	go g.commandLoop()
	
	g.wg.Add(1)
	go g.messageLoop()
}

// Stop stops the group
func (g *Group) Stop() {
	g.cancel()
	g.wg.Wait()
	
	close(g.commands)
	close(g.messages)
}

// Name returns the group name
func (g *Group) Name() string {
	return g.name
}

// Members returns a copy of current members
func (g *Group) Members() map[string]*Peer {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	
	members := make(map[string]*Peer)
	for uuid, peer := range g.members {
		members[uuid] = peer
	}
	return members
}

// MemberCount returns the number of members in the group
func (g *Group) MemberCount() int {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	return len(g.members)
}

// HasMember checks if a peer is a member of this group
func (g *Group) HasMember(peerUUID string) bool {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	_, exists := g.members[peerUUID]
	return exists
}

// AddMember adds a peer to the group
func (g *Group) AddMember(peer *Peer) error {
	reply := make(chan error, 1)
	cmd := groupCmd{
		action: "add_member",
		data:   peer,
		reply:  reply,
	}
	
	select {
	case g.commands <- cmd:
		return <-reply
	case <-g.ctx.Done():
		return g.ctx.Err()
	}
}

// RemoveMember removes a peer from the group
func (g *Group) RemoveMember(peerUUID string) error {
	reply := make(chan error, 1)
	cmd := groupCmd{
		action: "remove_member",
		data:   peerUUID,
		reply:  reply,
	}
	
	select {
	case g.commands <- cmd:
		return <-reply
	case <-g.ctx.Done():
		return g.ctx.Err()
	}
}

// SendMessage sends a message to all group members
func (g *Group) SendMessage(msg *Message) error {
	select {
	case g.messages <- msg:
		return nil
	case <-g.ctx.Done():
		return g.ctx.Err()
	default:
		return fmt.Errorf("group message queue full")
	}
}

// commandLoop processes group commands
func (g *Group) commandLoop() {
	defer g.wg.Done()
	
	for {
		select {
		case cmd := <-g.commands:
			g.handleCommand(cmd)
		case <-g.ctx.Done():
			return
		}
	}
}

// messageLoop processes messages for this group
func (g *Group) messageLoop() {
	defer g.wg.Done()
	
	for {
		select {
		case msg := <-g.messages:
			g.broadcastMessage(msg)
		case <-g.ctx.Done():
			return
		}
	}
}

// handleCommand processes a group command
func (g *Group) handleCommand(cmd groupCmd) {
	switch cmd.action {
	case "add_member":
		peer := cmd.data.(*Peer)
		g.mutex.Lock()
		peerUUID := fmt.Sprintf("%x", peer.UUID)
		g.members[peerUUID] = peer
		g.mutex.Unlock()
		
		// Update peer's group membership
		peer.JoinGroup(g.name)
		cmd.reply <- nil
		
	case "remove_member":
		peerUUID := cmd.data.(string)
		g.mutex.Lock()
		if peer, exists := g.members[peerUUID]; exists {
			delete(g.members, peerUUID)
			peer.LeaveGroup(g.name)
		}
		g.mutex.Unlock()
		cmd.reply <- nil
	}
}

// broadcastMessage sends a message to all group members
func (g *Group) broadcastMessage(msg *Message) {
	g.mutex.RLock()
	members := make([]*Peer, 0, len(g.members))
	for _, peer := range g.members {
		members = append(members, peer)
	}
	g.mutex.RUnlock()
	
	// Send message to all members
	for _, peer := range members {
		select {
		case <-g.ctx.Done():
			return
		default:
			peer.Send(msg) // Non-blocking send
		}
	}
}

// NewGroupManager creates a new group manager
func NewGroupManager(events *EventChannel) *GroupManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &GroupManager{
		groups:   make(map[string]*Group),
		commands: make(chan managerCmd, 100),
		events:   events,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins group manager operation
func (gm *GroupManager) Start() {
	gm.wg.Add(1)
	go gm.commandLoop()
}

// Stop stops the group manager
func (gm *GroupManager) Stop() {
	gm.cancel()
	gm.wg.Wait()
	
	// Stop all groups
	gm.mutex.Lock()
	defer gm.mutex.Unlock()
	
	for _, group := range gm.groups {
		group.Stop()
	}
	
	close(gm.commands)
}

// GetGroup returns a group by name, creating it if it doesn't exist
func (gm *GroupManager) GetGroup(name string) *Group {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "get_group",
		data:   name,
		reply:  reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		return result.(*Group)
	case <-gm.ctx.Done():
		return nil
	}
}

// CreateGroup creates a new group
func (gm *GroupManager) CreateGroup(name string) *Group {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "create_group",
		data:   name,
		reply:  reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		return result.(*Group)
	case <-gm.ctx.Done():
		return nil
	}
}

// DeleteGroup deletes a group
func (gm *GroupManager) DeleteGroup(name string) error {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "delete_group",
		data:   name,
		reply:  reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-gm.ctx.Done():
		return gm.ctx.Err()
	}
}

// ListGroups returns information about all groups
func (gm *GroupManager) ListGroups() map[string]*GroupMembership {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "list_groups",
		reply:  reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		return result.(map[string]*GroupMembership)
	case <-gm.ctx.Done():
		return nil
	}
}

// JoinGroup adds a peer to a group
func (gm *GroupManager) JoinGroup(groupName string, peer *Peer) error {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "join_group",
		data: map[string]interface{}{
			"group": groupName,
			"peer":  peer,
		},
		reply: reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-gm.ctx.Done():
		return gm.ctx.Err()
	}
}

// LeaveGroup removes a peer from a group
func (gm *GroupManager) LeaveGroup(groupName, peerUUID string) error {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "leave_group",
		data: map[string]interface{}{
			"group": groupName,
			"peer":  peerUUID,
		},
		reply: reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-gm.ctx.Done():
		return gm.ctx.Err()
	}
}

// SendToGroup sends a message to all members of a group
func (gm *GroupManager) SendToGroup(groupName string, msg *Message) error {
	reply := make(chan interface{}, 1)
	cmd := managerCmd{
		action: "send_to_group",
		data: map[string]interface{}{
			"group":   groupName,
			"message": msg,
		},
		reply: reply,
	}
	
	select {
	case gm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-gm.ctx.Done():
		return gm.ctx.Err()
	}
}

// commandLoop processes group manager commands
func (gm *GroupManager) commandLoop() {
	defer gm.wg.Done()
	
	for {
		select {
		case cmd := <-gm.commands:
			gm.handleCommand(cmd)
		case <-gm.ctx.Done():
			return
		}
	}
}

// handleCommand processes a group manager command
func (gm *GroupManager) handleCommand(cmd managerCmd) {
	switch cmd.action {
	case "get_group":
		name := cmd.data.(string)
		gm.mutex.Lock()
		group, exists := gm.groups[name]
		if !exists {
			group = NewGroup(name)
			group.Start()
			gm.groups[name] = group
		}
		gm.mutex.Unlock()
		cmd.reply <- group
		
	case "create_group":
		name := cmd.data.(string)
		gm.mutex.Lock()
		if _, exists := gm.groups[name]; !exists {
			group := NewGroup(name)
			group.Start()
			gm.groups[name] = group
			cmd.reply <- group
		} else {
			cmd.reply <- gm.groups[name]
		}
		gm.mutex.Unlock()
		
	case "delete_group":
		name := cmd.data.(string)
		gm.mutex.Lock()
		if group, exists := gm.groups[name]; exists {
			group.Stop()
			delete(gm.groups, name)
			cmd.reply <- nil
		} else {
			cmd.reply <- fmt.Errorf("group not found: %s", name)
		}
		gm.mutex.Unlock()
		
	case "list_groups":
		gm.mutex.RLock()
		groups := make(map[string]*GroupMembership)
		for name, group := range gm.groups {
			groups[name] = &GroupMembership{
				Group:   name,
				Members: group.Members(),
			}
		}
		gm.mutex.RUnlock()
		cmd.reply <- groups
		
	case "join_group":
		data := cmd.data.(map[string]interface{})
		groupName := data["group"].(string)
		peer := data["peer"].(*Peer)
		
		gm.mutex.Lock()
		group, exists := gm.groups[groupName]
		if !exists {
			group = NewGroup(groupName)
			group.Start()
			gm.groups[groupName] = group
		}
		gm.mutex.Unlock()
		
		err := group.AddMember(peer)
		cmd.reply <- err
		
		// Publish JOIN event
		if err == nil {
			peerUUID := fmt.Sprintf("%x", peer.UUID)
			event := NewJoinEvent(peerUUID, peer.Name, groupName)
			gm.events.Publish(event)
		}
		
	case "leave_group":
		data := cmd.data.(map[string]interface{})
		groupName := data["group"].(string)
		peerUUID := data["peer"].(string)
		
		gm.mutex.RLock()
		group, exists := gm.groups[groupName]
		gm.mutex.RUnlock()
		
		if exists {
			err := group.RemoveMember(peerUUID)
			cmd.reply <- err
			
			// Publish LEAVE event
			if err == nil {
				event := NewLeaveEvent(peerUUID, "", groupName)
				gm.events.Publish(event)
			}
			
			// Remove empty groups
			if group.MemberCount() == 0 {
				gm.mutex.Lock()
				group.Stop()
				delete(gm.groups, groupName)
				gm.mutex.Unlock()
			}
		} else {
			cmd.reply <- fmt.Errorf("group not found: %s", groupName)
		}
		
	case "send_to_group":
		data := cmd.data.(map[string]interface{})
		groupName := data["group"].(string)
		message := data["message"].(*Message)
		
		gm.mutex.RLock()
		group, exists := gm.groups[groupName]
		gm.mutex.RUnlock()
		
		if exists {
			err := group.SendMessage(message)
			cmd.reply <- err
		} else {
			cmd.reply <- fmt.Errorf("group not found: %s", groupName)
		}
	}
}

// CleanupExpiredPeers removes expired peers from all groups
func (gm *GroupManager) CleanupExpiredPeers(expiredPeers map[string]*Peer) {
	gm.mutex.RLock()
	defer gm.mutex.RUnlock()
	
	for _, group := range gm.groups {
		for peerUUID := range expiredPeers {
			group.RemoveMember(peerUUID)
		}
	}
}