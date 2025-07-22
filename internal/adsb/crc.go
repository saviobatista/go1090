package adsb

// ADS-B CRC-24 polynomial constant (Mode S standard)
const MODES_GENERATOR_POLY = 0xfff409

// Pre-computed CRC table for performance optimization
var crcTable []uint32

// Additional CRC tables for error correction (like dump1090)
var crcErrorSingleBitTable [112]uint32
var crcErrorTwoBitTable [112 * 112]uint32

// init initializes the pre-computed CRC tables
func init() {
	crcTable = make([]uint32, 256)
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

	// Initialize error correction tables (like dump1090)
	initErrorCorrectionTables()
}

// initErrorCorrectionTables initializes tables for single and two-bit error correction
func initErrorCorrectionTables() {
	// Single bit error table
	for i := 0; i < 112; i++ {
		msg := make([]byte, 14)
		// Set the bit at position i
		bytePos := i / 8
		bitPos := 7 - (i % 8)
		if bytePos < 14 {
			msg[bytePos] = 1 << bitPos
		}
		crcErrorSingleBitTable[i] = calculateCRCRaw(msg[:11])
	}

	// Two bit error table (simplified version)
	for i := 0; i < 112; i++ {
		for j := i + 1; j < 112; j++ {
			if i*112+j < len(crcErrorTwoBitTable) {
				msg := make([]byte, 14)
				// Set bits at positions i and j
				bytePos1, bitPos1 := i/8, 7-(i%8)
				bytePos2, bitPos2 := j/8, 7-(j%8)
				if bytePos1 < 14 {
					msg[bytePos1] |= 1 << bitPos1
				}
				if bytePos2 < 14 {
					msg[bytePos2] |= 1 << bitPos2
				}
				crcErrorTwoBitTable[i*112+j] = calculateCRCRaw(msg[:11])
			}
		}
	}
}

// calculateCRCRaw performs raw CRC calculation
func calculateCRCRaw(data []byte) uint32 {
	var rem uint32 = 0

	// Use pre-computed CRC table for better performance
	n := len(data)
	for i := 0; i < n; i++ {
		rem = (rem << 8) ^ crcTable[uint32(data[i])^((rem&0xff0000)>>16)]
		rem = rem & 0xffffff
	}

	return rem
}

// CalculateCRC calculates the ADS-B CRC-24 checksum using Mode S standard (from dump1090)
func CalculateCRC(data []byte) uint32 {
	return calculateCRCRaw(data)
}

// ValidateAndCorrectMessage performs CRC validation and error correction (dump1090-style)
func ValidateAndCorrectMessage(msg *ADSBMessage) (uint64, uint64, uint64) {
	var singleBitErrors, twoBitErrors, correctedMessages uint64

	// Get DF (Downlink Format) to determine message validity
	df := msg.Data[0] >> 3

	// Pre-filter invalid DF codes (dump1090 style)
	validDF := false
	switch df {
	case 0, 4, 5, 11, 16, 17, 18, 20, 21, 24:
		validDF = true
	default:
		validDF = false
	}

	if !validDF {
		msg.Valid = false
		msg.CRCType = "invalid-df"
		msg.ErrorsCorrected = 0
		return singleBitErrors, twoBitErrors, correctedMessages
	}

	// Determine message length
	msgLen := 14 // Long message
	if df == 0 || df == 4 || df == 5 || df == 11 {
		msgLen = 7 // Short message
	}

	// Calculate CRC using dump1090 method
	crc := calculateCRCRaw(msg.Data[:msgLen])
	msg.CRC = crc

	// For DF17/18, CRC should be 0
	// For DF11, CRC should have low 7 bits as 0 (IID field)
	if df == 17 || df == 18 {
		if crc == 0 {
			msg.Valid = true
			msg.CRCType = "valid"
			msg.ErrorsCorrected = 0
			return singleBitErrors, twoBitErrors, correctedMessages
		}
	} else if df == 11 {
		if (crc & 0xFFFF80) == 0 {
			msg.Valid = true
			msg.CRCType = "valid"
			msg.ErrorsCorrected = 0
			return singleBitErrors, twoBitErrors, correctedMessages
		}
	} else {
		// For other DF types, accept if CRC is 0
		if crc == 0 {
			msg.Valid = true
			msg.CRCType = "valid"
			msg.ErrorsCorrected = 0
			return singleBitErrors, twoBitErrors, correctedMessages
		}
	}

	// Only try error correction for DF11/17/18
	if df == 11 || df == 17 || df == 18 {
		// Try single-bit error correction
		for i := 0; i < len(crcErrorSingleBitTable); i++ {
			if crcErrorSingleBitTable[i] == crc {
				// Found single bit error
				bytePos := i / 8
				bitPos := 7 - (i % 8)
				if bytePos < msgLen {
					msg.Data[bytePos] ^= 1 << bitPos
					msg.Valid = true
					msg.CRCType = "corrected-1"
					msg.ErrorsCorrected = 1
					singleBitErrors++
					correctedMessages++
					return singleBitErrors, twoBitErrors, correctedMessages
				}
			}
		}

		// Try two-bit error correction (only for DF17/18)
		if df == 17 || df == 18 {
			for i := 0; i < 112; i++ {
				for j := i + 1; j < 112; j++ {
					if i*112+j < len(crcErrorTwoBitTable) && crcErrorTwoBitTable[i*112+j] == crc {
						// Found two bit error
						bytePos1, bitPos1 := i/8, 7-(i%8)
						bytePos2, bitPos2 := j/8, 7-(j%8)

						if bytePos1 < msgLen && bytePos2 < msgLen {
							msg.Data[bytePos1] ^= 1 << bitPos1
							msg.Data[bytePos2] ^= 1 << bitPos2
							msg.Valid = true
							msg.CRCType = "corrected-2"
							msg.ErrorsCorrected = 2
							twoBitErrors++
							correctedMessages++
							return singleBitErrors, twoBitErrors, correctedMessages
						}
					}
				}
			}
		}
	}

	// No correction possible
	msg.Valid = false
	msg.CRCType = "invalid"
	msg.ErrorsCorrected = 0
	return singleBitErrors, twoBitErrors, correctedMessages
}
