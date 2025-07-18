package main

import (
	"math/cmplx"
	"time"

	"github.com/sirupsen/logrus"
)

// ADSBProcessor handles the complete ADS-B processing pipeline using dump1090's approach
type ADSBProcessor struct {
	logger       *logrus.Logger
	sampleRate   uint32
	messageCount uint64

	// Statistics
	preambleCount   uint64
	validMessages   uint64
	rejectedBad     uint64
	rejectedUnknown uint64
}

// ADSBMessage represents a decoded ADS-B message
type ADSBMessage struct {
	Data      [14]byte // 112 bits = 14 bytes
	Timestamp time.Time
	Signal    float64
	CRC       uint32
	Valid     bool
	Score     int
	Phase     int
}

// NewADSBProcessor creates a new ADS-B processor
func NewADSBProcessor(sampleRate uint32, logger *logrus.Logger) *ADSBProcessor {
	return &ADSBProcessor{
		logger:     logger,
		sampleRate: sampleRate,
	}
}

// Correlation functions from dump1090 - these correlate a 1-0 pair of symbols (manchester encoded 1 bit)
// nb: the correlation functions sum to zero, so we do not need to adjust for the DC offset
func slicePhase0(m []uint16) int {
	return 5*int(m[0]) - 3*int(m[1]) - 2*int(m[2])
}

func slicePhase1(m []uint16) int {
	return 4*int(m[0]) - int(m[1]) - 3*int(m[2])
}

func slicePhase2(m []uint16) int {
	return 3*int(m[0]) + int(m[1]) - 4*int(m[2])
}

func slicePhase3(m []uint16) int {
	return 2*int(m[0]) + 3*int(m[1]) - 5*int(m[2])
}

func slicePhase4(m []uint16) int {
	return int(m[0]) + 5*int(m[1]) - 5*int(m[2]) - int(m[3])
}

// ProcessIQSamples processes I/Q samples and extracts ADS-B messages using dump1090's method
func (p *ADSBProcessor) ProcessIQSamples(iqData []complex128) []*ADSBMessage {
	// Convert I/Q to magnitude (uint16 to match dump1090)
	magnitude := p.calculateMagnitude(iqData)

	// Demodulate using dump1090's approach
	return p.demodulate2400(magnitude)
}

// calculateMagnitude converts I/Q samples to magnitude (similar to dump1090's magnitude calculation)
func (p *ADSBProcessor) calculateMagnitude(iqData []complex128) []uint16 {
	magnitude := make([]uint16, len(iqData))

	for i, sample := range iqData {
		mag := cmplx.Abs(sample)
		// Scale to uint16 range similar to dump1090
		scaled := mag * 1000 // Adjust scaling as needed
		if scaled > 65535 {
			scaled = 65535
		}
		magnitude[i] = uint16(scaled)
	}

	return magnitude
}

// demodulate2400 implements dump1090's 2.4MHz demodulation approach
func (p *ADSBProcessor) demodulate2400(m []uint16) []*ADSBMessage {
	var messages []*ADSBMessage
	mlen := len(m)

	for j := 0; j < mlen-240; j++ { // Need at least 240 samples for a long message
		preamble := m[j : j+19]

		// Quick check: rising edge 0->1 and falling edge 12->13
		if !(preamble[0] < preamble[1] && preamble[12] > preamble[13]) {
			continue
		}

		var high uint16
		var baseSignal, baseNoise uint32
		validPreamble := false

		// Check different phase patterns (from dump1090)
		if preamble[1] > preamble[2] &&
			preamble[2] < preamble[3] && preamble[3] > preamble[4] &&
			preamble[8] < preamble[9] && preamble[9] > preamble[10] &&
			preamble[10] < preamble[11] {
			// peaks at 1,3,9,11-12: phase 3
			high = (preamble[1] + preamble[3] + preamble[9] + preamble[11] + preamble[12]) / 4
			baseSignal = uint32(preamble[1]) + uint32(preamble[3]) + uint32(preamble[9])
			baseNoise = uint32(preamble[5]) + uint32(preamble[6]) + uint32(preamble[7])
			validPreamble = true
		} else if preamble[1] > preamble[2] &&
			preamble[2] < preamble[3] && preamble[3] > preamble[4] &&
			preamble[8] < preamble[9] && preamble[9] > preamble[10] &&
			preamble[11] < preamble[12] {
			// peaks at 1,3,9,12: phase 4
			high = (preamble[1] + preamble[3] + preamble[9] + preamble[12]) / 4
			baseSignal = uint32(preamble[1]) + uint32(preamble[3]) + uint32(preamble[9]) + uint32(preamble[12])
			baseNoise = uint32(preamble[5]) + uint32(preamble[6]) + uint32(preamble[7]) + uint32(preamble[8])
			validPreamble = true
		}
		// Add other phase patterns as needed...

		if !validPreamble {
			continue
		}

		// Check for enough signal (about 3.5dB SNR)
		if baseSignal*2 < 3*baseNoise {
			continue
		}

		// Check that the "quiet" bits are actually quiet
		if preamble[5] >= high || preamble[6] >= high || preamble[7] >= high ||
			preamble[8] >= high || preamble[14] >= high || preamble[15] >= high ||
			preamble[16] >= high || preamble[17] >= high || preamble[18] >= high {
			continue
		}

		p.preambleCount++

		// Try all phases and find the best scoring message
		bestMessage := p.tryAllPhases(m[j:], j)
		if bestMessage != nil {
			messages = append(messages, bestMessage)

			if bestMessage.Valid {
				p.validMessages++
			} else {
				p.rejectedBad++
			}

			// Skip ahead to avoid overlapping messages
			msgLen := 14 // Assume long message for now
			if bestMessage.Data[0]>>3 == 0 || bestMessage.Data[0]>>3 == 4 || bestMessage.Data[0]>>3 == 5 || bestMessage.Data[0]>>3 == 11 {
				msgLen = 7 // Short message
			}
			j += msgLen * 12 / 5 // samples per message
		} else {
			p.rejectedUnknown++
		}
	}

	return messages
}

// tryAllPhases tries decoding with different phases and returns the best scoring message
func (p *ADSBProcessor) tryAllPhases(m []uint16, position int) *ADSBMessage {
	var bestMessage *ADSBMessage
	bestScore := -1

	// Try phases 4-8 like dump1090
	for tryPhase := 4; tryPhase <= 8; tryPhase++ {
		message := p.decodeBitsWithPhase(m, tryPhase)
		if message == nil {
			continue
		}

		message.Phase = tryPhase
		message.Timestamp = time.Now()

		// Calculate CRC and validate
		calculatedCRC := p.calculateCRC(message.Data[:11])
		messageCRC := uint32(message.Data[11])<<16 | uint32(message.Data[12])<<8 | uint32(message.Data[13])
		message.CRC = calculatedCRC
		message.Valid = calculatedCRC == messageCRC

		// Score the message (simplified version of dump1090's scoring)
		score := p.scoreMessage(message)
		message.Score = score

		if score > bestScore {
			bestMessage = message
			bestScore = score
		}
	}

	return bestMessage
}

// decodeBitsWithPhase decodes 112 bits using the specified phase
func (p *ADSBProcessor) decodeBitsWithPhase(m []uint16, tryPhase int) *ADSBMessage {
	const MODES_LONG_MSG_BYTES = 14

	if len(m) < 19+MODES_LONG_MSG_BYTES*19 {
		return nil
	}

	var msg [MODES_LONG_MSG_BYTES]byte
	pPtr := 19 + (tryPhase / 5)
	phase := tryPhase % 5

	for i := 0; i < MODES_LONG_MSG_BYTES; i++ {
		if pPtr+20 >= len(m) {
			return nil
		}

		var theByte uint8

		switch phase {
		case 0:
			theByte =
				(p.bitValue(slicePhase0(m[pPtr:pPtr+3])) << 7) |
					(p.bitValue(slicePhase2(m[pPtr+2:pPtr+5])) << 6) |
					(p.bitValue(slicePhase4(m[pPtr+4:pPtr+8])) << 5) |
					(p.bitValue(slicePhase1(m[pPtr+7:pPtr+10])) << 4) |
					(p.bitValue(slicePhase3(m[pPtr+9:pPtr+12])) << 3) |
					(p.bitValue(slicePhase0(m[pPtr+12:pPtr+15])) << 2) |
					(p.bitValue(slicePhase2(m[pPtr+14:pPtr+17])) << 1) |
					(p.bitValue(slicePhase4(m[pPtr+16:pPtr+20])) << 0)
			phase = 1
			pPtr += 19

		case 1:
			theByte =
				(p.bitValue(slicePhase1(m[pPtr:pPtr+3])) << 7) |
					(p.bitValue(slicePhase3(m[pPtr+2:pPtr+5])) << 6) |
					(p.bitValue(slicePhase0(m[pPtr+5:pPtr+8])) << 5) |
					(p.bitValue(slicePhase2(m[pPtr+7:pPtr+10])) << 4) |
					(p.bitValue(slicePhase4(m[pPtr+9:pPtr+13])) << 3) |
					(p.bitValue(slicePhase1(m[pPtr+12:pPtr+15])) << 2) |
					(p.bitValue(slicePhase3(m[pPtr+14:pPtr+17])) << 1) |
					(p.bitValue(slicePhase0(m[pPtr+17:pPtr+20])) << 0)
			phase = 2
			pPtr += 19

		case 2:
			theByte =
				(p.bitValue(slicePhase2(m[pPtr:pPtr+3])) << 7) |
					(p.bitValue(slicePhase4(m[pPtr+2:pPtr+6])) << 6) |
					(p.bitValue(slicePhase1(m[pPtr+5:pPtr+8])) << 5) |
					(p.bitValue(slicePhase3(m[pPtr+7:pPtr+10])) << 4) |
					(p.bitValue(slicePhase0(m[pPtr+10:pPtr+13])) << 3) |
					(p.bitValue(slicePhase2(m[pPtr+12:pPtr+15])) << 2) |
					(p.bitValue(slicePhase4(m[pPtr+14:pPtr+18])) << 1) |
					(p.bitValue(slicePhase1(m[pPtr+17:pPtr+20])) << 0)
			phase = 3
			pPtr += 19

		case 3:
			theByte =
				(p.bitValue(slicePhase3(m[pPtr:pPtr+3])) << 7) |
					(p.bitValue(slicePhase0(m[pPtr+3:pPtr+6])) << 6) |
					(p.bitValue(slicePhase2(m[pPtr+5:pPtr+8])) << 5) |
					(p.bitValue(slicePhase4(m[pPtr+7:pPtr+11])) << 4) |
					(p.bitValue(slicePhase1(m[pPtr+10:pPtr+13])) << 3) |
					(p.bitValue(slicePhase3(m[pPtr+12:pPtr+15])) << 2) |
					(p.bitValue(slicePhase0(m[pPtr+15:pPtr+18])) << 1) |
					(p.bitValue(slicePhase2(m[pPtr+17:pPtr+20])) << 0)
			phase = 4
			pPtr += 19

		case 4:
			theByte =
				(p.bitValue(slicePhase4(m[pPtr:pPtr+4])) << 7) |
					(p.bitValue(slicePhase1(m[pPtr+3:pPtr+6])) << 6) |
					(p.bitValue(slicePhase3(m[pPtr+5:pPtr+8])) << 5) |
					(p.bitValue(slicePhase0(m[pPtr+8:pPtr+11])) << 4) |
					(p.bitValue(slicePhase2(m[pPtr+10:pPtr+13])) << 3) |
					(p.bitValue(slicePhase4(m[pPtr+12:pPtr+16])) << 2) |
					(p.bitValue(slicePhase1(m[pPtr+15:pPtr+18])) << 1) |
					(p.bitValue(slicePhase3(m[pPtr+17:pPtr+20])) << 0)
			phase = 0
			pPtr += 20

		default:
			return nil
		}

		msg[i] = theByte

		// Early termination for short messages
		if i == 0 {
			df := msg[0] >> 3
			if df == 0 || df == 4 || df == 5 || df == 11 {
				// Short message - decode only 7 bytes
				if i+1 < 7 {
					continue
				} else {
					// Fill remaining bytes with zeros for CRC calculation
					for j := 7; j < MODES_LONG_MSG_BYTES; j++ {
						msg[j] = 0
					}
					break
				}
			}
		}
	}

	return &ADSBMessage{
		Data: msg,
	}
}

// bitValue converts correlation result to bit value
func (p *ADSBProcessor) bitValue(correlation int) uint8 {
	if correlation > 0 {
		return 1
	}
	return 0
}

// scoreMessage scores a decoded message (simplified version of dump1090's scoring)
func (p *ADSBProcessor) scoreMessage(msg *ADSBMessage) int {
	if !msg.Valid {
		return -1 // Invalid CRC
	}

	// Base score for valid CRC
	score := 1000

	// Check DF (Downlink Format) validity
	df := msg.Data[0] >> 3
	switch df {
	case 0, 4, 5, 11, 16, 17, 18, 20, 21, 24:
		// Valid DF codes
		score += 500
	default:
		// Invalid DF
		return -1
	}

	return score
}

// calculateCRC calculates the ADS-B CRC-24 checksum using Mode S standard (from dump1090)
func (p *ADSBProcessor) calculateCRC(data []byte) uint32 {
	const MODES_GENERATOR_POLY = 0xfff409

	var rem uint32 = 0

	// Initialize CRC table if needed (simplified version)
	crcTable := make([]uint32, 256)
	for i := 0; i < 256; i++ {
		c := uint32(i) << 16
		for j := 0; j < 8; j++ {
			if c&0x800000 != 0 {
				c = (c << 1) ^ MODES_GENERATOR_POLY
			} else {
				c = c << 1
			}
		}
		crcTable[i] = c & 0x00ffffff
	}

	// Calculate CRC
	n := len(data)
	for i := 0; i < n; i++ {
		rem = (rem << 8) ^ crcTable[uint32(data[i])^((rem&0xff0000)>>16)]
		rem = rem & 0xffffff
	}

	return rem
}

// GetStats returns processing statistics
func (p *ADSBProcessor) GetStats() (uint64, uint64, uint64) {
	return p.messageCount, p.preambleCount, p.validMessages
}

// GetICAO extracts ICAO address from ADS-B message
func (msg *ADSBMessage) GetICAO() uint32 {
	if len(msg.Data) < 4 {
		return 0
	}
	return uint32(msg.Data[1])<<16 | uint32(msg.Data[2])<<8 | uint32(msg.Data[3])
}

// GetDF extracts Downlink Format from ADS-B message
func (msg *ADSBMessage) GetDF() uint8 {
	return (msg.Data[0] >> 3) & 0x1F
}

// GetTypeCode extracts Type Code for DF17/18 messages
func (msg *ADSBMessage) GetTypeCode() uint8 {
	if msg.GetDF() != 17 && msg.GetDF() != 18 {
		return 0
	}
	if len(msg.Data) < 5 {
		return 0
	}
	return (msg.Data[4] >> 3) & 0x1F
}
