// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Marshal serializes a Malamute message to wire format
func (m *Message) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write message header
	buf.WriteByte(m.Type)
	buf.WriteByte(m.Version)
	binary.Write(buf, binary.BigEndian, m.Sequence)
	
	// Write client ID (length-prefixed string)
	if len(m.ClientID) > MaxClientID {
		return nil, fmt.Errorf("client ID too long: %d", len(m.ClientID))
	}
	buf.WriteByte(byte(len(m.ClientID)))
	buf.WriteString(m.ClientID)
	
	// Write attributes count and attributes
	if len(m.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(m.Attributes))
	}
	buf.WriteByte(byte(len(m.Attributes)))
	for key, value := range m.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	// Write body length and body
	if len(m.Body) > MaxMessageSize {
		return nil, fmt.Errorf("message body too large: %d", len(m.Body))
	}
	binary.Write(buf, binary.BigEndian, uint32(len(m.Body)))
	buf.Write(m.Body)
	
	return buf.Bytes(), nil
}

// Unmarshal deserializes a Malamute message from wire format
func (m *Message) Unmarshal(data []byte) error {
	if len(data) < 8 { // Minimum: 1 type + 1 version + 4 sequence + 1 client_len + 1 attr_count
		return fmt.Errorf("message too short: %d bytes", len(data))
	}
	
	buf := bytes.NewReader(data)
	
	// Read message header
	m.Type, _ = buf.ReadByte()
	m.Version, _ = buf.ReadByte()
	binary.Read(buf, binary.BigEndian, &m.Sequence)
	
	// Read client ID
	clientID, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read client ID: %w", err)
	}
	m.ClientID = clientID
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	m.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		m.Attributes[key] = value
	}
	
	// Read body length and body
	var bodyLen uint32
	if err := binary.Read(buf, binary.BigEndian, &bodyLen); err != nil {
		return fmt.Errorf("failed to read body length: %w", err)
	}
	
	if bodyLen > 0 {
		m.Body = make([]byte, bodyLen)
		if _, err := buf.Read(m.Body); err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
	}
	
	return nil
}

// MarshalConnectionOpen serializes a CONNECTION-OPEN message body
func (c *ConnectionOpenMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write client ID (length-prefixed string)
	if len(c.ClientID) > MaxClientID {
		return nil, fmt.Errorf("client ID too long: %d", len(c.ClientID))
	}
	buf.WriteByte(byte(len(c.ClientID)))
	buf.WriteString(c.ClientID)
	
	// Write client type
	binary.Write(buf, binary.BigEndian, uint32(c.ClientType))
	
	// Write attributes
	if len(c.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(c.Attributes))
	}
	buf.WriteByte(byte(len(c.Attributes)))
	for key, value := range c.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalConnectionOpen deserializes a CONNECTION-OPEN message body
func (c *ConnectionOpenMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read client ID
	clientID, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read client ID: %w", err)
	}
	c.ClientID = clientID
	
	// Read client type
	var clientType uint32
	if err := binary.Read(buf, binary.BigEndian, &clientType); err != nil {
		return fmt.Errorf("failed to read client type: %w", err)
	}
	c.ClientType = int(clientType)
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	c.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		c.Attributes[key] = value
	}
	
	return nil
}

// MarshalStreamWrite serializes a STREAM-WRITE message body
func (s *StreamWriteMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write stream name
	if len(s.Stream) > MaxStreamName {
		return nil, fmt.Errorf("stream name too long: %d", len(s.Stream))
	}
	buf.WriteByte(byte(len(s.Stream)))
	buf.WriteString(s.Stream)
	
	// Write subject
	if len(s.Subject) > 255 {
		return nil, fmt.Errorf("subject too long: %d", len(s.Subject))
	}
	buf.WriteByte(byte(len(s.Subject)))
	buf.WriteString(s.Subject)
	
	// Write attributes
	if len(s.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(s.Attributes))
	}
	buf.WriteByte(byte(len(s.Attributes)))
	for key, value := range s.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	// Write body length and body
	if len(s.Body) > MaxMessageSize {
		return nil, fmt.Errorf("message body too large: %d", len(s.Body))
	}
	binary.Write(buf, binary.BigEndian, uint32(len(s.Body)))
	buf.Write(s.Body)
	
	return buf.Bytes(), nil
}

// UnmarshalStreamWrite deserializes a STREAM-WRITE message body
func (s *StreamWriteMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read stream name
	stream, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read stream: %w", err)
	}
	s.Stream = stream
	
	// Read subject
	subject, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read subject: %w", err)
	}
	s.Subject = subject
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	s.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		s.Attributes[key] = value
	}
	
	// Read body length and body
	var bodyLen uint32
	if err := binary.Read(buf, binary.BigEndian, &bodyLen); err != nil {
		return fmt.Errorf("failed to read body length: %w", err)
	}
	
	if bodyLen > 0 {
		s.Body = make([]byte, bodyLen)
		if _, err := buf.Read(s.Body); err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
	}
	
	return nil
}

// MarshalStreamRead serializes a STREAM-READ message body
func (s *StreamReadMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write stream name
	if len(s.Stream) > MaxStreamName {
		return nil, fmt.Errorf("stream name too long: %d", len(s.Stream))
	}
	buf.WriteByte(byte(len(s.Stream)))
	buf.WriteString(s.Stream)
	
	// Write pattern
	if len(s.Pattern) > 255 {
		return nil, fmt.Errorf("pattern too long: %d", len(s.Pattern))
	}
	buf.WriteByte(byte(len(s.Pattern)))
	buf.WriteString(s.Pattern)
	
	// Write attributes
	if len(s.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(s.Attributes))
	}
	buf.WriteByte(byte(len(s.Attributes)))
	for key, value := range s.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalStreamRead deserializes a STREAM-READ message body
func (s *StreamReadMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read stream name
	stream, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read stream: %w", err)
	}
	s.Stream = stream
	
	// Read pattern
	pattern, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read pattern: %w", err)
	}
	s.Pattern = pattern
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	s.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		s.Attributes[key] = value
	}
	
	return nil
}

// MarshalMailboxSend serializes a MAILBOX-SEND message body
func (m *MailboxSendMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write address
	if len(m.Address) > MaxMailboxName {
		return nil, fmt.Errorf("address too long: %d", len(m.Address))
	}
	buf.WriteByte(byte(len(m.Address)))
	buf.WriteString(m.Address)
	
	// Write subject
	if len(m.Subject) > 255 {
		return nil, fmt.Errorf("subject too long: %d", len(m.Subject))
	}
	buf.WriteByte(byte(len(m.Subject)))
	buf.WriteString(m.Subject)
	
	// Write tracker
	if len(m.Tracker) > 255 {
		return nil, fmt.Errorf("tracker too long: %d", len(m.Tracker))
	}
	buf.WriteByte(byte(len(m.Tracker)))
	buf.WriteString(m.Tracker)
	
	// Write timeout
	binary.Write(buf, binary.BigEndian, m.Timeout)
	
	// Write attributes
	if len(m.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(m.Attributes))
	}
	buf.WriteByte(byte(len(m.Attributes)))
	for key, value := range m.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	// Write body length and body
	if len(m.Body) > MaxMessageSize {
		return nil, fmt.Errorf("message body too large: %d", len(m.Body))
	}
	binary.Write(buf, binary.BigEndian, uint32(len(m.Body)))
	buf.Write(m.Body)
	
	return buf.Bytes(), nil
}

// UnmarshalMailboxSend deserializes a MAILBOX-SEND message body
func (m *MailboxSendMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read address
	address, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read address: %w", err)
	}
	m.Address = address
	
	// Read subject
	subject, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read subject: %w", err)
	}
	m.Subject = subject
	
	// Read tracker
	tracker, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read tracker: %w", err)
	}
	m.Tracker = tracker
	
	// Read timeout
	if err := binary.Read(buf, binary.BigEndian, &m.Timeout); err != nil {
		return fmt.Errorf("failed to read timeout: %w", err)
	}
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	m.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		m.Attributes[key] = value
	}
	
	// Read body length and body
	var bodyLen uint32
	if err := binary.Read(buf, binary.BigEndian, &bodyLen); err != nil {
		return fmt.Errorf("failed to read body length: %w", err)
	}
	
	if bodyLen > 0 {
		m.Body = make([]byte, bodyLen)
		if _, err := buf.Read(m.Body); err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
	}
	
	return nil
}

// MarshalServiceOffer serializes a SERVICE-OFFER message body
func (s *ServiceOfferMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write service name
	if len(s.Service) > MaxServiceName {
		return nil, fmt.Errorf("service name too long: %d", len(s.Service))
	}
	buf.WriteByte(byte(len(s.Service)))
	buf.WriteString(s.Service)
	
	// Write pattern
	if len(s.Pattern) > 255 {
		return nil, fmt.Errorf("pattern too long: %d", len(s.Pattern))
	}
	buf.WriteByte(byte(len(s.Pattern)))
	buf.WriteString(s.Pattern)
	
	// Write attributes
	if len(s.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(s.Attributes))
	}
	buf.WriteByte(byte(len(s.Attributes)))
	for key, value := range s.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalServiceOffer deserializes a SERVICE-OFFER message body
func (s *ServiceOfferMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read service name
	service, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read service: %w", err)
	}
	s.Service = service
	
	// Read pattern
	pattern, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read pattern: %w", err)
	}
	s.Pattern = pattern
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	s.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		s.Attributes[key] = value
	}
	
	return nil
}

// MarshalCreditRequest serializes a CREDIT-REQUEST message body
func (c *CreditRequestMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.BigEndian, c.Credit)
	return buf.Bytes(), nil
}

// UnmarshalCreditRequest deserializes a CREDIT-REQUEST message body
func (c *CreditRequestMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	return binary.Read(buf, binary.BigEndian, &c.Credit)
}

// MarshalError serializes an ERROR message body
func (e *ErrorMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write error code
	binary.Write(buf, binary.BigEndian, uint32(e.Code))
	
	// Write reason
	if len(e.Reason) > 255 {
		return nil, fmt.Errorf("reason too long: %d", len(e.Reason))
	}
	buf.WriteByte(byte(len(e.Reason)))
	buf.WriteString(e.Reason)
	
	// Write attributes
	if len(e.Attributes) > 255 {
		return nil, fmt.Errorf("too many attributes: %d", len(e.Attributes))
	}
	buf.WriteByte(byte(len(e.Attributes)))
	for key, value := range e.Attributes {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("attribute key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalError deserializes an ERROR message body
func (e *ErrorMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read error code
	var code uint32
	if err := binary.Read(buf, binary.BigEndian, &code); err != nil {
		return fmt.Errorf("failed to read error code: %w", err)
	}
	e.Code = int(code)
	
	// Read reason
	reason, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read reason: %w", err)
	}
	e.Reason = reason
	
	// Read attributes
	attrCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read attribute count: %w", err)
	}
	
	e.Attributes = make(map[string]string)
	for i := 0; i < int(attrCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read attribute value %d: %w", i, err)
		}
		e.Attributes[key] = value
	}
	
	return nil
}

// Helper functions

// readString reads a length-prefixed string from a reader
func readString(r io.Reader) (string, error) {
	lengthBuf := make([]byte, 1)
	if _, err := r.Read(lengthBuf); err != nil {
		return "", err
	}
	
	length := int(lengthBuf[0])
	if length == 0 {
		return "", nil
	}
	
	strBuf := make([]byte, length)
	if _, err := r.Read(strBuf); err != nil {
		return "", err
	}
	
	return string(strBuf), nil
}

// CreateMessage creates a Malamute message with common fields
func CreateMessage(msgType byte, clientID string, sequence uint32) *Message {
	return &Message{
		Type:       msgType,
		Version:    ProtocolVersion,
		Sequence:   sequence,
		ClientID:   clientID,
		Attributes: make(map[string]string),
	}
}

// CreateErrorMessage creates an ERROR message
func CreateErrorMessage(code int, reason string, clientID string, sequence uint32) *Message {
	errorMsg := &ErrorMessage{
		Code:       code,
		Reason:     reason,
		Attributes: make(map[string]string),
	}
	
	body, _ := errorMsg.Marshal()
	
	return &Message{
		Type:       MessageTypeError,
		Version:    ProtocolVersion,
		Sequence:   sequence,
		ClientID:   clientID,
		Attributes: make(map[string]string),
		Body:       body,
	}
}