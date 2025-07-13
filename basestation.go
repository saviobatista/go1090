package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// BaseStation message types
const (
	BaseStationSEL = "SEL" // Selection Change
	BaseStationID  = "ID"  // New ID
	BaseStationAIR = "AIR" // New Aircraft
	BaseStationSTA = "STA" // Status Change
	BaseStationCLK = "CLK" // Click
	BaseStationMSG = "MSG" // Transmission
)

// BaseStation transmission types
const (
	TransmissionES_ID_CAT       = 1 // Extended Squitter Aircraft ID and Category
	TransmissionES_SURFACE      = 2 // Extended Squitter Surface Position
	TransmissionES_AIRBORNE     = 3 // Extended Squitter Airborne Position
	TransmissionES_VELOCITY     = 4 // Extended Squitter Airborne Velocity
	TransmissionSURVEILLANCE    = 5 // Surveillance Alt, Squawk change
	TransmissionSURVEILLANCE_ID = 6 // Surveillance ID change
	TransmissionAIR_TO_AIR      = 7 // Air-to-Air Message
	TransmissionALL_CALL        = 8 // All Call Reply
)

// BaseStationMessage represents a BaseStation format message
type BaseStationMessage struct {
	MessageType      string
	TransmissionType int
	SessionID        int
	AircraftID       int
	HexIdent         string
	FlightID         int
	DateGenerated    time.Time
	TimeGenerated    time.Time
	DateLogged       time.Time
	TimeLogged       time.Time
	Callsign         string
	Altitude         string
	GroundSpeed      string
	Track            string
	Latitude         string
	Longitude        string
	VerticalRate     string
	Squawk           string
	Alert            string
	Emergency        string
	SPI              string
	IsOnGround       string
}

// BaseStationWriter writes messages in BaseStation format
type BaseStationWriter struct {
	logRotator *LogRotator
	logger     *logrus.Logger
	sessionID  int
	aircraftID int
}

// NewBaseStationWriter creates a new BaseStation writer
func NewBaseStationWriter(logRotator *LogRotator, logger *logrus.Logger) *BaseStationWriter {
	return &BaseStationWriter{
		logRotator: logRotator,
		logger:     logger,
		sessionID:  1,
		aircraftID: 1,
	}
}

// WriteMessage writes a Beast message in BaseStation format
func (w *BaseStationWriter) WriteMessage(msg *BeastMessage) error {
	if msg == nil {
		return fmt.Errorf("message cannot be nil")
	}

	if !msg.IsValid() {
		return fmt.Errorf("invalid message")
	}

	// Convert Beast message to BaseStation format
	baseMsg := w.convertMessage(msg)
	if baseMsg == nil {
		// Message type not supported for BaseStation format
		return nil
	}

	// Format as BaseStation CSV
	csvLine := w.formatCSV(baseMsg)

	// Get current writer
	writer, err := w.logRotator.GetWriter()
	if err != nil {
		return fmt.Errorf("failed to get log writer: %w", err)
	}

	// Write to log
	if _, err := writer.Write([]byte(csvLine + "\n")); err != nil {
		return fmt.Errorf("failed to write to log: %w", err)
	}

	return nil
}

// convertMessage converts a Beast message to BaseStation format
func (w *BaseStationWriter) convertMessage(msg *BeastMessage) *BaseStationMessage {
	now := time.Now()

	baseMsg := &BaseStationMessage{
		MessageType:   BaseStationMSG,
		SessionID:     w.sessionID,
		AircraftID:    w.aircraftID,
		FlightID:      w.aircraftID,
		DateGenerated: msg.Timestamp,
		TimeGenerated: msg.Timestamp,
		DateLogged:    now,
		TimeLogged:    now,
	}

	switch msg.MessageType {
	case BeastModeAC:
		// Mode A/C message
		baseMsg.TransmissionType = TransmissionSURVEILLANCE
		baseMsg.HexIdent = ""

		squawk := msg.GetSquawk()
		if squawk != 0 {
			baseMsg.Squawk = fmt.Sprintf("%04d", squawk)
		}

		return baseMsg

	case BeastModeS, BeastModeSLong:
		// Mode S message
		icao := msg.GetICAO()
		if icao != 0 {
			baseMsg.HexIdent = fmt.Sprintf("%06X", icao)
		}

		df := msg.GetDF()

		switch df {
		case 4, 5, 20, 21:
			// Surveillance messages
			baseMsg.TransmissionType = TransmissionSURVEILLANCE

			// Extract altitude if present
			if df == 4 || df == 20 {
				altitude := w.extractAltitude(msg.Data)
				if altitude != 0 {
					baseMsg.Altitude = strconv.Itoa(altitude)
				}
			}

			// Extract squawk if present
			if df == 5 || df == 21 {
				squawk := w.extractSquawk(msg.Data)
				if squawk != 0 {
					baseMsg.Squawk = fmt.Sprintf("%04d", squawk)
				}
			}

		case 11:
			// All call reply
			baseMsg.TransmissionType = TransmissionALL_CALL

		case 17, 18, 19:
			// Extended squitter
			if len(msg.Data) >= 5 {
				typeCode := (msg.Data[4] >> 3) & 0x1F

				switch {
				case typeCode >= 1 && typeCode <= 4:
					// Aircraft identification
					baseMsg.TransmissionType = TransmissionES_ID_CAT
					baseMsg.Callsign = w.extractCallsign(msg.Data)

				case typeCode >= 5 && typeCode <= 8:
					// Surface position
					baseMsg.TransmissionType = TransmissionES_SURFACE
					lat, lon := w.extractPosition(msg.Data)
					if lat != 0 || lon != 0 {
						baseMsg.Latitude = fmt.Sprintf("%.6f", lat)
						baseMsg.Longitude = fmt.Sprintf("%.6f", lon)
					}

				case typeCode >= 9 && typeCode <= 18:
					// Airborne position
					baseMsg.TransmissionType = TransmissionES_AIRBORNE
					lat, lon := w.extractPosition(msg.Data)
					if lat != 0 || lon != 0 {
						baseMsg.Latitude = fmt.Sprintf("%.6f", lat)
						baseMsg.Longitude = fmt.Sprintf("%.6f", lon)
					}

					altitude := w.extractAltitude(msg.Data)
					if altitude != 0 {
						baseMsg.Altitude = strconv.Itoa(altitude)
					}

				case typeCode == 19:
					// Airborne velocity
					baseMsg.TransmissionType = TransmissionES_VELOCITY
					speed, track, vrate := w.extractVelocity(msg.Data)
					if speed != 0 {
						baseMsg.GroundSpeed = strconv.Itoa(speed)
					}
					if track != 0 {
						baseMsg.Track = fmt.Sprintf("%.1f", track)
					}
					if vrate != 0 {
						baseMsg.VerticalRate = strconv.Itoa(vrate)
					}
				}
			}

		default:
			// Unsupported DF
			return nil
		}

		return baseMsg

	default:
		// Unsupported message type
		return nil
	}
}

// formatCSV formats a BaseStation message as CSV
func (w *BaseStationWriter) formatCSV(msg *BaseStationMessage) string {
	fields := []string{
		msg.MessageType,
		strconv.Itoa(msg.TransmissionType),
		strconv.Itoa(msg.SessionID),
		strconv.Itoa(msg.AircraftID),
		msg.HexIdent,
		strconv.Itoa(msg.FlightID),
		msg.DateGenerated.Format("2006/01/02"),
		msg.TimeGenerated.Format("15:04:05.000"),
		msg.DateLogged.Format("2006/01/02"),
		msg.TimeLogged.Format("15:04:05.000"),
		msg.Callsign,
		msg.Altitude,
		msg.GroundSpeed,
		msg.Track,
		msg.Latitude,
		msg.Longitude,
		msg.VerticalRate,
		msg.Squawk,
		msg.Alert,
		msg.Emergency,
		msg.SPI,
		msg.IsOnGround,
	}

	return strings.Join(fields, ",")
}

// extractAltitude extracts altitude from Mode S message
func (w *BaseStationWriter) extractAltitude(data []byte) int {
	if len(data) < 3 {
		return 0
	}

	// Altitude is in bits 20-32 of the message
	altitude := (int(data[2]) << 4) | ((int(data[3]) >> 4) & 0x0F)

	if altitude == 0 {
		return 0
	}

	// Convert to feet
	return (altitude - 1) * 25
}

// extractSquawk extracts squawk code from Mode S message
func (w *BaseStationWriter) extractSquawk(data []byte) int {
	if len(data) < 3 {
		return 0
	}

	// Squawk is in bits 19-31 of the message
	squawk := ((int(data[2]) & 0x1F) << 8) | int(data[3])

	// Convert from binary to octal representation
	return ((squawk & 0x1C00) >> 2) | ((squawk & 0x0380) >> 1) | (squawk & 0x007F)
}

// extractCallsign extracts callsign from Aircraft ID message
func (w *BaseStationWriter) extractCallsign(data []byte) string {
	if len(data) < 11 {
		return ""
	}

	// Callsign is in bits 40-87 of the message
	callsign := make([]byte, 8)

	for i := 0; i < 8; i++ {
		byteIndex := 4 + (i*6)/8
		bitOffset := (i * 6) % 8

		if byteIndex >= len(data) {
			break
		}

		char := (data[byteIndex] >> (2 - bitOffset)) & 0x3F

		if char == 0x20 {
			callsign[i] = ' '
		} else if char >= 0x01 && char <= 0x1A {
			callsign[i] = 'A' + char - 1
		} else if char >= 0x30 && char <= 0x39 {
			callsign[i] = '0' + char - 0x30
		} else {
			callsign[i] = '?'
		}
	}

	return strings.TrimSpace(string(callsign))
}

// extractPosition extracts position from position message (simplified)
func (w *BaseStationWriter) extractPosition(data []byte) (float64, float64) {
	// This is a simplified position extraction
	// Real CPR (Compact Position Reporting) decoding is much more complex
	// and requires multiple messages to determine position
	return 0, 0
}

// extractVelocity extracts velocity information from velocity message
func (w *BaseStationWriter) extractVelocity(data []byte) (int, float64, int) {
	if len(data) < 9 {
		return 0, 0, 0
	}

	// Simplified velocity extraction
	subtype := (data[4] >> 1) & 0x07

	var speed int
	var track float64
	var vrate int

	if subtype == 1 || subtype == 2 {
		// Ground speed
		ewDir := (data[5] >> 2) & 0x01
		ewVel := ((int(data[5]) & 0x03) << 8) | int(data[6])

		nsDir := (data[7] >> 7) & 0x01
		nsVel := ((int(data[7]) & 0x7F) << 3) | ((int(data[8]) >> 5) & 0x07)

		if ewVel != 0 || nsVel != 0 {
			ewSpeed := float64(ewVel - 1)
			nsSpeed := float64(nsVel - 1)

			if ewDir == 1 {
				ewSpeed = -ewSpeed
			}
			if nsDir == 1 {
				nsSpeed = -nsSpeed
			}

			speed = int(ewSpeed*ewSpeed + nsSpeed*nsSpeed)
			if speed > 0 {
				speed = int(float64(speed) * 0.5) // Convert to knots
			}

			if ewSpeed != 0 || nsSpeed != 0 {
				track = float64(int(57.2958 * float64(ewSpeed) / float64(nsSpeed)))
				if track < 0 {
					track += 360
				}
			}
		}
	}

	return speed, track, vrate
}
