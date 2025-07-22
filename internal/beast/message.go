package beast

import (
	"time"
)

// Beast mode message types
const (
	SyncByte   = 0x1A // Beast mode sync byte
	ModeAC     = 0x31 // Mode A/C
	ModeS      = 0x32 // Mode S Short (56 bits)
	ModeSLong  = 0x33 // Mode S Long (112 bits)
	ModeStatus = 0x34 // Status
)

// Message represents a decoded Beast mode message
type Message struct {
	MessageType byte
	Timestamp   time.Time
	Signal      byte
	Data        []byte
	Raw         []byte
}

// GetICAO extracts ICAO address from Mode S message
func (msg *Message) GetICAO() uint32 {
	if msg.MessageType != ModeS && msg.MessageType != ModeSLong {
		return 0
	}

	if len(msg.Data) < 3 {
		return 0
	}

	// ICAO address is in bytes 1-3 of Mode S message
	return (uint32(msg.Data[1]) << 16) | (uint32(msg.Data[2]) << 8) | uint32(msg.Data[3])
}

// GetDF extracts Downlink Format from Mode S message
func (msg *Message) GetDF() byte {
	if msg.MessageType != ModeS && msg.MessageType != ModeSLong {
		return 0
	}

	if len(msg.Data) < 1 {
		return 0
	}

	// DF is in upper 5 bits of first byte
	return (msg.Data[0] >> 3) & 0x1F
}

// GetSquawk extracts squawk code from Mode A/C message
func (msg *Message) GetSquawk() uint16 {
	if msg.MessageType != ModeAC {
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
func (msg *Message) IsValid() bool {
	if len(msg.Data) == 0 {
		return false
	}

	switch msg.MessageType {
	case ModeAC:
		return len(msg.Data) >= 2
	case ModeS:
		return len(msg.Data) >= 7
	case ModeSLong:
		return len(msg.Data) >= 14
	case ModeStatus:
		return len(msg.Data) >= 2
	default:
		return false
	}
}
