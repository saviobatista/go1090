package main

import (
	"math"
	"math/cmplx"
	"time"

	"github.com/sirupsen/logrus"
)

// ADSBProcessor handles the complete ADS-B processing pipeline
type ADSBProcessor struct {
	logger       *logrus.Logger
	sampleRate   uint32
	messageCount uint64

	// Demodulation state
	envelope  []float64
	lastPeak  float64
	threshold float64

	// Statistics
	preambleCount uint64
	validMessages uint64
}

// ADSBMessage represents a decoded ADS-B message
type ADSBMessage struct {
	Data      [14]byte // 112 bits = 14 bytes
	Timestamp time.Time
	Signal    float64
	CRC       uint32
	Valid     bool
}

// NewADSBProcessor creates a new ADS-B processor
func NewADSBProcessor(sampleRate uint32, logger *logrus.Logger) *ADSBProcessor {
	return &ADSBProcessor{
		logger:     logger,
		sampleRate: sampleRate,
		threshold:  0.0,
	}
}

// ProcessIQSamples processes I/Q samples and extracts ADS-B messages
func (p *ADSBProcessor) ProcessIQSamples(iqData []complex128) []*ADSBMessage {
	// Convert I/Q to envelope (magnitude)
	envelope := p.calculateEnvelope(iqData)

	// Update adaptive threshold
	p.updateThreshold(envelope)

	// Detect preambles and extract messages
	return p.detectAndExtractMessages(envelope)
}

// calculateEnvelope converts I/Q samples to envelope (magnitude)
func (p *ADSBProcessor) calculateEnvelope(iqData []complex128) []float64 {
	envelope := make([]float64, len(iqData))

	for i, sample := range iqData {
		envelope[i] = cmplx.Abs(sample)
	}

	return envelope
}

// updateThreshold implements adaptive thresholding similar to dump1090
func (p *ADSBProcessor) updateThreshold(envelope []float64) {
	if len(envelope) == 0 {
		return
	}

	// Calculate statistics
	var sum, max float64
	for _, val := range envelope {
		sum += val
		if val > max {
			max = val
		}
	}

	mean := sum / float64(len(envelope))

	// Adaptive threshold: between mean and peak
	p.threshold = mean + (max-mean)*0.3
	p.lastPeak = max
}

// detectAndExtractMessages detects ADS-B preambles and extracts messages
func (p *ADSBProcessor) detectAndExtractMessages(envelope []float64) []*ADSBMessage {
	var messages []*ADSBMessage

	// ADS-B timing (for 2 MHz sample rate)
	samplesPerMicrosecond := float64(p.sampleRate) / 1000000.0
	preambleLength := int(8.0 * samplesPerMicrosecond) // 8 μs preamble
	bitLength := int(1.0 * samplesPerMicrosecond)      // 1 μs per bit
	messageLength := 112 * bitLength                   // 112 bits total

	// Scan for preambles
	for i := 0; i < len(envelope)-preambleLength-messageLength; i++ {
		if p.detectPreamble(envelope[i : i+preambleLength]) {
			p.preambleCount++

			// Extract message starting after preamble
			msgStart := i + preambleLength
			if msgStart+messageLength < len(envelope) {
				if message := p.extractMessage(envelope[msgStart:msgStart+messageLength], bitLength); message != nil {
					message.Timestamp = time.Now()
					message.Signal = p.calculateSignalStrength(envelope[i : i+preambleLength+messageLength])
					messages = append(messages, message)
					p.validMessages++

					// Skip ahead to avoid duplicate detections
					i += preambleLength + messageLength - 1
				}
			}
		}
	}

	return messages
}

// detectPreamble detects the ADS-B preamble pattern
func (p *ADSBProcessor) detectPreamble(samples []float64) bool {
	if len(samples) < 16 {
		return false
	}

	// ADS-B preamble pattern (relative to 2 MHz sampling):
	// High at positions 0, 2, 7, 9 (in terms of 0.5μs intervals)
	// Low elsewhere

	samplesPerHalfMicro := len(samples) / 16 // 16 half-microsecond intervals
	if samplesPerHalfMicro < 1 {
		samplesPerHalfMicro = 1
	}

	// Expected high positions (in half-microsecond intervals)
	highPositions := []int{0, 2, 7, 9}

	// Calculate average levels for high and low positions
	var highSum, lowSum float64
	var highCount, lowCount int

	for pos := 0; pos < 16; pos++ {
		sampleIdx := pos * samplesPerHalfMicro
		if sampleIdx >= len(samples) {
			break
		}

		// Average samples in this interval
		var intervalSum float64
		intervalCount := 0
		for j := 0; j < samplesPerHalfMicro && sampleIdx+j < len(samples); j++ {
			intervalSum += samples[sampleIdx+j]
			intervalCount++
		}

		if intervalCount > 0 {
			intervalAvg := intervalSum / float64(intervalCount)

			// Check if this position should be high
			isHigh := false
			for _, highPos := range highPositions {
				if pos == highPos {
					isHigh = true
					break
				}
			}

			if isHigh {
				highSum += intervalAvg
				highCount++
			} else {
				lowSum += intervalAvg
				lowCount++
			}
		}
	}

	if highCount == 0 || lowCount == 0 {
		return false
	}

	highAvg := highSum / float64(highCount)
	lowAvg := lowSum / float64(lowCount)

	// Preamble detected if high positions are significantly higher than low positions
	return highAvg > lowAvg*1.4 && highAvg > p.threshold*0.8
}

// extractMessage extracts a 112-bit ADS-B message from envelope data
func (p *ADSBProcessor) extractMessage(envelope []float64, bitLength int) *ADSBMessage {
	if len(envelope) < 112*bitLength {
		return nil
	}

	var bits [112]bool

	// Extract each bit using PPM (Pulse Position Modulation)
	for bitNum := 0; bitNum < 112; bitNum++ {
		bitStart := bitNum * bitLength

		if bitStart+bitLength*2 > len(envelope) {
			return nil // Not enough data
		}

		// In PPM: bit 0 = pulse in first half, bit 1 = pulse in second half
		firstHalfSum := 0.0
		secondHalfSum := 0.0

		halfBit := bitLength / 2
		if halfBit < 1 {
			halfBit = 1
		}

		// Sum first half of bit period
		for i := 0; i < halfBit && bitStart+i < len(envelope); i++ {
			firstHalfSum += envelope[bitStart+i]
		}

		// Sum second half of bit period
		for i := halfBit; i < bitLength && bitStart+i < len(envelope); i++ {
			secondHalfSum += envelope[bitStart+i]
		}

		// Bit is 1 if second half is stronger, 0 if first half is stronger
		bits[bitNum] = secondHalfSum > firstHalfSum
	}

	// Convert bits to bytes
	var messageBytes [14]byte
	for i := 0; i < 112; i++ {
		if bits[i] {
			messageBytes[i/8] |= 1 << (7 - (i % 8))
		}
	}

	// Validate CRC
	crc := p.calculateCRC(messageBytes[:11])
	messageCRC := uint32(messageBytes[11])<<16 | uint32(messageBytes[12])<<8 | uint32(messageBytes[13])

	message := &ADSBMessage{
		Data:  messageBytes,
		CRC:   crc,
		Valid: crc == messageCRC,
	}

	return message
}

// calculateSignalStrength estimates signal strength from envelope data
func (p *ADSBProcessor) calculateSignalStrength(envelope []float64) float64 {
	if len(envelope) == 0 {
		return 0.0
	}

	var max float64
	for _, val := range envelope {
		if val > max {
			max = val
		}
	}

	// Convert to dB-like scale
	if max > 0 {
		return 20 * math.Log10(max)
	}
	return 0.0
}

// calculateCRC calculates the ADS-B CRC-24 checksum
func (p *ADSBProcessor) calculateCRC(data []byte) uint32 {
	const polynomial uint32 = 0xFFF409 // ADS-B CRC polynomial

	var crc uint32 = 0

	for _, b := range data {
		crc ^= uint32(b) << 16

		for i := 0; i < 8; i++ {
			if (crc & 0x800000) != 0 {
				crc = (crc << 1) ^ polynomial
			} else {
				crc <<= 1
			}
		}
	}

	return crc & 0xFFFFFF
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
