// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zmq4

import (
	"log"
	"time"
)

// Option configures some aspect of a ZeroMQ socket.
// (e.g. SocketIdentity, Security, ...)
type Option func(s *socket)

// WithID configures a ZeroMQ socket identity.
func WithID(id SocketIdentity) Option {
	return func(s *socket) {
		s.id = id
	}
}

// WithSecurity configures a ZeroMQ socket to use the given security mechanism.
// If the security mechanims is nil, the NULL mechanism is used.
func WithSecurity(sec Security) Option {
	return func(s *socket) {
		s.sec = sec
	}
}

// WithDialerRetry configures the time to wait before two failed attempts
// at dialing an endpoint.
func WithDialerRetry(retry time.Duration) Option {
	return func(s *socket) {
		s.retry = retry
	}
}

// WithDialerTimeout sets the maximum amount of time a dial will wait
// for a connect to complete.
func WithDialerTimeout(timeout time.Duration) Option {
	return func(s *socket) {
		s.dialer.Timeout = timeout
	}
}

// WithTimeout sets the timeout value for socket operations
func WithTimeout(timeout time.Duration) Option {
	return func(s *socket) {
		s.timeout = timeout
	}
}

// WithLogger sets a dedicated log.Logger for the socket.
func WithLogger(msg *log.Logger) Option {
	return func(s *socket) {
		s.log = msg
	}
}

// WithDialerMaxRetries configures the maximum number of retries
// when dialing an endpoint (-1 means infinite retries).
func WithDialerMaxRetries(maxRetries int) Option {
	return func(s *socket) {
		s.maxRetries = maxRetries
	}
}

// WithAutomaticReconnect allows to configure a socket to automatically
// reconnect on connection loss.
func WithAutomaticReconnect(automaticReconnect bool) Option {
	return func(s *socket) {
		s.autoReconnect = automaticReconnect
	}
}

// WithHeartbeatIVL sets the interval between sending ZMTP heartbeats (PING commands).
// If this option is set and is greater than 0, then a PING ZMTP command will be sent
// every heartbeatIVL milliseconds. Per RFC 37/ZMTP 3.1.
func WithHeartbeatIVL(heartbeatIVL time.Duration) Option {
	return func(s *socket) {
		s.SetOption(OptionHeartbeatIVL, heartbeatIVL)
	}
}

// WithHeartbeatTTL sets the timeout on the remote peer for ZMTP heartbeats.
// If this option is greater than 0, the remote side shall time out the connection
// if it does not receive any more traffic within the TTL period.
// TTL is specified in deciseconds (1/10th second) with max value 6553.5 seconds.
// Per RFC 37/ZMTP 3.1.
func WithHeartbeatTTL(heartbeatTTL time.Duration) Option {
	return func(s *socket) {
		s.SetOption(OptionHeartbeatTTL, heartbeatTTL)
	}
}

// WithHeartbeatTimeout sets how long to wait before timing-out a connection
// after sending a PING ZMTP command and not receiving any traffic.
// This option is only valid if heartbeat interval is also set and greater than 0.
// Per RFC 37/ZMTP 3.1.
func WithHeartbeatTimeout(heartbeatTimeout time.Duration) Option {
	return func(s *socket) {
		s.SetOption(OptionHeartbeatTimeout, heartbeatTimeout)
	}
}

/*
// Additional socket options for future implementation

func WithIOThreads(threads int) Option {
	return nil
}

func WithSendBufferSize(size int) Option {
	return nil
}

func WithRecvBufferSize(size int) Option {
	return nil
}
*/

const (
	OptionSubscribe   = "SUBSCRIBE"
	OptionUnsubscribe = "UNSUBSCRIBE"
	OptionHWM         = "HWM"
	
	// ZMTP 3.1 Heartbeat Options (RFC 37)
	OptionHeartbeatIVL     = "ZMQ_HEARTBEAT_IVL"     // Interval between PING commands (milliseconds)
	OptionHeartbeatTTL     = "ZMQ_HEARTBEAT_TTL"     // Timeout for remote peer (deciseconds, max 6553.5s)
	OptionHeartbeatTimeout = "ZMQ_HEARTBEAT_TIMEOUT" // Local timeout after sending PING (milliseconds)
)
