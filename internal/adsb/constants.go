package adsb

// ADS-B 6-bit character set: space, A-Z, 0-9
// This is the standard character set used in ADS-B callsign encoding
const ADSBCharset = "@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_ !\"#$%&'()*+,-./0123456789:;<=>?"

// CPR decoding constants
const (
	CPR_LAT_BITS = 17
	CPR_LON_BITS = 17
	CPR_LAT_MAX  = 131072 // 2^17
	CPR_LON_MAX  = 131072 // 2^17
)

// Squawk code bit manipulation constants
const (
	SquawkA4A2A1Mask = 0x07 // Mask for A4 A2 A1 bits
	SquawkB4B2B1Mask = 0x07 // Mask for B4 B2 B1 bits
	SquawkC4C2C1Mask = 0x07 // Mask for C4 C2 C1 bits
	SquawkD4D2D1Mask = 0x07 // Mask for D4 D2 D1 bits

	SquawkA4A2A1Shift = 9 // Shift for A4 A2 A1 bits
	SquawkB4B2B1Shift = 6 // Shift for B4 B2 B1 bits
	SquawkC4C2C1Shift = 3 // Shift for C4 C2 C1 bits
	SquawkD4D2D1Shift = 0 // Shift for D4 D2 D1 bits

	SquawkAMultiplier = 1000 // Multiplier for A digit
	SquawkBMultiplier = 100  // Multiplier for B digit
	SquawkCMultiplier = 10   // Multiplier for C digit
	SquawkDMultiplier = 1    // Multiplier for D digit
)
