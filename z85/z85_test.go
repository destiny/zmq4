// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package z85

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
)

// Test vectors from RFC 32 specification
var testVectors = []struct {
	binary []byte
	z85    string
}{
	{
		binary: []byte{0x86, 0x4F, 0xD2, 0x6F, 0xB5, 0x59, 0xF7, 0x5B},
		z85:    "HelloWorld",
	},
	{
		binary: []byte{0x8E, 0x0B, 0xDD, 0x69, 0x76, 0x28, 0xB9, 0x1D, 0x8F, 0x24, 0x55, 0x87, 0xEE, 0x95, 0xC5, 0xB0, 0x4D, 0x48, 0x96, 0x3F, 0x79, 0x25, 0x98, 0x77, 0xB4, 0x9C, 0xD9, 0x06, 0x3A, 0xEA, 0xD3, 0xB7},
		z85:    "JTKVSB%%)wK0E.X)V>+}o?pNmC{O&4W4b!Ni{Lh6",
	},
}

func TestEncode(t *testing.T) {
	for i, tv := range testVectors {
		dst := make([]byte, EncodedLen(len(tv.binary)))
		err := Encode(dst, tv.binary)
		if err != nil {
			t.Errorf("Test %d: Encode failed: %v", i, err)
			continue
		}
		
		if string(dst) != tv.z85 {
			t.Errorf("Test %d: Encode mismatch\nExpected: %q\nGot:      %q", i, tv.z85, string(dst))
		}
	}
}

func TestEncodeToString(t *testing.T) {
	for i, tv := range testVectors {
		result, err := EncodeToString(tv.binary)
		if err != nil {
			t.Errorf("Test %d: EncodeToString failed: %v", i, err)
			continue
		}
		
		if result != tv.z85 {
			t.Errorf("Test %d: EncodeToString mismatch\nExpected: %q\nGot:      %q", i, tv.z85, result)
		}
	}
}

func TestDecode(t *testing.T) {
	for i, tv := range testVectors {
		dst := make([]byte, DecodedLen(len(tv.z85)))
		n, err := Decode(dst, []byte(tv.z85))
		if err != nil {
			t.Errorf("Test %d: Decode failed: %v", i, err)
			continue
		}
		
		if n != len(tv.binary) {
			t.Errorf("Test %d: Decode length mismatch: expected %d, got %d", i, len(tv.binary), n)
			continue
		}
		
		if !bytes.Equal(dst[:n], tv.binary) {
			t.Errorf("Test %d: Decode mismatch\nExpected: %x\nGot:      %x", i, tv.binary, dst[:n])
		}
	}
}

func TestDecodeString(t *testing.T) {
	for i, tv := range testVectors {
		result, err := DecodeString(tv.z85)
		if err != nil {
			t.Errorf("Test %d: DecodeString failed: %v", i, err)
			continue
		}
		
		if !bytes.Equal(result, tv.binary) {
			t.Errorf("Test %d: DecodeString mismatch\nExpected: %x\nGot:      %x", i, tv.binary, result)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	// Test with various data sizes (all divisible by 4)
	sizes := []int{4, 8, 16, 32, 64, 128, 256}
	
	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			// Generate random data
			original := make([]byte, size)
			_, err := rand.Read(original)
			if err != nil {
				t.Fatalf("Failed to generate random data: %v", err)
			}
			
			// Encode
			encoded, err := EncodeToString(original)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}
			
			// Decode
			decoded, err := DecodeString(encoded)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
			
			// Verify
			if !bytes.Equal(original, decoded) {
				t.Errorf("Round trip failed\nOriginal: %x\nDecoded:  %x", original, decoded)
			}
		})
	}
}

func TestCurveKeySize(t *testing.T) {
	// Test 32-byte CURVE keys specifically
	curveKey := make([]byte, 32)
	_, err := rand.Read(curveKey)
	if err != nil {
		t.Fatalf("Failed to generate curve key: %v", err)
	}
	
	// Encode
	encoded, err := EncodeToString(curveKey)
	if err != nil {
		t.Fatalf("Failed to encode CURVE key: %v", err)
	}
	
	// Should be exactly 40 characters
	if len(encoded) != 40 {
		t.Errorf("CURVE key Z85 encoding should be 40 characters, got %d", len(encoded))
	}
	
	// Decode back
	decoded, err := DecodeString(encoded)
	if err != nil {
		t.Fatalf("Failed to decode CURVE key: %v", err)
	}
	
	// Verify
	if !bytes.Equal(curveKey, decoded) {
		t.Errorf("CURVE key round trip failed")
	}
}

func TestValidateString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid z85", "HelloWorld", false},
		{"valid long", "JTKVSB%%)wK0E.X)V>+}o?pNmC{O&4W4b!Ni{Lh6", false},
		{"invalid length", "Hel", true}, // 3 chars, not divisible by 5
		{"invalid char", "Hello~orld", true}, // ~ is not in Z85 alphabet
		{"empty string", "", false},
		{"single invalid", "~", true},
		{"mixed valid/invalid", "Hell~World", true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestErrorCases(t *testing.T) {
	t.Run("Encode invalid length", func(t *testing.T) {
		// Length not divisible by 4
		src := []byte{1, 2, 3} // 3 bytes
		dst := make([]byte, 5)
		err := Encode(dst, src)
		if err == nil {
			t.Error("Expected error for invalid source length")
		}
		if !strings.Contains(err.Error(), "not divisible by 4") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("Encode buffer too small", func(t *testing.T) {
		src := []byte{1, 2, 3, 4} // 4 bytes
		dst := make([]byte, 4)    // Need 5 bytes
		err := Encode(dst, src)
		if err == nil {
			t.Error("Expected error for small destination buffer")
		}
		if !strings.Contains(err.Error(), "destination buffer too small") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("Decode invalid length", func(t *testing.T) {
		// Length not divisible by 5
		src := []byte("Hell") // 4 characters
		dst := make([]byte, 4)
		_, err := Decode(dst, src)
		if err == nil {
			t.Error("Expected error for invalid source length")
		}
		if !strings.Contains(err.Error(), "not divisible by 5") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("Decode invalid character", func(t *testing.T) {
		// Invalid character
		src := []byte("Hell~") // ~ is not in Z85 alphabet
		dst := make([]byte, 4)
		_, err := Decode(dst, src)
		if err == nil {
			t.Error("Expected error for invalid character")
		}
		if !strings.Contains(err.Error(), "invalid character") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
	
	t.Run("Decode high bit character", func(t *testing.T) {
		// Character with high bit set
		src := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		dst := make([]byte, 4)
		_, err := Decode(dst, src)
		if err == nil {
			t.Error("Expected error for high bit character")
		}
	})
}

func TestAlphabetCompleteness(t *testing.T) {
	// Verify all 85 characters are unique and in correct order
	if len(alphabet) != 85 {
		t.Errorf("Alphabet should have 85 characters, got %d", len(alphabet))
	}
	
	// Check uniqueness
	seen := make(map[byte]bool)
	for i, char := range []byte(alphabet) {
		if seen[char] {
			t.Errorf("Duplicate character %q at position %d", char, i)
		}
		seen[char] = true
	}
	
	// Verify decoder table matches alphabet
	for i, char := range []byte(alphabet) {
		if decoder[char] != byte(i) {
			t.Errorf("Decoder mismatch for character %q: expected %d, got %d", char, i, decoder[char])
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	data := make([]byte, 32) // CURVE key size
	rand.Read(data)
	dst := make([]byte, EncodedLen(len(data)))
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := Encode(dst, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	data := make([]byte, 32) // CURVE key size
	rand.Read(data)
	encoded, _ := EncodeToString(data)
	dst := make([]byte, DecodedLen(len(encoded)))
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Decode(dst, []byte(encoded))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeToString(b *testing.B) {
	data := make([]byte, 32) // CURVE key size
	rand.Read(data)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := EncodeToString(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeString(b *testing.B) {
	data := make([]byte, 32) // CURVE key size
	rand.Read(data)
	encoded, _ := EncodeToString(data)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := DecodeString(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}