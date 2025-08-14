// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package z85 provides ZeroMQ Base-85 Encoding as specified by:
// https://rfc.zeromq.org/spec/32/Z85/
package z85

import (
	"errors"
	"fmt"
)

// EncodedLen returns the Z85 encoded length for n source bytes
func EncodedLen(n int) int { 
	return n * 5 / 4 
}

// DecodedLen returns the maximum length in bytes of the decoded data
// corresponding to n bytes of Z85-encoded data.
func DecodedLen(n int) int { 
	return n * 4 / 5 
}

var (
	ErrInvalidLength = errors.New("z85: invalid input length")
	ErrInvalidChar   = errors.New("z85: invalid character")
)

// Z85 alphabet as defined in RFC 32
const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.-:+=^!/*?&<>()[]{}@%$#"

// Decoder table for fast character lookup
var decoder = [256]byte{
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 68,  255, 84,  83,  82,  72,  255, 75,  76,  70,  65,  255, 63,  62,  69,
	0,   1,   2,   3,   4,   5,   6,   7,   8,   9,   64,  255, 73,  66,  74,  71,
	81,  36,  37,  38,  39,  40,  41,  42,  43,  44,  45,  46,  47,  48,  49,  50,
	51,  52,  53,  54,  55,  56,  57,  58,  59,  60,  61,  77,  255, 78,  67,  255,
	255, 10,  11,  12,  13,  14,  15,  16,  17,  18,  19,  20,  21,  22,  23,  24,
	25,  26,  27,  28,  29,  30,  31,  32,  33,  34,  35,  79,  255, 80,  255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
}

// Encode encodes src using Z85 encoding,
// writing EncodedLen(len(src)) bytes to dst.
//
// The encoding handles 4-byte groups, so len(src) must be divisible by 4.
func Encode(dst, src []byte) error {
	if len(src)%4 != 0 {
		return fmt.Errorf("%w: source length %d not divisible by 4", ErrInvalidLength, len(src))
	}
	
	if len(dst) < EncodedLen(len(src)) {
		return fmt.Errorf("z85: destination buffer too small: need %d, got %d", EncodedLen(len(src)), len(dst))
	}

	// Process 4-byte chunks into 5-character groups
	di := 0
	for si := 0; si < len(src); si += 4 {
		// Convert 4 bytes to 32-bit value (big endian)
		value := uint64(src[si])<<24 | uint64(src[si+1])<<16 | uint64(src[si+2])<<8 | uint64(src[si+3])
		
		// Extract 5 characters (most significant first)
		dst[di+4] = alphabet[value%85]
		value /= 85
		dst[di+3] = alphabet[value%85]
		value /= 85
		dst[di+2] = alphabet[value%85]
		value /= 85
		dst[di+1] = alphabet[value%85]
		value /= 85
		dst[di] = alphabet[value%85]
		
		di += 5
	}
	
	return nil
}

// EncodeToString returns the Z85 encoding of src.
func EncodeToString(src []byte) (string, error) {
	if len(src)%4 != 0 {
		return "", fmt.Errorf("%w: source length %d not divisible by 4", ErrInvalidLength, len(src))
	}
	
	dst := make([]byte, EncodedLen(len(src)))
	err := Encode(dst, src)
	if err != nil {
		return "", err
	}
	return string(dst), nil
}

// Decode decodes src using Z85 encoding,
// writing DecodedLen(len(src)) bytes to dst.
//
// The decoding handles 5-character groups, so len(src) must be divisible by 5.
func Decode(dst, src []byte) (int, error) {
	if len(src)%5 != 0 {
		return 0, fmt.Errorf("%w: source length %d not divisible by 5", ErrInvalidLength, len(src))
	}
	
	if len(dst) < DecodedLen(len(src)) {
		return 0, fmt.Errorf("z85: destination buffer too small: need %d, got %d", DecodedLen(len(src)), len(dst))
	}

	// Process 5-character chunks into 4-byte groups
	di := 0
	for si := 0; si < len(src); si += 5 {
		// Convert 5 characters to 32-bit value
		var value uint64
		
		for i := 0; i < 5; i++ {
			char := src[si+i]
			if char >= 128 {
				return 0, fmt.Errorf("%w: character 0x%02x at position %d", ErrInvalidChar, char, si+i)
			}
			
			charValue := decoder[char]
			if charValue == 255 {
				return 0, fmt.Errorf("%w: character %q at position %d", ErrInvalidChar, char, si+i)
			}
			
			value = value*85 + uint64(charValue)
		}
		
		// Check for overflow (value should fit in 32 bits)
		if value > 0xFFFFFFFF {
			return 0, fmt.Errorf("z85: decoded value overflow at position %d", si)
		}
		
		// Extract 4 bytes (big endian)
		dst[di]   = byte(value >> 24)
		dst[di+1] = byte(value >> 16)
		dst[di+2] = byte(value >> 8)
		dst[di+3] = byte(value)
		
		di += 4
	}
	
	return di, nil
}

// DecodeString returns the bytes represented by the Z85 string s.
func DecodeString(s string) ([]byte, error) {
	if len(s)%5 != 0 {
		return nil, fmt.Errorf("%w: source length %d not divisible by 5", ErrInvalidLength, len(s))
	}
	
	dst := make([]byte, DecodedLen(len(s)))
	n, err := Decode(dst, []byte(s))
	if err != nil {
		return nil, err
	}
	return dst[:n], nil
}

// ValidateString checks if s is a valid Z85 encoded string.
func ValidateString(s string) error {
	if len(s)%5 != 0 {
		return fmt.Errorf("%w: length %d not divisible by 5", ErrInvalidLength, len(s))
	}
	
	for i, char := range []byte(s) {
		if char >= 128 || decoder[char] == 255 {
			return fmt.Errorf("%w: character %q at position %d", ErrInvalidChar, char, i)
		}
	}
	
	return nil
}