// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CreditController manages credit-based flow control for Malamute
type CreditController struct {
	// Configuration
	windowSize    int32             // Credit window size
	lowWatermark  int32             // Low watermark for credit renewal
	highWatermark int32             // High watermark for credit limiting
	
	// Client credit tracking
	clientCredits map[string]*ClientCredit // Credit per client
	
	// Channel-based communication
	requests     chan *CreditRequest       // Credit requests
	confirmations chan *CreditConfirmation  // Credit confirmations
	commands     chan creditCmd            // Credit control commands
	
	// State management
	ctx          context.Context   // Context for cancellation
	cancel       context.CancelFunc // Cancel function
	wg           sync.WaitGroup    // Wait group for goroutines
	mutex        sync.RWMutex      // Protects credit state
	statistics   *CreditStatistics // Credit statistics
}

// ClientCredit tracks credit for a specific client
type ClientCredit struct {
	ClientID     string    // Client identifier
	Available    int32     // Available credit
	Total        int32     // Total credit assigned
	Used         int32     // Used credit
	Reserved     int32     // Reserved credit
	LastRequest  time.Time // Last credit request
	LastConfirm  time.Time // Last credit confirmation
	RequestCount uint64    // Total requests
	ConfirmCount uint64    // Total confirmations
	DenialCount  uint64    // Total denials
}

// CreditRequest represents a credit request
type CreditRequest struct {
	ClientID    string    // Client requesting credit
	Amount      int32     // Requested credit amount
	Priority    int       // Request priority
	Timestamp   time.Time // Request timestamp
	Response    chan *CreditResponse // Response channel
}

// CreditResponse represents a credit response
type CreditResponse struct {
	Granted   int32  // Granted credit amount
	Available int32  // Total available credit
	Error     error  // Error if request failed
}

// CreditConfirmation represents a credit confirmation/return
type CreditConfirmation struct {
	ClientID  string    // Client returning credit
	Amount    int32     // Returned credit amount
	Reason    string    // Reason for return
	Timestamp time.Time // Confirmation timestamp
}

// creditCmd represents internal credit commands
type creditCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// CreditStatistics holds credit system statistics
type CreditStatistics struct {
	TotalRequests     uint64 // Total credit requests
	TotalConfirmations uint64 // Total credit confirmations
	TotalGranted      uint64 // Total credit granted
	TotalDenied       uint64 // Total credit denied
	TotalReturned     uint64 // Total credit returned
	ActiveClients     uint64 // Clients with active credit
	TotalCredit       int32  // Total credit in system
	AvailableCredit   int32  // Available credit pool
	UsedCredit        int32  // Currently used credit
	LastUpdate        time.Time // Last statistics update
}

// NewCreditController creates a new credit controller
func NewCreditController(windowSize int32) *CreditController {
	if windowSize <= 0 {
		windowSize = DefaultCreditWindow
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &CreditController{
		windowSize:     windowSize,
		lowWatermark:   windowSize / 4,
		highWatermark:  windowSize * 3 / 4,
		clientCredits:  make(map[string]*ClientCredit),
		requests:       make(chan *CreditRequest, 1000),
		confirmations:  make(chan *CreditConfirmation, 1000),
		commands:       make(chan creditCmd, 100),
		ctx:            ctx,
		cancel:         cancel,
		statistics:     &CreditStatistics{},
	}
}

// Start begins credit controller operation
func (cc *CreditController) Start() {
	cc.wg.Add(1)
	go cc.requestLoop()
	
	cc.wg.Add(1)
	go cc.confirmationLoop()
	
	cc.wg.Add(1)
	go cc.commandLoop()
	
	cc.wg.Add(1)
	go cc.cleanupLoop()
}

// Stop stops the credit controller
func (cc *CreditController) Stop() {
	cc.cancel()
	cc.wg.Wait()
	
	close(cc.requests)
	close(cc.confirmations)
	close(cc.commands)
}

// RequestCredit requests credit for a client
func (cc *CreditController) RequestCredit(clientID string, amount int32, priority int) (*CreditResponse, error) {
	response := make(chan *CreditResponse, 1)
	
	request := &CreditRequest{
		ClientID:  clientID,
		Amount:    amount,
		Priority:  priority,
		Timestamp: time.Now(),
		Response:  response,
	}
	
	select {
	case cc.requests <- request:
		// Request queued successfully
	case <-cc.ctx.Done():
		return nil, cc.ctx.Err()
	default:
		return nil, fmt.Errorf("credit request queue full")
	}
	
	// Wait for response
	select {
	case resp := <-response:
		return resp, resp.Error
	case <-cc.ctx.Done():
		return nil, cc.ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("credit request timeout")
	}
}

// ConfirmCredit confirms/returns credit from a client
func (cc *CreditController) ConfirmCredit(clientID string, amount int32, reason string) error {
	confirmation := &CreditConfirmation{
		ClientID:  clientID,
		Amount:    amount,
		Reason:    reason,
		Timestamp: time.Now(),
	}
	
	select {
	case cc.confirmations <- confirmation:
		return nil
	case <-cc.ctx.Done():
		return cc.ctx.Err()
	default:
		return fmt.Errorf("credit confirmation queue full")
	}
}

// GetClientCredit returns credit information for a client
func (cc *CreditController) GetClientCredit(clientID string) (*ClientCredit, error) {
	reply := make(chan interface{}, 1)
	cmd := creditCmd{
		action: "get_client_credit",
		data:   clientID,
		reply:  reply,
	}
	
	select {
	case cc.commands <- cmd:
		result := <-reply
		if credit, ok := result.(*ClientCredit); ok {
			return credit, nil
		}
		return nil, fmt.Errorf("client not found")
	case <-cc.ctx.Done():
		return nil, cc.ctx.Err()
	}
}

// SetWindowSize sets the credit window size
func (cc *CreditController) SetWindowSize(size int32) error {
	if size < MinCreditWindow || size > MaxCreditWindow {
		return fmt.Errorf("invalid window size: %d", size)
	}
	
	reply := make(chan interface{}, 1)
	cmd := creditCmd{
		action: "set_window_size",
		data:   size,
		reply:  reply,
	}
	
	select {
	case cc.commands <- cmd:
		<-reply
		return nil
	case <-cc.ctx.Done():
		return cc.ctx.Err()
	}
}

// GetStatistics returns credit statistics
func (cc *CreditController) GetStatistics() *CreditStatistics {
	reply := make(chan interface{}, 1)
	cmd := creditCmd{
		action: "get_statistics",
		reply:  reply,
	}
	
	select {
	case cc.commands <- cmd:
		result := <-reply
		return result.(*CreditStatistics)
	case <-cc.ctx.Done():
		return &CreditStatistics{}
	}
}

// RemoveClient removes a client from credit tracking
func (cc *CreditController) RemoveClient(clientID string) error {
	reply := make(chan interface{}, 1)
	cmd := creditCmd{
		action: "remove_client",
		data:   clientID,
		reply:  reply,
	}
	
	select {
	case cc.commands <- cmd:
		<-reply
		return nil
	case <-cc.ctx.Done():
		return cc.ctx.Err()
	}
}

// requestLoop processes credit requests
func (cc *CreditController) requestLoop() {
	defer cc.wg.Done()
	
	for {
		select {
		case request := <-cc.requests:
			cc.processRequest(request)
		case <-cc.ctx.Done():
			return
		}
	}
}

// confirmationLoop processes credit confirmations
func (cc *CreditController) confirmationLoop() {
	defer cc.wg.Done()
	
	for {
		select {
		case confirmation := <-cc.confirmations:
			cc.processConfirmation(confirmation)
		case <-cc.ctx.Done():
			return
		}
	}
}

// commandLoop processes credit control commands
func (cc *CreditController) commandLoop() {
	defer cc.wg.Done()
	
	for {
		select {
		case cmd := <-cc.commands:
			cc.handleCommand(cmd)
		case <-cc.ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically cleans up inactive clients
func (cc *CreditController) cleanupLoop() {
	defer cc.wg.Done()
	
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			cc.cleanupInactiveClients()
		case <-cc.ctx.Done():
			return
		}
	}
}

// processRequest processes a credit request
func (cc *CreditController) processRequest(request *CreditRequest) {
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	
	// Get or create client credit
	clientCredit, exists := cc.clientCredits[request.ClientID]
	if !exists {
		clientCredit = &ClientCredit{
			ClientID:    request.ClientID,
			Available:   0,
			Total:       0,
			Used:        0,
			Reserved:    0,
			LastRequest: time.Now(),
		}
		cc.clientCredits[request.ClientID] = clientCredit
	}
	
	// Update request tracking
	clientCredit.LastRequest = time.Now()
	clientCredit.RequestCount++
	cc.statistics.TotalRequests++
	
	// Determine how much credit to grant
	granted := cc.calculateGrantedCredit(clientCredit, request.Amount)
	
	if granted > 0 {
		// Grant credit
		clientCredit.Available += granted
		clientCredit.Total += granted
		clientCredit.LastConfirm = time.Now()
		clientCredit.ConfirmCount++
		
		cc.statistics.TotalGranted += uint64(granted)
		cc.statistics.TotalConfirmations++
		
		// Send positive response
		response := &CreditResponse{
			Granted:   granted,
			Available: clientCredit.Available,
		}
		
		select {
		case request.Response <- response:
		default:
			// Response channel full
		}
	} else {
		// Deny credit
		clientCredit.DenialCount++
		cc.statistics.TotalDenied++
		
		// Send negative response
		response := &CreditResponse{
			Granted:   0,
			Available: clientCredit.Available,
			Error:     fmt.Errorf("insufficient credit available"),
		}
		
		select {
		case request.Response <- response:
		default:
			// Response channel full
		}
	}
}

// processConfirmation processes a credit confirmation
func (cc *CreditController) processConfirmation(confirmation *CreditConfirmation) {
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	
	clientCredit, exists := cc.clientCredits[confirmation.ClientID]
	if !exists {
		return // Client not found
	}
	
	// Return credit
	if confirmation.Amount > 0 {
		clientCredit.Used -= confirmation.Amount
		if clientCredit.Used < 0 {
			clientCredit.Used = 0
		}
		
		cc.statistics.TotalReturned += uint64(confirmation.Amount)
	}
}

// handleCommand processes a credit control command
func (cc *CreditController) handleCommand(cmd creditCmd) {
	switch cmd.action {
	case "get_client_credit":
		clientID := cmd.data.(string)
		cc.mutex.RLock()
		if clientCredit, exists := cc.clientCredits[clientID]; exists {
			// Return a copy
			creditCopy := *clientCredit
			cmd.reply <- &creditCopy
		} else {
			cmd.reply <- nil
		}
		cc.mutex.RUnlock()
		
	case "set_window_size":
		size := cmd.data.(int32)
		cc.mutex.Lock()
		cc.windowSize = size
		cc.lowWatermark = size / 4
		cc.highWatermark = size * 3 / 4
		cc.mutex.Unlock()
		cmd.reply <- nil
		
	case "get_statistics":
		cc.mutex.RLock()
		stats := cc.calculateStatistics()
		cc.mutex.RUnlock()
		cmd.reply <- stats
		
	case "remove_client":
		clientID := cmd.data.(string)
		cc.mutex.Lock()
		delete(cc.clientCredits, clientID)
		cc.mutex.Unlock()
		cmd.reply <- nil
	}
}

// calculateGrantedCredit calculates how much credit to grant
func (cc *CreditController) calculateGrantedCredit(clientCredit *ClientCredit, requested int32) int32 {
	// Check if client has too much credit already
	if clientCredit.Available >= cc.highWatermark {
		return 0
	}
	
	// Calculate maximum credit this client can have
	maxCredit := cc.windowSize
	if clientCredit.Total >= maxCredit {
		return 0
	}
	
	// Calculate available credit to grant
	available := maxCredit - clientCredit.Total
	
	// Grant the minimum of requested and available
	granted := requested
	if granted > available {
		granted = available
	}
	
	// Don't grant more than window size at once
	if granted > cc.windowSize/2 {
		granted = cc.windowSize / 2
	}
	
	return granted
}

// calculateStatistics calculates current statistics
func (cc *CreditController) calculateStatistics() *CreditStatistics {
	stats := *cc.statistics
	
	// Calculate current state
	stats.ActiveClients = uint64(len(cc.clientCredits))
	stats.TotalCredit = 0
	stats.AvailableCredit = 0
	stats.UsedCredit = 0
	
	for _, clientCredit := range cc.clientCredits {
		stats.TotalCredit += clientCredit.Total
		stats.AvailableCredit += clientCredit.Available
		stats.UsedCredit += clientCredit.Used
	}
	
	stats.LastUpdate = time.Now()
	return &stats
}

// cleanupInactiveClients removes clients that haven't been active
func (cc *CreditController) cleanupInactiveClients() {
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	
	cutoff := time.Now().Add(-60 * time.Minute) // 1 hour timeout
	
	for clientID, clientCredit := range cc.clientCredits {
		if clientCredit.LastRequest.Before(cutoff) && clientCredit.Available == 0 && clientCredit.Used == 0 {
			delete(cc.clientCredits, clientID)
		}
	}
}

// UseCredit marks credit as used by a client
func (cc *CreditController) UseCredit(clientID string, amount int32) error {
	cc.mutex.Lock()
	defer cc.mutex.Unlock()
	
	clientCredit, exists := cc.clientCredits[clientID]
	if !exists {
		return fmt.Errorf("client not found: %s", clientID)
	}
	
	if clientCredit.Available < amount {
		return fmt.Errorf("insufficient credit: available=%d, requested=%d", clientCredit.Available, amount)
	}
	
	clientCredit.Available -= amount
	clientCredit.Used += amount
	
	return nil
}

// ReturnCredit returns unused credit from a client
func (cc *CreditController) ReturnCredit(clientID string, amount int32) error {
	return cc.ConfirmCredit(clientID, amount, "unused")
}

// CheckCredit checks if a client has sufficient credit
func (cc *CreditController) CheckCredit(clientID string, amount int32) bool {
	cc.mutex.RLock()
	defer cc.mutex.RUnlock()
	
	clientCredit, exists := cc.clientCredits[clientID]
	if !exists {
		return false
	}
	
	return clientCredit.Available >= amount
}

// GetAvailableCredit returns the available credit for a client
func (cc *CreditController) GetAvailableCredit(clientID string) int32 {
	cc.mutex.RLock()
	defer cc.mutex.RUnlock()
	
	clientCredit, exists := cc.clientCredits[clientID]
	if !exists {
		return 0
	}
	
	return clientCredit.Available
}