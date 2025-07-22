package adsb

import (
	"time"
)

// ADSBMessage represents a decoded ADS-B message
type ADSBMessage struct {
	Data            [14]byte // 112 bits = 14 bytes
	Timestamp       time.Time
	Signal          float64
	CRC             uint32
	Valid           bool
	Score           int
	Phase           int
	ErrorsCorrected int    // Number of bit errors corrected
	CRCType         string // "valid", "corrected-1", "corrected-2", "invalid"
}

// AircraftPosition tracks CPR position data for an aircraft
type AircraftPosition struct {
	ICAO       uint32
	EvenFrame  *CPRFrame
	OddFrame   *CPRFrame
	LastPos    *Position
	LastUpdate time.Time
}

// AircraftState tracks position data for CPR decoding
type AircraftState struct {
	ICAO    uint32
	EvenCPR *CPRFrame
	OddCPR  *CPRFrame
	LastPos *Position
	Updated time.Time
}

// CPRFrame represents a CPR encoded position frame
type CPRFrame struct {
	LatCPR    uint32
	LonCPR    uint32
	FFlag     uint8
	Timestamp time.Time
}

// Position represents decoded lat/lon coordinates
type Position struct {
	Latitude  float64
	Longitude float64
	Timestamp time.Time
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
