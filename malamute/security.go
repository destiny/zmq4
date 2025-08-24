// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"sync"

	"github.com/destiny/zmq4/v25"
)

// SecurityMechanism defines the security interface for Malamute
type SecurityMechanism interface {
	Apply(socket zmq4.Socket) error
	GetMechanism() zmq4.SecurityMechanism
	GetCredentials() map[string]string
}

// PlainSecurity implements PLAIN authentication
type PlainSecurity struct {
	Username string
	Password string
}

// CurveSecurity implements CURVE encryption
type CurveSecurity struct {
	ServerKey string
	PublicKey string
	SecretKey string
}

// NullSecurity implements no security (for development)
type NullSecurity struct{}

// SecurityManager manages authentication and encryption for Malamute
type SecurityManager struct {
	mechanism SecurityMechanism
	mutex     sync.RWMutex
}

// NewPlainSecurity creates PLAIN security mechanism
func NewPlainSecurity(username, password string) *PlainSecurity {
	return &PlainSecurity{
		Username: username,
		Password: password,
	}
}

// Apply applies PLAIN security to a socket
func (ps *PlainSecurity) Apply(socket zmq4.Socket) error {
	if err := socket.SetOption(zmq4.OptionPlainUsername, ps.Username); err != nil {
		return fmt.Errorf("failed to set PLAIN username: %w", err)
	}
	
	if err := socket.SetOption(zmq4.OptionPlainPassword, ps.Password); err != nil {
		return fmt.Errorf("failed to set PLAIN password: %w", err)
	}
	
	return nil
}

// GetMechanism returns the security mechanism type
func (ps *PlainSecurity) GetMechanism() zmq4.SecurityMechanism {
	return zmq4.PlainSecurity
}

// GetCredentials returns the credentials
func (ps *PlainSecurity) GetCredentials() map[string]string {
	return map[string]string{
		"username": ps.Username,
		"password": ps.Password,
	}
}

// NewCurveSecurity creates CURVE security mechanism
func NewCurveSecurity(serverKey, publicKey, secretKey string) *CurveSecurity {
	return &CurveSecurity{
		ServerKey: serverKey,
		PublicKey: publicKey,
		SecretKey: secretKey,
	}
}

// Apply applies CURVE security to a socket
func (cs *CurveSecurity) Apply(socket zmq4.Socket) error {
	if err := socket.SetOption(zmq4.OptionCurveServerKey, cs.ServerKey); err != nil {
		return fmt.Errorf("failed to set CURVE server key: %w", err)
	}
	
	if err := socket.SetOption(zmq4.OptionCurvePublicKey, cs.PublicKey); err != nil {
		return fmt.Errorf("failed to set CURVE public key: %w", err)
	}
	
	if err := socket.SetOption(zmq4.OptionCurveSecretKey, cs.SecretKey); err != nil {
		return fmt.Errorf("failed to set CURVE secret key: %w", err)
	}
	
	return nil
}

// GetMechanism returns the security mechanism type
func (cs *CurveSecurity) GetMechanism() zmq4.SecurityMechanism {
	return zmq4.CurveSecurity
}

// GetCredentials returns the credentials
func (cs *CurveSecurity) GetCredentials() map[string]string {
	return map[string]string{
		"server_key": cs.ServerKey,
		"public_key": cs.PublicKey,
		"secret_key": cs.SecretKey,
	}
}

// NewNullSecurity creates NULL security mechanism (no security)
func NewNullSecurity() *NullSecurity {
	return &NullSecurity{}
}

// Apply applies NULL security to a socket (no-op)
func (ns *NullSecurity) Apply(socket zmq4.Socket) error {
	return nil // No security configuration needed
}

// GetMechanism returns the security mechanism type
func (ns *NullSecurity) GetMechanism() zmq4.SecurityMechanism {
	return zmq4.NullSecurity
}

// GetCredentials returns empty credentials
func (ns *NullSecurity) GetCredentials() map[string]string {
	return make(map[string]string)
}

// NewSecurityManager creates a new security manager
func NewSecurityManager(mechanism SecurityMechanism) *SecurityManager {
	return &SecurityManager{
		mechanism: mechanism,
	}
}

// SetMechanism sets the security mechanism
func (sm *SecurityManager) SetMechanism(mechanism SecurityMechanism) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.mechanism = mechanism
}

// ApplyToSocket applies security to a socket
func (sm *SecurityManager) ApplyToSocket(socket zmq4.Socket) error {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	if sm.mechanism == nil {
		return fmt.Errorf("no security mechanism configured")
	}
	
	return sm.mechanism.Apply(socket)
}

// GetMechanism returns the current mechanism
func (sm *SecurityManager) GetMechanism() SecurityMechanism {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.mechanism
}

// SecureBroker wraps a broker with security
type SecureBroker struct {
	*Broker
	security *SecurityManager
}

// NewSecureBroker creates a broker with security
func NewSecureBroker(config *BrokerConfig, security SecurityMechanism) (*SecureBroker, error) {
	broker, err := NewBroker(config)
	if err != nil {
		return nil, err
	}
	
	secureBroker := &SecureBroker{
		Broker:   broker,
		security: NewSecurityManager(security),
	}
	
	return secureBroker, nil
}

// Start starts the secure broker
func (sb *SecureBroker) Start() error {
	// Apply security to the router socket before starting
	if err := sb.security.ApplyToSocket(sb.routerSocket); err != nil {
		return fmt.Errorf("failed to apply security to broker socket: %w", err)
	}
	
	return sb.Broker.Start()
}

// SecureClient wraps a client with security
type SecureClient struct {
	*MalamuteClient
	security *SecurityManager
}

// NewSecureClient creates a client with security
func NewSecureClient(config *ClientConfig, security SecurityMechanism) (*SecureClient, error) {
	client, err := NewMalamuteClient(config)
	if err != nil {
		return nil, err
	}
	
	secureClient := &SecureClient{
		MalamuteClient: client,
		security:       NewSecurityManager(security),
	}
	
	return secureClient, nil
}

// Connect connects with security
func (sc *SecureClient) Connect() error {
	// Apply security to the dealer socket before connecting
	if err := sc.security.ApplyToSocket(sc.dealerSocket); err != nil {
		return fmt.Errorf("failed to apply security to client socket: %w", err)
	}
	
	return sc.MalamuteClient.Connect()
}

// CertificateManager manages CURVE certificates
type CertificateManager struct {
	certificates map[string]*CurveCertificate
	mutex        sync.RWMutex
}

// CurveCertificate represents a CURVE certificate
type CurveCertificate struct {
	PublicKey  string
	SecretKey  string
	Metadata   map[string]string
	CreatedAt  int64
	ExpiresAt  int64
}

// NewCertificateManager creates a new certificate manager
func NewCertificateManager() *CertificateManager {
	return &CertificateManager{
		certificates: make(map[string]*CurveCertificate),
	}
}

// AddCertificate adds a certificate
func (cm *CertificateManager) AddCertificate(id string, cert *CurveCertificate) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.certificates[id] = cert
}

// GetCertificate retrieves a certificate
func (cm *CertificateManager) GetCertificate(id string) (*CurveCertificate, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cert, exists := cm.certificates[id]
	return cert, exists
}

// RemoveCertificate removes a certificate
func (cm *CertificateManager) RemoveCertificate(id string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	delete(cm.certificates, id)
}

// ValidateCertificate validates a certificate
func (cm *CertificateManager) ValidateCertificate(id string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	
	cert, exists := cm.certificates[id]
	if !exists {
		return false
	}
	
	// Check expiration
	now := GetCurrentTimestamp()
	if cert.ExpiresAt > 0 && now > cert.ExpiresAt {
		return false
	}
	
	return true
}

// AuthenticationHandler handles client authentication
type AuthenticationHandler struct {
	authorizedClients map[string]bool
	certManager       *CertificateManager
	mutex             sync.RWMutex
}

// NewAuthenticationHandler creates an authentication handler
func NewAuthenticationHandler() *AuthenticationHandler {
	return &AuthenticationHandler{
		authorizedClients: make(map[string]bool),
		certManager:       NewCertificateManager(),
	}
}

// AuthorizeClient authorizes a client
func (ah *AuthenticationHandler) AuthorizeClient(clientID string) {
	ah.mutex.Lock()
	defer ah.mutex.Unlock()
	ah.authorizedClients[clientID] = true
}

// RevokeClient revokes a client's authorization
func (ah *AuthenticationHandler) RevokeClient(clientID string) {
	ah.mutex.Lock()
	defer ah.mutex.Unlock()
	delete(ah.authorizedClients, clientID)
}

// IsAuthorized checks if a client is authorized
func (ah *AuthenticationHandler) IsAuthorized(clientID string) bool {
	ah.mutex.RLock()
	defer ah.mutex.RUnlock()
	return ah.authorizedClients[clientID]
}

// GetCertificateManager returns the certificate manager
func (ah *AuthenticationHandler) GetCertificateManager() *CertificateManager {
	return ah.certManager
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	Mechanism       string            // Security mechanism (null, plain, curve)
	PlainUsername   string            // PLAIN username
	PlainPassword   string            // PLAIN password
	CurveServerKey  string            // CURVE server key
	CurvePublicKey  string            // CURVE public key
	CurveSecretKey  string            // CURVE secret key
	CertificateFile string            // Certificate file path
	KeyFile         string            // Private key file path
	Attributes      map[string]string // Additional attributes
}

// CreateSecurityFromConfig creates security mechanism from config
func CreateSecurityFromConfig(config *SecurityConfig) (SecurityMechanism, error) {
	if config == nil {
		return NewNullSecurity(), nil
	}
	
	switch config.Mechanism {
	case "null", "":
		return NewNullSecurity(), nil
		
	case "plain":
		if config.PlainUsername == "" {
			return nil, fmt.Errorf("PLAIN username required")
		}
		return NewPlainSecurity(config.PlainUsername, config.PlainPassword), nil
		
	case "curve":
		if config.CurveServerKey == "" || config.CurvePublicKey == "" || config.CurveSecretKey == "" {
			return nil, fmt.Errorf("CURVE keys required")
		}
		return NewCurveSecurity(config.CurveServerKey, config.CurvePublicKey, config.CurveSecretKey), nil
		
	default:
		return nil, fmt.Errorf("unsupported security mechanism: %s", config.Mechanism)
	}
}

// ApplyBrokerSecurity applies security to a broker configuration
func ApplyBrokerSecurity(config *BrokerConfig, security *SecurityConfig) (*SecureBroker, error) {
	mechanism, err := CreateSecurityFromConfig(security)
	if err != nil {
		return nil, err
	}
	
	return NewSecureBroker(config, mechanism)
}

// ApplyClientSecurity applies security to a client configuration
func ApplyClientSecurity(config *ClientConfig, security *SecurityConfig) (*SecureClient, error) {
	mechanism, err := CreateSecurityFromConfig(security)
	if err != nil {
		return nil, err
	}
	
	return NewSecureClient(config, mechanism)
}

// CreateSecureEndpoint creates a secure endpoint with the given security
func CreateSecureEndpoint(baseEndpoint string, security SecurityMechanism) string {
	// In a real implementation, this might modify the endpoint based on security requirements
	// For now, we return the base endpoint
	return baseEndpoint
}

// ValidateSecurityConfig validates a security configuration
func ValidateSecurityConfig(config *SecurityConfig) error {
	if config == nil {
		return nil // NULL security is valid
	}
	
	switch config.Mechanism {
	case "null", "":
		// No validation needed for NULL security
		return nil
		
	case "plain":
		if config.PlainUsername == "" {
			return fmt.Errorf("PLAIN username is required")
		}
		// Password can be empty for some use cases
		return nil
		
	case "curve":
		if config.CurveServerKey == "" {
			return fmt.Errorf("CURVE server key is required")
		}
		if config.CurvePublicKey == "" {
			return fmt.Errorf("CURVE public key is required")
		}
		if config.CurveSecretKey == "" {
			return fmt.Errorf("CURVE secret key is required")
		}
		
		// Validate key formats (basic check)
		if len(config.CurveServerKey) != 40 {
			return fmt.Errorf("CURVE server key must be 40 characters")
		}
		if len(config.CurvePublicKey) != 40 {
			return fmt.Errorf("CURVE public key must be 40 characters")
		}
		if len(config.CurveSecretKey) != 40 {
			return fmt.Errorf("CURVE secret key must be 40 characters")
		}
		
		return nil
		
	default:
		return fmt.Errorf("unsupported security mechanism: %s", config.Mechanism)
	}
}