package main

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Beast mode message types
const (
	BeastSyncByte   = 0x1A // Beast mode sync byte
	BeastModeAC     = 0x31 // Mode A/C
	BeastModeS      = 0x32 // Mode S Short (56 bits)
	BeastModeSLong  = 0x33 // Mode S Long (112 bits)
	BeastModeStatus = 0x34 // Status
)

// BeastMessage represents a decoded Beast mode message
type BeastMessage struct {
	MessageType byte
	Timestamp   time.Time
	Signal      byte
	Data        []byte
	Raw         []byte
}

// BeastDecoder decodes Beast mode messages
type BeastDecoder struct {
	logger *logrus.Logger
	buffer []byte
}

// NewBeastDecoder creates a new Beast decoder
func NewBeastDecoder(logger *logrus.Logger) *BeastDecoder {
	return &BeastDecoder{
		logger: logger,
		buffer: make([]byte, 0, 4096),
	}
}

// Decode decodes Beast mode messages from raw data
func (d *BeastDecoder) Decode(data []byte) ([]*BeastMessage, error) {
	d.buffer = append(d.buffer, data...)

	var messages []*BeastMessage

	// Debug: Log buffer state occasionally
	if len(d.buffer) > 0 && len(d.buffer)%1024 == 0 {
		d.logger.WithFields(logrus.Fields{
			"buffer_size": len(d.buffer),
			"data_length": len(data),
		}).Debug("Beast decoder buffer status")
	}

	for {
		// Look for sync byte
		syncIndex := -1
		for i, b := range d.buffer {
			if b == BeastSyncByte {
				syncIndex = i
				break
			}
		}

		if syncIndex == -1 {
			// No sync byte found, clear buffer
			if len(d.buffer) > 1024 {
				d.logger.WithFields(logrus.Fields{
					"buffer_size": len(d.buffer),
				}).Debug("No sync byte found, clearing buffer")
			}
			d.buffer = d.buffer[:0]
			break
		}

		// Remove data before sync byte
		if syncIndex > 0 {
			d.buffer = d.buffer[syncIndex:]
		}

		// Check if we have enough data for a complete message
		if len(d.buffer) < 2 {
			break
		}

		messageType := d.buffer[1]
		messageLen := d.getMessageLength(messageType)

		if messageLen == 0 {
			// Unknown message type, skip this sync byte
			d.logger.WithFields(logrus.Fields{
				"message_type": fmt.Sprintf("0x%02x", messageType),
			}).Debug("Unknown message type, skipping")
			d.buffer = d.buffer[1:]
			continue
		}

		// Check if we have a complete message
		if len(d.buffer) < messageLen {
			break
		}

		// Extract message
		messageData := make([]byte, messageLen)
		copy(messageData, d.buffer[:messageLen])

		// Debug: Log message detection
		d.logger.WithFields(logrus.Fields{
			"message_type": fmt.Sprintf("0x%02x", messageType),
			"message_len":  messageLen,
			"buffer_size":  len(d.buffer),
		}).Debug("Found potential Beast message")

		// Decode message
		msg, err := d.decodeMessage(messageData)
		if err != nil {
			d.logger.WithError(err).Debug("Failed to decode beast message")
			d.buffer = d.buffer[1:]
			continue
		}

		// Debug: Log successful message decode
		d.logger.WithFields(logrus.Fields{
			"message_type": fmt.Sprintf("0x%02x", msg.MessageType),
			"signal":       msg.Signal,
			"data_length":  len(msg.Data),
		}).Debug("Successfully decoded Beast message")

		messages = append(messages, msg)

		// Remove processed message from buffer
		d.buffer = d.buffer[messageLen:]
	}

	// Keep buffer size reasonable
	if len(d.buffer) > 2048 {
		d.buffer = d.buffer[:0]
	}

	return messages, nil
}

// getMessageLength returns the expected length of a Beast message based on type
func (d *BeastDecoder) getMessageLength(messageType byte) int {
	switch messageType {
	case BeastModeAC:
		return 11 // 1 sync + 1 type + 6 timestamp + 1 signal + 2 data
	case BeastModeS:
		return 16 // 1 sync + 1 type + 6 timestamp + 1 signal + 7 data
	case BeastModeSLong:
		return 23 // 1 sync + 1 type + 6 timestamp + 1 signal + 14 data
	case BeastModeStatus:
		return 11 // 1 sync + 1 type + 6 timestamp + 1 signal + 2 data
	default:
		return 0
	}
}

// decodeMessage decodes a complete Beast message
func (d *BeastDecoder) decodeMessage(data []byte) (*BeastMessage, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}

	if data[0] != BeastSyncByte {
		return nil, fmt.Errorf("invalid sync byte: 0x%02x", data[0])
	}

	messageType := data[1]

	// Extract timestamp (6 bytes, 48-bit counter at 12MHz)
	timestamp := uint64(0)
	for i := 0; i < 6; i++ {
		timestamp = (timestamp << 8) | uint64(data[2+i])
	}

	// Convert 12MHz counter to time
	// This is a simplified conversion - in reality you'd need to sync with system time
	timestampTime := time.Now().Add(-time.Duration(timestamp) * time.Nanosecond / 12)

	// Extract signal strength
	signal := data[8]

	// Extract message data
	expectedLen := d.getMessageLength(messageType)
	if len(data) < expectedLen {
		return nil, fmt.Errorf("incomplete message: got %d bytes, expected %d", len(data), expectedLen)
	}

	messageData := make([]byte, expectedLen-9) // Subtract header length
	copy(messageData, data[9:expectedLen])

	// Unescape data (Beast protocol escapes 0x1A bytes)
	messageData = d.unescapeData(messageData)

	return &BeastMessage{
		MessageType: messageType,
		Timestamp:   timestampTime,
		Signal:      signal,
		Data:        messageData,
		Raw:         data,
	}, nil
}

// unescapeData removes Beast protocol escaping
func (d *BeastDecoder) unescapeData(data []byte) []byte {
	result := make([]byte, 0, len(data))

	for i := 0; i < len(data); i++ {
		if data[i] == 0x1A && i+1 < len(data) {
			// Escaped byte
			result = append(result, data[i+1])
			i++ // Skip the escape byte
		} else {
			result = append(result, data[i])
		}
	}

	return result
}

// GetICAO extracts ICAO address from Mode S message
func (msg *BeastMessage) GetICAO() uint32 {
	if msg.MessageType != BeastModeS && msg.MessageType != BeastModeSLong {
		return 0
	}

	if len(msg.Data) < 3 {
		return 0
	}

	// ICAO address is in bytes 1-3 of Mode S message
	return (uint32(msg.Data[1]) << 16) | (uint32(msg.Data[2]) << 8) | uint32(msg.Data[3])
}

// GetDF extracts Downlink Format from Mode S message
func (msg *BeastMessage) GetDF() byte {
	if msg.MessageType != BeastModeS && msg.MessageType != BeastModeSLong {
		return 0
	}

	if len(msg.Data) < 1 {
		return 0
	}

	// DF is in upper 5 bits of first byte
	return (msg.Data[0] >> 3) & 0x1F
}

// GetSquawk extracts squawk code from Mode A/C message
func (msg *BeastMessage) GetSquawk() uint16 {
	if msg.MessageType != BeastModeAC {
		return 0
	}

	if len(msg.Data) < 2 {
		return 0
	}

	// Decode Mode A squawk from 13-bit format
	data := (uint16(msg.Data[0]) << 8) | uint16(msg.Data[1])

	// Convert from 13-bit to 12-bit squawk
	squawk := uint16(0)
	squawk |= (data & 0x1000) >> 9  // A1
	squawk |= (data & 0x0800) >> 7  // A2
	squawk |= (data & 0x0400) >> 5  // A4
	squawk |= (data & 0x0200) >> 3  // B1
	squawk |= (data & 0x0100) >> 1  // B2
	squawk |= (data & 0x0080) << 1  // B4
	squawk |= (data & 0x0040) << 3  // C1
	squawk |= (data & 0x0020) << 5  // C2
	squawk |= (data & 0x0010) << 7  // C4
	squawk |= (data & 0x0008) << 9  // D1
	squawk |= (data & 0x0004) << 11 // D2
	squawk |= (data & 0x0002) << 13 // D4

	return squawk
}

// IsValid performs basic validation on the message
func (msg *BeastMessage) IsValid() bool {
	if len(msg.Data) == 0 {
		return false
	}

	switch msg.MessageType {
	case BeastModeAC:
		return len(msg.Data) >= 2
	case BeastModeS:
		return len(msg.Data) >= 7
	case BeastModeSLong:
		return len(msg.Data) >= 14
	case BeastModeStatus:
		return len(msg.Data) >= 2
	default:
		return false
	}
}
