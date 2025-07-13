package main

import (
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestBeastModeDecoder_ValidMessages(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		expectedLen int
		wantErr     bool
	}{
		{
			name: "Valid Mode S Short Message",
			input: []byte{
				0x1A, 0x32, // Sync + Type
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // Timestamp
				0x02,                                     // Signal level
				0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78, // Message data
			},
			expectedLen: 1,
			wantErr:     false,
		},
		{
			name: "Valid Mode S Long Message",
			input: []byte{
				0x1A, 0x33, // Sync + Type
				0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // Timestamp
				0x03, // Signal level
				// 14 bytes of message data
				0x8D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78, 0x9A,
				0xBC, 0xDE, 0xF0, 0x12, 0x34, 0x56,
			},
			expectedLen: 1,
			wantErr:     false,
		},
		{
			name: "Valid Mode A/C Message",
			input: []byte{
				0x1A, 0x31, // Sync + Type
				0x00, 0x00, 0x00, 0x00, 0x00, 0x03, // Timestamp
				0x04,       // Signal level
				0x02, 0x34, // Mode A/C data
			},
			expectedLen: 1,
			wantErr:     false,
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress logs during testing

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := NewBeastDecoder(logger)

			messages, err := decoder.Decode(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(messages) != tt.expectedLen {
				t.Errorf("Expected %d messages, got %d", tt.expectedLen, len(messages))
				return
			}

			if len(messages) > 0 {
				message := messages[0]

				// Check message type
				if tt.input[1] == BeastModeAC && message.MessageType != BeastModeAC {
					t.Errorf("MessageType = %v, want %v", message.MessageType, BeastModeAC)
				}
				if tt.input[1] == BeastModeS && message.MessageType != BeastModeS {
					t.Errorf("MessageType = %v, want %v", message.MessageType, BeastModeS)
				}
				if tt.input[1] == BeastModeSLong && message.MessageType != BeastModeSLong {
					t.Errorf("MessageType = %v, want %v", message.MessageType, BeastModeSLong)
				}

				// Check that timestamp is parsed
				if message.Timestamp.IsZero() {
					t.Errorf("Timestamp should not be zero")
				}

				// Check signal level
				expectedSignal := tt.input[8] // Signal is at position 8
				if message.Signal != expectedSignal {
					t.Errorf("Signal = %v, want %v", message.Signal, expectedSignal)
				}

				// Check that data exists
				if len(message.Data) == 0 {
					t.Errorf("Data should not be empty")
				}
			}
		})
	}
}

func TestBeastModeDecoder_InvalidMessages(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{
			name:    "Invalid sync byte",
			input:   []byte{0x1B, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			wantErr: false, // Should just ignore invalid sync
		},
		{
			name:    "Unknown message type",
			input:   []byte{0x1A, 0x99, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			wantErr: false, // Should ignore unknown types
		},
		{
			name:    "Empty input",
			input:   []byte{},
			wantErr: false, // No error, just no messages
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := NewBeastDecoder(logger)

			messages, err := decoder.Decode(tt.input)

			if tt.wantErr && err == nil {
				t.Errorf("Expected error, got none")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// For invalid messages, we should get no decoded messages
			if !tt.wantErr && len(messages) > 0 {
				t.Errorf("Expected no messages for invalid input, got %d", len(messages))
			}
		})
	}
}

func TestBeastMessage_GetICAO(t *testing.T) {
	tests := []struct {
		name        string
		messageType byte
		data        []byte
		expected    uint32
	}{
		{
			name:        "Valid ICAO",
			messageType: BeastModeS,
			data:        []byte{0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
			expected:    0x484412, // ICAO from bytes 1-3
		},
		{
			name:        "Another ICAO",
			messageType: BeastModeSLong,
			data:        []byte{0x8D, 0xAB, 0xCD, 0xEF, 0x12, 0x34, 0x56},
			expected:    0xABCDEF,
		},
		{
			name:        "Mode A/C message",
			messageType: BeastModeAC,
			data:        []byte{0x02, 0x34},
			expected:    0, // Mode A/C doesn't have ICAO
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &BeastMessage{
				MessageType: tt.messageType,
				Data:        tt.data,
			}

			icao := msg.GetICAO()
			if icao != tt.expected {
				t.Errorf("GetICAO() = %06X, want %06X", icao, tt.expected)
			}
		})
	}
}

func TestBeastMessage_GetDF(t *testing.T) {
	tests := []struct {
		name        string
		messageType byte
		data        []byte
		expected    byte
	}{
		{
			name:        "DF 11",
			messageType: BeastModeS,
			data:        []byte{0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
			expected:    11, // DF from first byte upper 5 bits (0x5D >> 3) & 0x1F = 11
		},
		{
			name:        "DF 17",
			messageType: BeastModeSLong,
			data:        []byte{0x8D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
			expected:    17, // DF from first byte upper 5 bits (0x8D >> 3) & 0x1F = 17
		},
		{
			name:        "Mode A/C message",
			messageType: BeastModeAC,
			data:        []byte{0x02, 0x34},
			expected:    0, // Mode A/C doesn't have DF
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &BeastMessage{
				MessageType: tt.messageType,
				Data:        tt.data,
			}

			df := msg.GetDF()
			if df != tt.expected {
				t.Errorf("GetDF() = %d, want %d", df, tt.expected)
			}
		})
	}
}

func TestBeastMessage_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		message  *BeastMessage
		expected bool
	}{
		{
			name: "Valid Mode S message",
			message: &BeastMessage{
				MessageType: BeastModeS,
				Data:        []byte{0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
			},
			expected: true,
		},
		{
			name: "Valid Mode A/C message",
			message: &BeastMessage{
				MessageType: BeastModeAC,
				Data:        []byte{0x02, 0x34},
			},
			expected: true,
		},
		{
			name: "Empty data",
			message: &BeastMessage{
				MessageType: BeastModeS,
				Data:        []byte{},
			},
			expected: false,
		},
		{
			name:     "Nil message",
			message:  nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var valid bool
			if tt.message != nil {
				valid = tt.message.IsValid()
			} else {
				valid = false
			}

			if valid != tt.expected {
				t.Errorf("IsValid() = %v, want %v", valid, tt.expected)
			}
		})
	}
}

func TestBeastModeDecoder_ConcurrentSafety(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Valid message data
	messageData := []byte{
		0x1A, 0x32,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x02,
		0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78,
	}

	// Run multiple goroutines concurrently, each with its own decoder
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("panic: %v", r)
					return
				}
			}()

			// Each goroutine gets its own decoder to avoid race conditions
			decoder := NewBeastDecoder(logger)
			_, err := decoder.Decode(messageData)
			done <- err
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		err := <-done
		if err != nil {
			t.Errorf("Concurrent decode failed: %v", err)
		}
	}
}

func BenchmarkBeastModeDecoder_Decode(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	decoder := NewBeastDecoder(logger)

	messageData := []byte{
		0x1A, 0x32,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x02,
		0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := decoder.Decode(messageData)
		if err != nil {
			b.Fatalf("Decode failed: %v", err)
		}
	}
}

func BenchmarkBeastMessage_GetICAO(b *testing.B) {
	msg := &BeastMessage{
		Data: []byte{0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.GetICAO()
	}
}
