package beast

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Decoder decodes Beast mode messages
type Decoder struct {
	logger *logrus.Logger
	buffer []byte
}

// NewDecoder creates a new Beast decoder
func NewDecoder(logger *logrus.Logger) *Decoder {
	return &Decoder{
		logger: logger,
		buffer: make([]byte, 0, 4096),
	}
}

// Decode decodes Beast mode messages from raw data
func (d *Decoder) Decode(data []byte) ([]*Message, error) {
	d.buffer = append(d.buffer, data...)

	var messages []*Message

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
			if b == SyncByte {
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
func (d *Decoder) getMessageLength(messageType byte) int {
	switch messageType {
	case ModeAC:
		return 11 // 1 sync + 1 type + 6 timestamp + 1 signal + 2 data
	case ModeS:
		return 16 // 1 sync + 1 type + 6 timestamp + 1 signal + 7 data
	case ModeSLong:
		return 23 // 1 sync + 1 type + 6 timestamp + 1 signal + 14 data
	case ModeStatus:
		return 11 // 1 sync + 1 type + 6 timestamp + 1 signal + 2 data
	default:
		return 0
	}
}

// decodeMessage decodes a complete Beast message
func (d *Decoder) decodeMessage(data []byte) (*Message, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}

	if data[0] != SyncByte {
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

	return &Message{
		MessageType: messageType,
		Timestamp:   timestampTime,
		Signal:      signal,
		Data:        messageData,
		Raw:         data,
	}, nil
}

// unescapeData removes Beast protocol escaping
func (d *Decoder) unescapeData(data []byte) []byte {
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
