// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// ServiceManager manages service request/reply patterns
type ServiceManager struct {
	// Configuration
	maxServices    int           // Maximum number of services
	serviceTimeout time.Duration // Service timeout
	
	// Service storage
	services map[string]*Service // Active services
	
	// Channel-based communication
	commands chan serviceCmd     // Service control commands
	requests chan *ServiceReq    // Service requests
	replies  chan *ServiceReply  // Service replies
	
	// State management
	ctx         context.Context   // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup    // Wait group for goroutines
	mutex       sync.RWMutex      // Protects service state
	requestID   uint64            // Request ID counter
}

// ServiceReq represents a service request
type ServiceReq struct {
	Service    string            // Service name
	Method     string            // Service method
	Sender     string            // Request sender
	Tracker    string            // Request tracker
	Body       []byte            // Request body
	Attributes map[string]string // Request attributes
	Timeout    time.Duration     // Request timeout
	Timestamp  time.Time         // Request timestamp
}

// ServiceReply represents a service reply
type ServiceReply struct {
	Tracker    string            // Request tracker
	Status     int               // Reply status
	Body       []byte            // Reply body
	Attributes map[string]string // Reply attributes
	Timestamp  time.Time         // Reply timestamp
}

// serviceCmd represents internal service commands
type serviceCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// workerRequest represents a worker registration request
type workerRequest struct {
	Service    string            // Service name
	ClientID   string            // Worker client ID
	Pattern    string            // Request pattern
	Attributes map[string]string // Worker attributes
}

// NewServiceManager creates a new service manager
func NewServiceManager(maxServices int, serviceTimeout time.Duration) *ServiceManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ServiceManager{
		maxServices:    maxServices,
		serviceTimeout: serviceTimeout,
		services:       make(map[string]*Service),
		commands:       make(chan serviceCmd, 1000),
		requests:       make(chan *ServiceReq, 10000),
		replies:        make(chan *ServiceReply, 10000),
		ctx:            ctx,
		cancel:         cancel,
		requestID:      0,
	}
}

// Start begins service manager operation
func (sm *ServiceManager) Start() {
	sm.wg.Add(1)
	go sm.commandLoop()
	
	sm.wg.Add(1)
	go sm.requestLoop()
	
	sm.wg.Add(1)
	go sm.replyLoop()
	
	sm.wg.Add(1)
	go sm.cleanupLoop()
}

// Stop stops the service manager
func (sm *ServiceManager) Stop() {
	sm.cancel()
	sm.wg.Wait()
	
	close(sm.commands)
	close(sm.requests)
	close(sm.replies)
}

// RegisterWorker registers a worker for a service
func (sm *ServiceManager) RegisterWorker(serviceName, clientID, pattern string, attributes map[string]string) error {
	if len(serviceName) == 0 || len(serviceName) > MaxServiceName {
		return fmt.Errorf("invalid service name length: %d", len(serviceName))
	}
	
	request := &workerRequest{
		Service:    serviceName,
		ClientID:   clientID,
		Pattern:    pattern,
		Attributes: attributes,
	}
	
	reply := make(chan interface{}, 1)
	cmd := serviceCmd{
		action: "register_worker",
		data:   request,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	}
}

// UnregisterWorker unregisters a worker from a service
func (sm *ServiceManager) UnregisterWorker(serviceName, clientID string) error {
	request := struct {
		Service  string
		ClientID string
	}{
		Service:  serviceName,
		ClientID: clientID,
	}
	
	reply := make(chan interface{}, 1)
	cmd := serviceCmd{
		action: "unregister_worker",
		data:   request,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		<-reply
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	}
}

// ProcessRequest processes a service request
func (sm *ServiceManager) ProcessRequest(serviceName, method, sender, tracker string, body []byte, attributes map[string]string, timeout time.Duration) error {
	if len(body) > MaxMessageSize {
		return fmt.Errorf("request too large: %d bytes", len(body))
	}
	
	request := &ServiceReq{
		Service:    serviceName,
		Method:     method,
		Sender:     sender,
		Tracker:    tracker,
		Body:       body,
		Attributes: attributes,
		Timeout:    timeout,
		Timestamp:  time.Now(),
	}
	
	select {
	case sm.requests <- request:
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	default:
		return fmt.Errorf("request queue full")
	}
}

// ProcessReply processes a service reply
func (sm *ServiceManager) ProcessReply(tracker string, status int, body []byte, attributes map[string]string) error {
	reply := &ServiceReply{
		Tracker:    tracker,
		Status:     status,
		Body:       body,
		Attributes: attributes,
		Timestamp:  time.Now(),
	}
	
	select {
	case sm.replies <- reply:
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	default:
		return fmt.Errorf("reply queue full")
	}
}

// RemoveClient removes a client from all services
func (sm *ServiceManager) RemoveClient(clientID string) {
	reply := make(chan interface{}, 1)
	cmd := serviceCmd{
		action: "remove_client",
		data:   clientID,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		<-reply
	case <-sm.ctx.Done():
	}
}

// GetService returns information about a service
func (sm *ServiceManager) GetService(serviceName string) (*Service, error) {
	reply := make(chan interface{}, 1)
	cmd := serviceCmd{
		action: "get_service",
		data:   serviceName,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		result := <-reply
		if service, ok := result.(*Service); ok {
			return service, nil
		}
		return nil, fmt.Errorf("service not found: %s", serviceName)
	case <-sm.ctx.Done():
		return nil, sm.ctx.Err()
	}
}

// GetServices returns all active services
func (sm *ServiceManager) GetServices() map[string]*Service {
	reply := make(chan interface{}, 1)
	cmd := serviceCmd{
		action: "get_services",
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		result := <-reply
		return result.(map[string]*Service)
	case <-sm.ctx.Done():
		return make(map[string]*Service)
	}
}

// commandLoop processes service commands
func (sm *ServiceManager) commandLoop() {
	defer sm.wg.Done()
	
	for {
		select {
		case cmd := <-sm.commands:
			sm.handleCommand(cmd)
		case <-sm.ctx.Done():
			return
		}
	}
}

// requestLoop processes service requests
func (sm *ServiceManager) requestLoop() {
	defer sm.wg.Done()
	
	for {
		select {
		case request := <-sm.requests:
			sm.processRequest(request)
		case <-sm.ctx.Done():
			return
		}
	}
}

// replyLoop processes service replies
func (sm *ServiceManager) replyLoop() {
	defer sm.wg.Done()
	
	for {
		select {
		case reply := <-sm.replies:
			sm.processReply(reply)
		case <-sm.ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically cleans up expired requests
func (sm *ServiceManager) cleanupLoop() {
	defer sm.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			sm.cleanupExpiredRequests()
		case <-sm.ctx.Done():
			return
		}
	}
}

// handleCommand processes a service command
func (sm *ServiceManager) handleCommand(cmd serviceCmd) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	switch cmd.action {
	case "register_worker":
		request := cmd.data.(*workerRequest)
		err := sm.handleRegisterWorker(request)
		cmd.reply <- err
		
	case "unregister_worker":
		request := cmd.data.(struct {
			Service  string
			ClientID string
		})
		sm.handleUnregisterWorker(request.Service, request.ClientID)
		cmd.reply <- nil
		
	case "remove_client":
		clientID := cmd.data.(string)
		sm.handleRemoveClient(clientID)
		cmd.reply <- nil
		
	case "get_service":
		serviceName := cmd.data.(string)
		if service, exists := sm.services[serviceName]; exists {
			// Return a copy
			serviceCopy := *service
			cmd.reply <- &serviceCopy
		} else {
			cmd.reply <- nil
		}
		
	case "get_services":
		services := make(map[string]*Service)
		for name, service := range sm.services {
			serviceCopy := *service
			services[name] = &serviceCopy
		}
		cmd.reply <- services
	}
}

// processRequest processes a service request
func (sm *ServiceManager) processRequest(request *ServiceReq) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	// Get service
	service, exists := sm.services[request.Service]
	if !exists {
		// Service not available
		fmt.Printf("Service not available: %s\n", request.Service)
		return
	}
	
	// Find available worker
	worker := sm.findAvailableWorker(service, request.Method)
	if worker == nil {
		// No workers available, queue request
		sm.requestID++
		serviceRequest := &ServiceRequest{
			ID:         sm.requestID,
			Service:    request.Service,
			Method:     request.Method,
			Sender:     request.Sender,
			Tracker:    request.Tracker,
			Attributes: request.Attributes,
			Body:       request.Body,
			Timestamp:  request.Timestamp,
			Timeout:    request.Timestamp.Add(request.Timeout),
			Retries:    0,
		}
		
		service.Queue = append(service.Queue, serviceRequest)
		
		// Limit queue size
		if len(service.Queue) > MaxServiceBacklog {
			// Remove oldest request
			copy(service.Queue, service.Queue[1:])
			service.Queue = service.Queue[:len(service.Queue)-1]
		}
		
		fmt.Printf("Queued request %d for service '%s'\n", serviceRequest.ID, request.Service)
		return
	}
	
	// Deliver request to worker
	worker.Credit--
	worker.LastRequest = time.Now()
	worker.RequestCount++
	
	service.RequestCount++
	service.LastActivity = time.Now()
	
	// In a real implementation, this would send a ServiceDeliverMessage
	// to the broker's response channel for delivery to the worker
	fmt.Printf("Delivering request for service '%s' method '%s' to worker '%s'\n", 
		request.Service, request.Method, worker.ClientID)
}

// processReply processes a service reply
func (sm *ServiceManager) processReply(reply *ServiceReply) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	// In a real implementation, this would:
	// 1. Find the original request using the tracker
	// 2. Send the reply back to the original requester
	// 3. Update service statistics
	
	fmt.Printf("Processing reply for tracker '%s' with status %d\n", 
		reply.Tracker, reply.Status)
	
	// Update statistics for all services
	for _, service := range sm.services {
		service.ReplyCount++
	}
}

// handleRegisterWorker handles a worker registration
func (sm *ServiceManager) handleRegisterWorker(request *workerRequest) error {
	// Get or create service
	service, exists := sm.services[request.Service]
	if !exists {
		if len(sm.services) >= sm.maxServices {
			return fmt.Errorf("maximum services exceeded")
		}
		
		service = &Service{
			Name:         request.Service,
			Workers:      make(map[string]*ServiceWorker),
			Queue:        make([]*ServiceRequest, 0),
			Attributes:   make(map[string]string),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
		sm.services[request.Service] = service
	}
	
	// Create worker
	worker := &ServiceWorker{
		ClientID:     request.ClientID,
		Pattern:      request.Pattern,
		Credit:       DefaultCreditWindow, // Default credit
		Attributes:   request.Attributes,
		RegisteredAt: time.Now(),
		LastRequest:  time.Time{},
		RequestCount: 0,
	}
	
	service.Workers[request.ClientID] = worker
	service.LastActivity = time.Now()
	
	// Try to dispatch queued requests
	sm.dispatchQueuedRequests(service)
	
	return nil
}

// handleUnregisterWorker handles a worker unregistration
func (sm *ServiceManager) handleUnregisterWorker(serviceName, clientID string) {
	if service, exists := sm.services[serviceName]; exists {
		delete(service.Workers, clientID)
		
		// Remove service if no workers
		if len(service.Workers) == 0 && len(service.Queue) == 0 {
			delete(sm.services, serviceName)
		}
	}
}

// handleRemoveClient removes a client from all services
func (sm *ServiceManager) handleRemoveClient(clientID string) {
	for serviceName, service := range sm.services {
		delete(service.Workers, clientID)
		
		// Remove service if no workers
		if len(service.Workers) == 0 && len(service.Queue) == 0 {
			delete(sm.services, serviceName)
		}
	}
}

// findAvailableWorker finds an available worker for a method
func (sm *ServiceManager) findAvailableWorker(service *Service, method string) *ServiceWorker {
	// Find worker with credit that matches the method pattern
	for _, worker := range service.Workers {
		if worker.Credit > 0 && sm.matchesPattern(method, worker.Pattern) {
			return worker
		}
	}
	
	return nil
}

// matchesPattern checks if a method matches a worker pattern
func (sm *ServiceManager) matchesPattern(method, pattern string) bool {
	// Simple pattern matching - supports wildcards
	if pattern == "*" {
		return true
	}
	
	// Support glob-style patterns
	matched, err := filepath.Match(pattern, method)
	if err != nil {
		// Fallback to exact match
		return method == pattern
	}
	
	return matched
}

// dispatchQueuedRequests dispatches queued requests to available workers
func (sm *ServiceManager) dispatchQueuedRequests(service *Service) {
	now := time.Now()
	
	// Process queued requests
	validRequests := make([]*ServiceRequest, 0, len(service.Queue))
	
	for _, request := range service.Queue {
		if request.Timeout.After(now) {
			// Find available worker
			worker := sm.findAvailableWorker(service, request.Method)
			if worker != nil {
				// Deliver request to worker
				worker.Credit--
				worker.LastRequest = time.Now()
				worker.RequestCount++
				
				service.RequestCount++
				service.LastActivity = time.Now()
				
				fmt.Printf("Dispatching queued request %d for service '%s' to worker '%s'\n", 
					request.ID, service.Name, worker.ClientID)
			} else {
				// Keep in queue
				validRequests = append(validRequests, request)
			}
		}
		// Expired requests are dropped
	}
	
	service.Queue = validRequests
}

// cleanupExpiredRequests removes expired requests from service queues
func (sm *ServiceManager) cleanupExpiredRequests() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	now := time.Now()
	
	for serviceName, service := range sm.services {
		// Remove expired requests
		validRequests := make([]*ServiceRequest, 0, len(service.Queue))
		
		for _, request := range service.Queue {
			if request.Timeout.After(now) {
				validRequests = append(validRequests, request)
			}
		}
		
		service.Queue = validRequests
		
		// Remove inactive services with no workers and no requests
		if len(service.Workers) == 0 && 
		   len(service.Queue) == 0 && 
		   time.Since(service.LastActivity) > sm.serviceTimeout {
			delete(sm.services, serviceName)
		}
	}
}

// RestoreCredit restores credit to a worker
func (sm *ServiceManager) RestoreCredit(serviceName, clientID string, credit int32) error {
	reply := make(chan interface{}, 1)
	cmd := serviceCmd{
		action: "restore_credit",
		data: struct {
			Service  string
			ClientID string
			Credit   int32
		}{
			Service:  serviceName,
			ClientID: clientID,
			Credit:   credit,
		},
		reply: reply,
	}
	
	select {
	case sm.commands <- cmd:
		<-reply
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	}
}