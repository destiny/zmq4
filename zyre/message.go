// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zyre

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Marshal serializes a ZRE message to wire format
func (m *Message) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write protocol signature and version
	buf.WriteByte(ProtocolSignature1)
	buf.WriteByte(ProtocolSignature2)
	buf.WriteByte(m.Version)
	
	// Write message type and sequence
	buf.WriteByte(m.Type)
	binary.Write(buf, binary.BigEndian, m.Sequence)
	
	// Write sender UUID
	buf.Write(m.SenderUUID[:])
	
	// Write message body
	buf.Write(m.Body)
	
	return buf.Bytes(), nil
}

// Unmarshal deserializes a ZRE message from wire format
func (m *Message) Unmarshal(data []byte) error {
	if len(data) < 21 { // Minimum message size: 2 sig + 1 ver + 1 type + 2 seq + 16 uuid
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
	
	// Read message type and sequence
	msgType, _ := buf.ReadByte()
	m.Type = msgType
	binary.Read(buf, binary.BigEndian, &m.Sequence)
	
	// Read sender UUID
	buf.Read(m.SenderUUID[:])
	
	// Read remaining body
	remaining := make([]byte, buf.Len())
	buf.Read(remaining)
	m.Body = remaining
	
	return nil
}

// MarshalHello serializes a HELLO message body
func (h *HelloMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write version and sequence
	buf.WriteByte(h.Version)
	binary.Write(buf, binary.BigEndian, h.Sequence)
	
	// Write endpoint (length-prefixed string)
	if len(h.Endpoint) > 255 {
		return nil, fmt.Errorf("endpoint too long: %d", len(h.Endpoint))
	}
	buf.WriteByte(byte(len(h.Endpoint)))
	buf.WriteString(h.Endpoint)
	
	// Write groups count and groups
	if len(h.Groups) > 255 {
		return nil, fmt.Errorf("too many groups: %d", len(h.Groups))
	}
	buf.WriteByte(byte(len(h.Groups)))
	for _, group := range h.Groups {
		if len(group) > 255 {
			return nil, fmt.Errorf("group name too long: %d", len(group))
		}
		buf.WriteByte(byte(len(group)))
		buf.WriteString(group)
	}
	
	// Write status
	buf.WriteByte(h.Status)
	
	// Write name (length-prefixed string)
	if len(h.Name) > 255 {
		return nil, fmt.Errorf("name too long: %d", len(h.Name))
	}
	buf.WriteByte(byte(len(h.Name)))
	buf.WriteString(h.Name)
	
	// Write headers count and headers
	if len(h.Headers) > 255 {
		return nil, fmt.Errorf("too many headers: %d", len(h.Headers))
	}
	buf.WriteByte(byte(len(h.Headers)))
	for key, value := range h.Headers {
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

// UnmarshalHello deserializes a HELLO message body
func (h *HelloMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read version and sequence
	version, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read version: %w", err)
	}
	h.Version = version
	
	if err := binary.Read(buf, binary.BigEndian, &h.Sequence); err != nil {
		return fmt.Errorf("failed to read sequence: %w", err)
	}
	
	// Read endpoint
	if endpoint, err := readString(buf); err != nil {
		return fmt.Errorf("failed to read endpoint: %w", err)
	} else {
		h.Endpoint = endpoint
	}
	
	// Read groups
	groupCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read group count: %w", err)
	}
	
	h.Groups = make([]string, groupCount)
	for i := 0; i < int(groupCount); i++ {
		if group, err := readString(buf); err != nil {
			return fmt.Errorf("failed to read group %d: %w", i, err)
		} else {
			h.Groups[i] = group
		}
	}
	
	// Read status
	status, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read status: %w", err)
	}
	h.Status = status
	
	// Read name
	if name, err := readString(buf); err != nil {
		return fmt.Errorf("failed to read name: %w", err)
	} else {
		h.Name = name
	}
	
	// Read headers
	headerCount, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read header count: %w", err)
	}
	
	h.Headers = make(map[string]string)
	for i := 0; i < int(headerCount); i++ {
		key, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header key %d: %w", i, err)
		}
		value, err := readString(buf)
		if err != nil {
			return fmt.Errorf("failed to read header value %d: %w", i, err)
		}
		h.Headers[key] = value
	}
	
	return nil
}

// MarshalWhisper serializes a WHISPER message body
func (w *WhisperMessage) Marshal() ([]byte, error) {
	return marshalMultipart(w.Content)
}

// UnmarshalWhisper deserializes a WHISPER message body
func (w *WhisperMessage) Unmarshal(data []byte) error {
	content, err := unmarshalMultipart(data)
	if err != nil {
		return err
	}
	w.Content = content
	return nil
}

// MarshalShout serializes a SHOUT message body
func (s *ShoutMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write group name (length-prefixed)
	if len(s.Group) > 255 {
		return nil, fmt.Errorf("group name too long: %d", len(s.Group))
	}
	buf.WriteByte(byte(len(s.Group)))
	buf.WriteString(s.Group)
	
	// Write message content
	content, err := marshalMultipart(s.Content)
	if err != nil {
		return nil, err
	}
	buf.Write(content)
	
	return buf.Bytes(), nil
}

// UnmarshalShout deserializes a SHOUT message body
func (s *ShoutMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read group name
	group, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read group: %w", err)
	}
	s.Group = group
	
	// Read message content
	remaining := make([]byte, buf.Len())
	buf.Read(remaining)
	
	content, err := unmarshalMultipart(remaining)
	if err != nil {
		return err
	}
	s.Content = content
	
	return nil
}

// MarshalJoin serializes a JOIN message body
func (j *JoinMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write group name (length-prefixed)
	if len(j.Group) > 255 {
		return nil, fmt.Errorf("group name too long: %d", len(j.Group))
	}
	buf.WriteByte(byte(len(j.Group)))
	buf.WriteString(j.Group)
	
	// Write status
	buf.WriteByte(j.Status)
	
	return buf.Bytes(), nil
}

// UnmarshalJoin deserializes a JOIN message body
func (j *JoinMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read group name
	group, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read group: %w", err)
	}
	j.Group = group
	
	// Read status
	status, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read status: %w", err)
	}
	j.Status = status
	
	return nil
}

// MarshalLeave serializes a LEAVE message body
func (l *LeaveMessage) Marshal() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write group name (length-prefixed)
	if len(l.Group) > 255 {
		return nil, fmt.Errorf("group name too long: %d", len(l.Group))
	}
	buf.WriteByte(byte(len(l.Group)))
	buf.WriteString(l.Group)
	
	// Write status
	buf.WriteByte(l.Status)
	
	return buf.Bytes(), nil
}

// UnmarshalLeave deserializes a LEAVE message body
func (l *LeaveMessage) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)
	
	// Read group name
	group, err := readString(buf)
	if err != nil {
		return fmt.Errorf("failed to read group: %w", err)
	}
	l.Group = group
	
	// Read status
	status, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read status: %w", err)
	}
	l.Status = status
	
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

// marshalMultipart serializes a multipart message
func marshalMultipart(frames [][]byte) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// Write frame count
	if len(frames) > 255 {
		return nil, fmt.Errorf("too many frames: %d", len(frames))
	}
	buf.WriteByte(byte(len(frames)))
	
	// Write each frame with length prefix
	for _, frame := range frames {
		if len(frame) > 65535 {
			return nil, fmt.Errorf("frame too large: %d bytes", len(frame))
		}
		binary.Write(buf, binary.BigEndian, uint16(len(frame)))
		buf.Write(frame)
	}
	
	return buf.Bytes(), nil
}

// unmarshalMultipart deserializes a multipart message
func unmarshalMultipart(data []byte) ([][]byte, error) {
	buf := bytes.NewReader(data)
	
	// Read frame count
	frameCount, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("failed to read frame count: %w", err)
	}
	
	frames := make([][]byte, frameCount)
	for i := 0; i < int(frameCount); i++ {
		// Read frame length
		var frameLength uint16
		if err := binary.Read(buf, binary.BigEndian, &frameLength); err != nil {
			return nil, fmt.Errorf("failed to read frame %d length: %w", i, err)
		}
		
		// Read frame data
		frame := make([]byte, frameLength)
		if _, err := buf.Read(frame); err != nil {
			return nil, fmt.Errorf("failed to read frame %d data: %w", i, err)
		}
		
		frames[i] = frame
	}
	
	return frames, nil
}