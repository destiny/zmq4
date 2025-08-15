// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package majordomo

import (
	"fmt"

	"github.com/destiny/zmq4/v25"
	"github.com/destiny/zmq4/v25/security/curve"
)

// CURVESecurityConfig holds CURVE security configuration
type CURVESecurityConfig struct {
	ServerKeys   *curve.KeyPair // Server key pair (required for server/broker)
	ClientKeys   *curve.KeyPair // Client key pair (required for client/worker)
	ServerPubKey [32]byte       // Server public key (required for client/worker)
}

// NewCURVEServerSecurity creates CURVE security for a server (broker)
func NewCURVEServerSecurity(serverKeys *curve.KeyPair) *CURVESecurityConfig {
	return &CURVESecurityConfig{
		ServerKeys: serverKeys,
	}
}

// NewCURVEClientSecurity creates CURVE security for a client (worker/client)
func NewCURVEClientSecurity(clientKeys *curve.KeyPair, serverPublicKey [32]byte) *CURVESecurityConfig {
	return &CURVESecurityConfig{
		ClientKeys:   clientKeys,
		ServerPubKey: serverPublicKey,
	}
}

// ToZMQSecurity converts the CURVE config to ZMQ security mechanism
func (c *CURVESecurityConfig) ToZMQSecurity(isServer bool) (zmq4.Security, error) {
	if isServer {
		if c.ServerKeys == nil {
			return nil, fmt.Errorf("mdp: server keys required for server security")
		}
		return curve.NewServerSecurity(c.ServerKeys), nil
	} else {
		if c.ClientKeys == nil {
			return nil, fmt.Errorf("mdp: client keys required for client security")
		}
		return curve.NewClientSecurity(c.ClientKeys, c.ServerPubKey), nil
	}
}

// NewBrokerWithCURVE creates a new MDP broker with CURVE security
func NewBrokerWithCURVE(endpoint string, serverKeys *curve.KeyPair, options *BrokerOptions) (*Broker, error) {
	if serverKeys == nil {
		return nil, fmt.Errorf("mdp: server keys required for CURVE broker")
	}
	
	if options == nil {
		options = DefaultBrokerOptions()
	}
	
	// Set up CURVE security
	options.Security = curve.NewServerSecurity(serverKeys)
	
	return NewBroker(endpoint, options), nil
}

// NewWorkerWithCURVE creates a new MDP worker with CURVE security
func NewWorkerWithCURVE(service ServiceName, brokerEndpoint string, handler RequestHandler, 
	clientKeys *curve.KeyPair, serverPublicKey [32]byte, options *WorkerOptions) (*Worker, error) {
	
	if clientKeys == nil {
		return nil, fmt.Errorf("mdp: client keys required for CURVE worker")
	}
	
	if options == nil {
		options = DefaultWorkerOptions()
	}
	
	// Set up CURVE security
	options.Security = curve.NewClientSecurity(clientKeys, serverPublicKey)
	
	return NewWorker(service, brokerEndpoint, handler, options)
}

// NewClientWithCURVE creates a new MDP client with CURVE security
func NewClientWithCURVE(brokerEndpoint string, clientKeys *curve.KeyPair, 
	serverPublicKey [32]byte, options *ClientOptions) (*Client, error) {
	
	if clientKeys == nil {
		return nil, fmt.Errorf("mdp: client keys required for CURVE client")
	}
	
	if options == nil {
		options = DefaultClientOptions()
	}
	
	// Set up CURVE security
	options.Security = curve.NewClientSecurity(clientKeys, serverPublicKey)
	
	return NewClient(brokerEndpoint, options), nil
}

// NewAsyncClientWithCURVE creates a new MDP async client with CURVE security  
func NewAsyncClientWithCURVE(brokerEndpoint string, clientKeys *curve.KeyPair,
	serverPublicKey [32]byte, options *ClientOptions) (*AsyncClient, error) {
	
	if clientKeys == nil {
		return nil, fmt.Errorf("mdp: client keys required for CURVE async client")
	}
	
	if options == nil {
		options = DefaultClientOptions()
	}
	
	// Set up CURVE security
	options.Security = curve.NewClientSecurity(clientKeys, serverPublicKey)
	
	return NewAsyncClient(brokerEndpoint, options), nil
}

// GenerateCURVEKeys generates a new CURVE key pair for use with majordomo
func GenerateCURVEKeys() (*curve.KeyPair, error) {
	return curve.GenerateKeyPair()
}

// LoadCURVEKeysFromZ85 loads CURVE keys from Z85-encoded strings
func LoadCURVEKeysFromZ85(publicZ85, secretZ85 string) (*curve.KeyPair, error) {
	return curve.NewKeyPairFromZ85(publicZ85, secretZ85)
}

// LoadCURVEKeysFromHex loads CURVE keys from hex-encoded strings
func LoadCURVEKeysFromHex(publicHex, secretHex string) (*curve.KeyPair, error) {
	return curve.NewKeyPairFromHex(publicHex, secretHex)
}

// ValidateCURVEKey validates a Z85-encoded CURVE key
func ValidateCURVEKey(keyZ85 string) error {
	return curve.ValidateZ85Key(keyZ85)
}