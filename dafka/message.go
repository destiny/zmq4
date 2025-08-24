// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dafka

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Marshal serializes a DAFKA message to wire format
func (m *Message) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write protocol signature and version
	buf.WriteByte(ProtocolSignature1)
	buf.WriteByte(ProtocolSignature2)
	buf.WriteByte(m.Version)
	
	// Write message type
	buf.WriteByte(m.Type)
	
	// Write topic (length-prefixed string)
	if len(m.Topic) > MaxTopicLength {
		return nil, fmt.Errorf("topic too long: %d", len(m.Topic))
	}
	buf.WriteByte(byte(len(m.Topic)))
	buf.WriteString(m.Topic)
	
	// Write message body
	buf.Write(m.Body)
	
	return buf.Bytes(), nil
}

// Unmarshal deserializes a DAFKA message from wire format
func (m *Message) Unmarshal(data []byte) error {
	if len(data) < 5 { // Minimum: 2 sig + 1 ver + 1 type + 1 topic_len
		return fmt.Errorf("message too short: %d bytes", len(data))
	}
	
	buf := bytes.NewReader(data)
	
	// Read and verify protocol signature
	sig1, _ := buf.ReadByte()
	sig2, _ := buf.ReadByte()
	if sig1 != ProtocolSignature1 || sig2 != ProtocolSignature2 {
		return fmt.Errorf("invalid protocol signature: %02x %02x", sig1, sig2)
	}
	
	// Read version
	version, _ := buf.ReadByte()
	m.Version = version
	
	// Read message type
	msgType, _ := buf.ReadByte()
	m.Type = msgType
	
	// Read topic
	topicLen, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read topic length: %w", err)
	}
	
	if topicLen > 0 {
		topicBytes := make([]byte, topicLen)
		if _, err := buf.Read(topicBytes); err != nil {
			return fmt.Errorf("failed to read topic: %w", err)
		}
		m.Topic = string(topicBytes)
	}
	
	// Read remaining body
	remaining := make([]byte, buf.Len())
	buf.Read(remaining)
	m.Body = remaining
	
	return nil
}

// MarshalStoreHello serializes a STORE-HELLO message body
func (s *StoreHelloMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write address (length-prefixed string)
	if len(s.Address) > 255 {
		return nil, fmt.Errorf("address too long: %d", len(s.Address))
	}
	buf.WriteByte(byte(len(s.Address)))
	buf.WriteString(s.Address)
	
	// Write headers count and headers
	if len(s.Headers) > 255 {
		return nil, fmt.Errorf("too many headers: %d", len(s.Headers))
	}
	buf.WriteByte(byte(len(s.Headers)))
	for key, value := range s.Headers {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("header key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalStoreHello deserializes a STORE-HELLO message body
func (s *StoreHelloMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read address
	address, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read address: %w", err)
	}
	s.Address = address
	
	// Read headers
	headerCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read header count: %w", err)
	}
	
	s.Headers = make(map[string]string)
	for i := 0; i < int(headerCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header value %d: %w", i, err)
		}
		s.Headers[key] = value
	}
	
	return nil
}

// MarshalConsumerHello serializes a CONSUMER-HELLO message body
func (c *ConsumerHelloMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write address (length-prefixed string)
	if len(c.Address) > 255 {
		return nil, fmt.Errorf("address too long: %d", len(c.Address))
	}
	buf.WriteByte(byte(len(c.Address)))
	buf.WriteString(c.Address)
	
	// Write headers count and headers
	if len(c.Headers) > 255 {
		return nil, fmt.Errorf("too many headers: %d", len(c.Headers))
	}
	buf.WriteByte(byte(len(c.Headers)))
	for key, value := range c.Headers {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("header key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalConsumerHello deserializes a CONSUMER-HELLO message body
func (c *ConsumerHelloMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read address
	address, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read address: %w", err)
	}
	c.Address = address
	
	// Read headers
	headerCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read header count: %w", err)
	}
	
	c.Headers = make(map[string]string)
	for i := 0; i < int(headerCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header value %d: %w", i, err)
		}
		c.Headers[key] = value
	}
	
	return nil
}

// MarshalRecord serializes a RECORD message body
func (r *RecordMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write partition (length-prefixed string)
	if len(r.Partition) > 255 {
		return nil, fmt.Errorf("partition too long: %d", len(r.Partition))
	}
	buf.WriteByte(byte(len(r.Partition)))
	buf.WriteString(r.Partition)
	
	// Write offset (8 bytes, big endian)
	binary.Write(buf, binary.BigEndian, r.Offset)
	
	// Write key length and key
	if len(r.Key) > 65535 {
		return nil, fmt.Errorf("key too long: %d", len(r.Key))
	}
	binary.Write(buf, binary.BigEndian, uint16(len(r.Key)))
	buf.Write(r.Key)
	
	// Write value length and value
	if len(r.Value) > MaxRecordSize {
		return nil, fmt.Errorf("value too long: %d", len(r.Value))
	}
	binary.Write(buf, binary.BigEndian, uint32(len(r.Value)))
	buf.Write(r.Value)
	
	// Write headers count and headers
	if len(r.Headers) > 255 {
		return nil, fmt.Errorf("too many headers: %d", len(r.Headers))
	}
	buf.WriteByte(byte(len(r.Headers)))
	for key, value := range r.Headers {
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("header key or value too long")
		}
		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		buf.WriteByte(byte(len(value)))
		buf.WriteString(value)
	}
	
	return buf.Bytes(), nil
}

// UnmarshalRecord deserializes a RECORD message body
func (r *RecordMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read partition
	partition, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read partition: %w", err)
	}
	r.Partition = partition
	
	// Read offset
	if err := binary.Read(buf, binary.BigEndian, &r.Offset); err != nil {
		return fmt.Errorf("failed to read offset: %w", err)
	}
	
	// Read key
	var keyLen uint16
	if err := binary.Read(buf, binary.BigEndian, &keyLen); err != nil {
		return fmt.Errorf("failed to read key length: %w", err)
	}
	if keyLen > 0 {
		r.Key = make([]byte, keyLen)
		if _, err := buf.Read(r.Key); err != nil {
			return fmt.Errorf("failed to read key: %w", err)
		}
	}
	
	// Read value
	var valueLen uint32
	if err := binary.Read(buf, binary.BigEndian, &valueLen); err != nil {
		return fmt.Errorf("failed to read value length: %w", err)
	}
	if valueLen > 0 {
		r.Value = make([]byte, valueLen)
		if _, err := buf.Read(r.Value); err != nil {
			return fmt.Errorf("failed to read value: %w", err)
		}
	}
	
	// Read headers
	headerCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read header count: %w", err)
	}
	
	r.Headers = make(map[string]string)
	for i := 0; i < int(headerCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header value %d: %w", i, err)
		}
		r.Headers[key] = value
	}
	
	return nil
}

// MarshalAck serializes an ACK message body
func (a *AckMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write partition (length-prefixed string)
	if len(a.Partition) > 255 {
		return nil, fmt.Errorf("partition too long: %d", len(a.Partition))
	}
	buf.WriteByte(byte(len(a.Partition)))
	buf.WriteString(a.Partition)
	
	// Write offset (8 bytes, big endian)
	binary.Write(buf, binary.BigEndian, a.Offset)
	
	return buf.Bytes(), nil
}

// UnmarshalAck deserializes an ACK message body
func (a *AckMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read partition
	partition, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read partition: %w", err)
	}
	a.Partition = partition
	
	// Read offset
	if err := binary.Read(buf, binary.BigEndian, &a.Offset); err != nil {
		return fmt.Errorf("failed to read offset: %w", err)
	}
	
	return nil
}

// MarshalHead serializes a HEAD message body
func (h *HeadMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write partition (length-prefixed string)
	if len(h.Partition) > 255 {
		return nil, fmt.Errorf("partition too long: %d", len(h.Partition))
	}
	buf.WriteByte(byte(len(h.Partition)))
	buf.WriteString(h.Partition)
	
	// Write offset (8 bytes, big endian)
	binary.Write(buf, binary.BigEndian, h.Offset)
	
	return buf.Bytes(), nil
}

// UnmarshalHead deserializes a HEAD message body
func (h *HeadMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read partition
	partition, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read partition: %w", err)
	}
	h.Partition = partition
	
	// Read offset
	if err := binary.Read(buf, binary.BigEndian, &h.Offset); err != nil {
		return fmt.Errorf("failed to read offset: %w", err)
	}
	
	return nil
}

// MarshalFetch serializes a FETCH message body
func (f *FetchMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write partition (length-prefixed string)
	if len(f.Partition) > 255 {
		return nil, fmt.Errorf("partition too long: %d", len(f.Partition))
	}
	buf.WriteByte(byte(len(f.Partition)))
	buf.WriteString(f.Partition)
	
	// Write offset (8 bytes, big endian)
	binary.Write(buf, binary.BigEndian, f.Offset)
	
	// Write count (4 bytes, big endian)
	binary.Write(buf, binary.BigEndian, f.Count)
	
	return buf.Bytes(), nil
}

// UnmarshalFetch deserializes a FETCH message body
func (f *FetchMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read partition
	partition, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read partition: %w", err)
	}
	f.Partition = partition
	
	// Read offset
	if err := binary.Read(buf, binary.BigEndian, &f.Offset); err != nil {
		return fmt.Errorf("failed to read offset: %w", err)
	}
	
	// Read count
	if err := binary.Read(buf, binary.BigEndian, &f.Count); err != nil {
		return fmt.Errorf("failed to read count: %w", err)
	}
	
	return nil
}

// MarshalDirectRecord serializes a DIRECT-RECORD message body
func (d *DirectRecordMessage) Marshal() ([]byte, error) {
	// Same format as RECORD message
	r := &RecordMessage{
		Partition: d.Partition,
		Offset:    d.Offset,
		Key:       d.Key,
		Value:     d.Value,
		Headers:   d.Headers,
	}
	return r.Marshal()
}

// UnmarshalDirectRecord deserializes a DIRECT-RECORD message body
func (d *DirectRecordMessage) Unmarshal(data []byte) error {
	// Same format as RECORD message
	r := &RecordMessage{}
	if err := r.Unmarshal(data); err != nil {
		return err
	}
	
	d.Partition = r.Partition
	d.Offset = r.Offset
	d.Key = r.Key
	d.Value = r.Value
	d.Headers = r.Headers
	
	return nil
}

// MarshalGetHeads serializes a GET-HEADS message body
func (g *GetHeadsMessage) Marshal() ([]byte, error) {
	// Empty body
	return []byte{}, nil
}

// UnmarshalGetHeads deserializes a GET-HEADS message body
func (g *GetHeadsMessage) Unmarshal(data []byte) error {
	// Empty body - nothing to unmarshal
	return nil
}

// MarshalDirectHead serializes a DIRECT-HEAD message body
func (d *DirectHeadMessage) Marshal() ([]byte, error) {
	// Same format as HEAD message
	h := &HeadMessage{
		Partition: d.Partition,
		Offset:    d.Offset,
	}
	return h.Marshal()
}

// UnmarshalDirectHead deserializes a DIRECT-HEAD message body
func (d *DirectHeadMessage) Unmarshal(data []byte) error {
	// Same format as HEAD message
	h := &HeadMessage{}
	if err := h.Unmarshal(data); err != nil {
		return err
	}
	
	d.Partition = h.Partition
	d.Offset = h.Offset
	
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

// CreateRecordMessage creates a RECORD message from a Record
func CreateRecordMessage(topic string, record *Record) (*Message, error) {
	recordMsg := &RecordMessage{
		Partition: record.Partition,
		Offset:    record.Offset,
		Key:       record.Key,
		Value:     record.Value,
		Headers:   record.Headers,
	}
	
	body, err := recordMsg.Marshal()
	if err != nil {
		return nil, err
	}
	
	return &Message{
		Signature: [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:   ProtocolVersion,
		Type:      MessageTypeRecord,
		Topic:     topic,
		Body:      body,
	}, nil
}

// CreateAckMessage creates an ACK message
func CreateAckMessage(topic, partition string, offset uint64) (*Message, error) {
	ackMsg := &AckMessage{
		Partition: partition,
		Offset:    offset,
	}
	
	body, err := ackMsg.Marshal()
	if err != nil {
		return nil, err
	}
	
	return &Message{
		Signature: [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:   ProtocolVersion,
		Type:      MessageTypeAck,
		Topic:     topic,
		Body:      body,
	}, nil
}

// CreateHeadMessage creates a HEAD message
func CreateHeadMessage(topic, partition string, offset uint64) (*Message, error) {
	headMsg := &HeadMessage{
		Partition: partition,
		Offset:    offset,
	}
	
	body, err := headMsg.Marshal()
	if err != nil {
		return nil, err
	}
	
	return &Message{
		Signature: [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:   ProtocolVersion,
		Type:      MessageTypeHead,
		Topic:     topic,
		Body:      body,
	}, nil
}

// CreateFetchMessage creates a FETCH message
func CreateFetchMessage(topic, partition string, offset uint64, count uint32) (*Message, error) {
	fetchMsg := &FetchMessage{
		Partition: partition,
		Offset:    offset,
		Count:     count,
	}
	
	body, err := fetchMsg.Marshal()
	if err != nil {
		return nil, err
	}
	
	return &Message{
		Signature: [2]byte{ProtocolSignature1, ProtocolSignature2},
		Version:   ProtocolVersion,
		Type:      MessageTypeFetch,
		Topic:     topic,
		Body:      body,
	}, nil
}