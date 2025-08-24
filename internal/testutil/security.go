// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/destiny/zmq4/v25/security/curve"
	"github.com/destiny/zmq4/v25/z85"
)

// TestKeyPair holds a complete test key pair with multiple encodings
type TestKeyPair struct {
	*curve.KeyPair
	PublicZ85  string
	SecretZ85  string
	PublicHex  string
	SecretHex  string
}

// NewTestKeyPair generates a new test key pair with all encodings
func NewTestKeyPair(t testing.TB) *TestKeyPair {
	keyPair, err := curve.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate test key pair: %v", err)
	}
	
	publicZ85, err := keyPair.PublicKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode public key as Z85: %v", err)
	}
	
	secretZ85, err := keyPair.SecretKeyZ85()
	if err != nil {
		t.Fatalf("Failed to encode secret key as Z85: %v", err)
	}
	
	return &TestKeyPair{
		KeyPair:   keyPair,
		PublicZ85: publicZ85,
		SecretZ85: secretZ85,
		PublicHex: keyPair.PublicKeyHex(),
		SecretHex: keyPair.SecretKeyHex(),
	}
}

// TestKeySet holds a complete set of keys for testing
type TestKeySet struct {
	Server *TestKeyPair
	Client *TestKeyPair
}

// NewTestKeySet generates a complete key set for client-server testing
func NewTestKeySet(t testing.TB) *TestKeySet {
	return &TestKeySet{
		Server: NewTestKeyPair(t),
		Client: NewTestKeyPair(t),
	}
}

// ValidateKeyPair validates a key pair for correctness
func ValidateKeyPair(t testing.TB, kp *curve.KeyPair) {
	if len(kp.Public) != curve.KeySize {
		t.Errorf("Invalid public key size: got %d, want %d", len(kp.Public), curve.KeySize)
	}
	
	if len(kp.Secret) != curve.KeySize {
		t.Errorf("Invalid secret key size: got %d, want %d", len(kp.Secret), curve.KeySize)
	}
	
	// Ensure keys are not all zeros
	zeroKey := [curve.KeySize]byte{}
	if kp.Public == zeroKey {
		t.Error("Public key is all zeros")
	}
	if kp.Secret == zeroKey {
		t.Error("Secret key is all zeros")
	}
}

// ValidateZ85Encoding validates Z85 encoding/decoding
func ValidateZ85Encoding(t testing.TB, original []byte, encoded string) {
	// Validate that the encoded string is valid Z85
	if err := z85.ValidateString(encoded); err != nil {
		t.Errorf("Invalid Z85 encoding: %v", err)
	}
	
	// Decode and compare
	decoded, err := z85.DecodeString(encoded)
	if err != nil {
		t.Errorf("Failed to decode Z85 string: %v", err)
	}
	
	if !equalBytes(decoded, original) {
		t.Errorf("Z85 round-trip failed: original=%x, decoded=%x", original, decoded)
	}
}

// equalBytes compares two byte slices
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// GenerateTestNonce generates a test nonce for testing
func GenerateTestNonce() ([24]byte, error) {
	var nonce [24]byte
	_, err := rand.Read(nonce[:])
	return nonce, err
}

// TestSecurityConfig holds security configuration for testing
type TestSecurityConfig struct {
	UseCurve      bool
	UsePlain      bool
	PlainUsername string
	PlainPassword string
	CurveKeys     *TestKeySet
}

// NewCurveTestConfig creates a CURVE security test configuration
func NewCurveTestConfig(t testing.TB) *TestSecurityConfig {
	return &TestSecurityConfig{
		UseCurve:  true,
		CurveKeys: NewTestKeySet(t),
	}
}

// NewPlainTestConfig creates a PLAIN security test configuration
func NewPlainTestConfig() *TestSecurityConfig {
	return &TestSecurityConfig{
		UsePlain:      true,
		PlainUsername: "testuser",
		PlainPassword: "testpass",
	}
}

// NewNullTestConfig creates a NULL security test configuration
func NewNullTestConfig() *TestSecurityConfig {
	return &TestSecurityConfig{}
}

// SecurityTestMatrix holds multiple security configurations for matrix testing
type SecurityTestMatrix struct {
	Null  *TestSecurityConfig
	Plain *TestSecurityConfig
	Curve *TestSecurityConfig
}

// NewSecurityTestMatrix creates a complete security test matrix
func NewSecurityTestMatrix(t testing.TB) *SecurityTestMatrix {
	return &SecurityTestMatrix{
		Null:  NewNullTestConfig(),
		Plain: NewPlainTestConfig(),
		Curve: NewCurveTestConfig(t),
	}
}

// GetSecurityName returns a human-readable security mechanism name
func (c *TestSecurityConfig) GetSecurityName() string {
	switch {
	case c.UseCurve:
		return "CURVE"
	case c.UsePlain:
		return "PLAIN"
	default:
		return "NULL"
	}
}

// CreateCurveClientSecurity creates a CURVE client security instance
func (c *TestSecurityConfig) CreateCurveClientSecurity() *curve.Security {
	if !c.UseCurve || c.CurveKeys == nil {
		return nil
	}
	
	return curve.NewClientSecurity(c.CurveKeys.Client.KeyPair, c.CurveKeys.Server.Public)
}

// CreateCurveServerSecurity creates a CURVE server security instance
func (c *TestSecurityConfig) CreateCurveServerSecurity() *curve.Security {
	if !c.UseCurve || c.CurveKeys == nil {
		return nil
	}
	
	return curve.NewServerSecurity(c.CurveKeys.Server.KeyPair)
}

// MockHandshakeCompletion simulates handshake completion for testing MESSAGE encryption
func MockHandshakeCompletion(clientSec, serverSec *curve.Security) error {
	// Generate transient keys for both sides
	clientTransient, err := curve.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate client transient keys: %w", err)
	}
	
	serverTransient, err := curve.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate server transient keys: %w", err)
	}
	
	// Set transient keys on both sides
	clientSec.SetClientTransient(clientTransient)
	clientSec.SetServerTransient(serverTransient)
	
	serverSec.SetClientTransient(clientTransient)
	serverSec.SetServerTransient(serverTransient)
	
	// Mark handshakes as complete
	clientSec.SetHandshakeComplete()
	serverSec.SetHandshakeComplete()
	
	return nil
}

// ValidateCurveHandshake validates CURVE handshake state
func ValidateCurveHandshake(t testing.TB, sec *curve.Security, expectedState int) {
	if sec.HandshakeState() != expectedState {
		t.Errorf("Invalid handshake state: got %d, want %d", sec.HandshakeState(), expectedState)
	}
	
	if expectedState == 5 && !sec.IsReady() { // HandshakeComplete = 5
		t.Error("Security should be ready after handshake completion")
	}
}

// CreateTestMessage creates a test message with specific content
func CreateTestMessage(content string, size int) []byte {
	if size <= 0 {
		return []byte(content)
	}
	
	msg := make([]byte, size)
	contentBytes := []byte(content)
	
	for i := 0; i < size; i++ {
		msg[i] = contentBytes[i%len(contentBytes)]
	}
	
	return msg
}

// SecurityTestCase represents a single security test case
type SecurityTestCase struct {
	Name     string
	Config   *TestSecurityConfig
	TestFunc func(*testing.T, *TestSecurityConfig)
}

// RunSecurityTestMatrix runs a test function across all security mechanisms
func RunSecurityTestMatrix(t *testing.T, testFunc func(*testing.T, *TestSecurityConfig)) {
	matrix := NewSecurityTestMatrix(t)
	
	configs := []*TestSecurityConfig{
		matrix.Null,
		matrix.Plain,
		matrix.Curve,
	}
	
	for _, config := range configs {
		t.Run(config.GetSecurityName(), func(t *testing.T) {
			testFunc(t, config)
		})
	}
}

// PreGeneratedTestKeys contains pre-generated keys for consistent testing
var PreGeneratedTestKeys = struct {
	ServerPublicZ85  string
	ServerSecretZ85  string
	ClientPublicZ85  string
	ClientSecretZ85  string
}{
	ServerPublicZ85: "Yne@$w-vo<fVvi]a<NY6T1ed:M$fCG*[IaLV{hID",
	ServerSecretZ85: "D:)Q[IlAW!ahhC2ac:9*A}h:p?([4X*7nu}m}V]",
	ClientPublicZ85: "rq:rM>}U?@Lns47E1%kR.o@n%FcmmsL/@{H8]yf7",
	ClientSecretZ85: "Hm2p-[<>m=uF+>p?Z/*6!*H6P?2U}JG@r^!?",
}

// GetPreGeneratedTestKeys returns pre-generated test keys for reproducible testing
func GetPreGeneratedTestKeys(t testing.TB) *TestKeySet {
	serverKeys, err := curve.NewKeyPairFromZ85(PreGeneratedTestKeys.ServerPublicZ85, PreGeneratedTestKeys.ServerSecretZ85)
	if err != nil {
		t.Fatalf("Failed to create server keys from pre-generated Z85: %v", err)
	}
	
	clientKeys, err := curve.NewKeyPairFromZ85(PreGeneratedTestKeys.ClientPublicZ85, PreGeneratedTestKeys.ClientSecretZ85)
	if err != nil {
		t.Fatalf("Failed to create client keys from pre-generated Z85: %v", err)
	}
	
	serverPublicZ85, _ := serverKeys.PublicKeyZ85()
	serverSecretZ85, _ := serverKeys.SecretKeyZ85()
	clientPublicZ85, _ := clientKeys.PublicKeyZ85()
	clientSecretZ85, _ := clientKeys.SecretKeyZ85()
	
	return &TestKeySet{
		Server: &TestKeyPair{
			KeyPair:   serverKeys,
			PublicZ85: serverPublicZ85,
			SecretZ85: serverSecretZ85,
			PublicHex: serverKeys.PublicKeyHex(),
			SecretHex: serverKeys.SecretKeyHex(),
		},
		Client: &TestKeyPair{
			KeyPair:   clientKeys,
			PublicZ85: clientPublicZ85,
			SecretZ85: clientSecretZ85,
			PublicHex: clientKeys.PublicKeyHex(),
			SecretHex: clientKeys.SecretKeyHex(),
		},
	}
}