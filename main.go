package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// ADS-B 6-bit character set: space, A-Z, 0-9
// This is the standard character set used in ADS-B callsign encoding
const adsbCharset = "@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_ !\"#$%&'()*+,-./0123456789:;<=>?"

// CPR decoding constants
const (
	CPR_LAT_BITS = 17
	CPR_LON_BITS = 17
	CPR_LAT_MAX  = 131072 // 2^17
	CPR_LON_MAX  = 131072 // 2^17
)

// Default configuration constants
const (
	DefaultFrequency  = 1090000000 // 1090 MHz
	DefaultSampleRate = 2400000    // 2.4 MHz (same as dump1090)
	DefaultGain       = 40         // Manual gain
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

// Version information (set by build flags)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// AircraftPosition tracks CPR position data for an aircraft
type AircraftPosition struct {
	ICAO       uint32
	EvenFrame  *CPRFrame
	OddFrame   *CPRFrame
	LastPos    *Position
	LastUpdate time.Time
}

// Application represents the main application
type Application struct {
	config        Config
	logger        *logrus.Logger
	rtlsdr        *RTLSDRDevice
	adsbProcessor *ADSBProcessor
	baseStation   *BaseStationWriter
	logRotator    *LogRotator
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	verbose       bool

	// Aircraft position tracking for CPR decoding
	aircraftPositions map[uint32]*AircraftPosition
	positionMutex     sync.RWMutex
}

// Config holds application configuration
type Config struct {
	Frequency    uint32
	SampleRate   uint32
	Gain         int
	DeviceIndex  int
	LogDir       string
	LogRotateUTC bool
	Verbose      bool
	ShowVersion  bool
}

// NewApplication creates a new application instance
func NewApplication(config Config) *Application {
	ctx, cancel := context.WithCancel(context.Background())

	logger := logrus.New()
	if config.Verbose {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	return &Application{
		config:            config,
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		verbose:           config.Verbose,
		aircraftPositions: make(map[uint32]*AircraftPosition),
	}
}

// Start starts the application
func (app *Application) Start() error {
	app.logger.WithFields(logrus.Fields{
		"version":    Version,
		"build_time": BuildTime,
		"git_commit": GitCommit,
	}).Info("Starting ADS-B Decoder (dump1090-style)")

	// Initialize components
	if err := app.initializeComponents(); err != nil {
		return fmt.Errorf("failed to initialize components: %w", err)
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start processing
	if err := app.run(); err != nil {
		app.logger.WithError(err).Error("Application error")
		return err
	}

	// Wait for shutdown signal
	<-sigChan
	app.logger.Info("Received shutdown signal")
	app.shutdown()

	return nil
}

// initializeComponents initializes all application components
func (app *Application) initializeComponents() error {
	var err error

	// Initialize RTL-SDR device
	app.rtlsdr, err = NewRTLSDRDevice(app.config.DeviceIndex)
	if err != nil {
		return fmt.Errorf("failed to initialize RTL-SDR: %w", err)
	}

	// Configure RTL-SDR
	if err := app.rtlsdr.Configure(app.config.Frequency, app.config.SampleRate, app.config.Gain); err != nil {
		return fmt.Errorf("failed to configure RTL-SDR: %w", err)
	}

	// Initialize ADS-B processor
	app.adsbProcessor = NewADSBProcessor(app.config.SampleRate, app.logger)

	// Initialize log rotator
	app.logRotator, err = NewLogRotator(app.config.LogDir, app.config.LogRotateUTC, app.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize log rotator: %w", err)
	}

	// Initialize BaseStation writer
	app.baseStation = NewBaseStationWriter(app.logRotator, app.logger)

	return nil
}

// run runs the main application loop
func (app *Application) run() error {
	app.logger.Info("Starting RTL-SDR capture and ADS-B demodulation")

	// Create data channel for RTL-SDR I/Q samples
	dataChan := make(chan []byte, 100)

	// Start RTL-SDR data capture
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		if err := app.rtlsdr.StartCapture(app.ctx, dataChan); err != nil {
			app.logger.WithError(err).Error("RTL-SDR capture failed")
		}
	}()

	// Start log rotation
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		app.logRotator.Start(app.ctx)
	}()

	// Process I/Q data and demodulate ADS-B
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		app.processIQData(dataChan)
	}()

	// Start statistics reporting
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		app.reportStatistics()
	}()

	app.logger.Info("All components started successfully")
	return nil
}

// Helper: Convert raw bytes to complex128 I/Q samples (unsigned 8-bit to signed)
func bytesToIQ(data []byte) []complex128 {
	samples := make([]complex128, len(data)/2)
	sampleIndex := 0
	for i := 0; i < len(data)-1; i += 2 {
		iSample := float64(data[i]) - 127.5
		qSample := float64(data[i+1]) - 127.5
		samples[sampleIndex] = complex(iSample, qSample)
		sampleIndex++
	}
	return samples
}

// processIQData processes incoming I/Q data from RTL-SDR
func (app *Application) processIQData(dataChan <-chan []byte) {
	sampleCount := 0
	dataPackets := 0

	for {
		select {
		case <-app.ctx.Done():
			app.logger.Info("I/Q data processing stopped")
			return
		case data := <-dataChan:
			if data == nil {
				continue
			}

			dataPackets++
			sampleCount += len(data) / 2 // I/Q pairs

			// Log periodic statistics
			if dataPackets%100 == 0 {
				app.logger.WithFields(logrus.Fields{
					"packets":   dataPackets,
					"samples":   sampleCount,
					"data_size": len(data),
				}).Debug("I/Q data stats")
			}

			// Convert raw bytes to I/Q samples
			iqSamples := bytesToIQ(data)

			// Log first few samples for debugging
			if dataPackets <= 3 {
				app.logger.WithFields(logrus.Fields{
					"packet":       dataPackets,
					"iq_samples":   len(iqSamples),
					"first_sample": iqSamples[0],
				}).Debug("Sample data")
			}

			// Process with ADS-B decoder
			messages := app.adsbProcessor.ProcessIQSamples(iqSamples)

			// Convert valid messages to SBS format
			for _, msg := range messages {
				if msg.Valid {
					if err := app.writeADSBMessage(msg); err != nil {
						app.logger.WithError(err).Debug("Failed to write SBS message")
					}
				}
			}
		}
	}
}

// writeADSBMessage converts ADS-B message to SBS format and writes it
func (app *Application) writeADSBMessage(msg *ADSBMessage) error {
	// Convert ADS-B message to BaseStation format
	sbs := app.convertToSBS(msg)
	if sbs == "" {
		return nil // Skip unsupported message types
	}

	// Get current writer
	writer, err := app.logRotator.GetWriter()
	if err != nil {
		return fmt.Errorf("failed to get log writer: %w", err)
	}

	// Write to log and stdout
	line := sbs + "\n"
	if _, err := writer.Write([]byte(line)); err != nil {
		return fmt.Errorf("failed to write to log: %w", err)
	}

	// Also print to stdout like dump1090
	fmt.Print(line)

	return nil
}

// convertToSBS converts ADS-B message to SBS (BaseStation) format
func (app *Application) convertToSBS(msg *ADSBMessage) string {
	now := time.Now().UTC()
	dateStr := now.Format("2006/01/02")
	timeStr := now.Format("15:04:05.000")

	icao := fmt.Sprintf("%06X", msg.GetICAO())
	df := msg.GetDF()

	// SBS message format: MSG,transmission_type,session_id,aircraft_id,hex_ident,flight_id,date_gen,time_gen,date_log,time_log,callsign,altitude,ground_speed,track,lat,lon,vertical_rate,squawk,alert,emergency,spi,is_on_ground

	sessionID := "1"
	aircraftID := "1"
	flightID := "1"

	switch df {
	case 17, 18: // Extended Squitter
		typeCode := msg.GetTypeCode()
		transmissionType := "3" // Default to airborne position

		if app.verbose {
			app.logger.Debugf("Extended Squitter: DF=%d, TypeCode=%d, ICAO=%06X", df, typeCode, msg.GetICAO())
		}

		// Initialize all fields as empty
		callsign := ""
		altitude := ""
		groundSpeed := ""
		track := ""
		latitude := ""
		longitude := ""
		verticalRate := ""
		squawk := ""
		alert := ""
		emergency := ""
		spi := ""
		isOnGround := app.extractGroundState(msg.Data[:])

		// Extract alert, emergency, and SPI flags
		if alertFlag, spiFlag := app.extractAlertSPI(msg.Data[:]); alertFlag || spiFlag {
			if alertFlag {
				alert = "1"
			}
			if spiFlag {
				spi = "1"
			}
		}

		if emergencyStatus := app.extractEmergency(msg.Data[:]); emergencyStatus != "" {
			emergency = emergencyStatus
		}

		// Parse based on type code
		switch {
		case typeCode >= 1 && typeCode <= 4:
			// Aircraft identification
			transmissionType = "1"
			callsign = app.extractCallsign(msg.Data[:])

		case typeCode >= 5 && typeCode <= 8:
			// Surface position
			transmissionType = "2"
			isOnGround = "1"
			if lat, lon := app.extractPosition(msg.Data[:]); lat != 0 || lon != 0 {
				latitude = fmt.Sprintf("%.6f", lat)
				longitude = fmt.Sprintf("%.6f", lon)
			}

		case typeCode >= 9 && typeCode <= 18:
			// Airborne position
			transmissionType = "3"
			if alt := app.extractAltitude(msg.Data[:]); alt != 0 {
				altitude = fmt.Sprintf("%d", alt)
			}
			// Extract position (lat/lon)
			if lat, lon := app.extractPosition(msg.Data[:]); lat != 0 || lon != 0 {
				latitude = fmt.Sprintf("%.6f", lat)
				longitude = fmt.Sprintf("%.6f", lon)
			}

		case typeCode >= 19 && typeCode <= 22:
			// Airborne velocity
			transmissionType = "4"
			if app.verbose {
				app.logger.Debugf("Processing velocity message, type code: %d", typeCode)
			}
			if speed, trk, vrate := app.extractVelocity(msg.Data[:]); speed > 0 || trk > 0 || vrate != 0 {
				if speed > 0 {
					groundSpeed = fmt.Sprintf("%d", speed)
				}
				if trk > 0 {
					track = fmt.Sprintf("%.1f", trk)
				}
				if vrate != 0 {
					verticalRate = fmt.Sprintf("%d", vrate)
				}
				if app.verbose {
					app.logger.Debugf("Extracted velocity: speed=%d, track=%.1f, vrate=%d", speed, trk, vrate)
				}
			} else if app.verbose {
				app.logger.Debugf("Failed to extract velocity data")
			}

		case typeCode == 28:
			// Aircraft status
			transmissionType = "7"
			if app.verbose {
				app.logger.Debugf("Aircraft status message received")
			}

		case typeCode == 31:
			// Aircraft operation status
			transmissionType = "8"
			if app.verbose {
				app.logger.Debugf("Aircraft operation status message received")
			}

		default:
			if app.verbose {
				app.logger.Debugf("Unhandled type code: %d", typeCode)
			}
			// For unknown type codes, use default transmission type 3
		}

		return fmt.Sprintf("MSG,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s",
			transmissionType, sessionID, aircraftID, icao, flightID,
			dateStr, timeStr, dateStr, timeStr,
			callsign, altitude, groundSpeed, track, latitude, longitude,
			verticalRate, squawk, alert, emergency, spi, isOnGround)

	case 4, 5, 20, 21: // Surveillance replies
		transmissionType := "5" // Surveillance

		if app.verbose {
			app.logger.Debugf("Surveillance message: DF=%d, ICAO=%06X", df, msg.GetICAO())
		}

		altitude := ""
		squawk := ""
		alert := ""
		emergency := ""
		spi := ""
		isOnGround := app.extractGroundState(msg.Data[:])

		// Extract alert and SPI flags
		if alertFlag, spiFlag := app.extractAlertSPI(msg.Data[:]); alertFlag || spiFlag {
			if alertFlag {
				alert = "1"
			}
			if spiFlag {
				spi = "1"
			}
		}

		if df == 4 || df == 20 {
			if alt := app.extractAltitude(msg.Data[:]); alt != 0 {
				altitude = fmt.Sprintf("%d", alt)
				if app.verbose {
					app.logger.Debugf("Surveillance altitude: %d", alt)
				}
			}
		}

		if df == 5 || df == 21 {
			if sq := app.extractSquawk(msg.Data[:]); sq != 0 {
				squawk = fmt.Sprintf("%04d", sq)
				if app.verbose {
					app.logger.Debugf("Surveillance squawk: %04d", sq)
				}
			}
		}

		return fmt.Sprintf("MSG,%s,%s,%s,%s,%s,%s,%s,%s,%s,,%s,,,,,%s,%s,%s,%s,%s",
			transmissionType, sessionID, aircraftID, icao, flightID,
			dateStr, timeStr, dateStr, timeStr,
			altitude, squawk, alert, emergency, spi, isOnGround)

	default:
		if app.verbose {
			app.logger.Debugf("Unhandled DF: %d, ICAO=%06X", df, msg.GetICAO())
		}
		return "" // Skip unknown downlink formats
	}

	return "" // Unsupported message type
}

// extractCallsign extracts callsign from aircraft identification message (dump1090 style)
func (app *Application) extractCallsign(data []byte) string {
	if len(data) < 11 {
		return ""
	}

	// Debug: print the raw data for analysis
	if app.verbose {
		app.logger.Debugf("Callsign raw data: %x", data[:11])
	}

	// ME (Message Extended) field starts at byte 4 for DF17/18
	me := data[4:]
	if len(me) < 7 {
		return ""
	}

	// Extract callsign using dump1090's exact method: bits 9-14, 15-20, 21-26, etc. (1-based)
	var callsign [9]byte // 8 chars + null terminator

	callsign[0] = adsbCharset[app.getBits(me, 9, 14)]  // bits 9-14 in ME
	callsign[1] = adsbCharset[app.getBits(me, 15, 20)] // bits 15-20 in ME
	callsign[2] = adsbCharset[app.getBits(me, 21, 26)] // bits 21-26 in ME
	callsign[3] = adsbCharset[app.getBits(me, 27, 32)] // bits 27-32 in ME
	callsign[4] = adsbCharset[app.getBits(me, 33, 38)] // bits 33-38 in ME
	callsign[5] = adsbCharset[app.getBits(me, 39, 44)] // bits 39-44 in ME
	callsign[6] = adsbCharset[app.getBits(me, 45, 50)] // bits 45-50 in ME
	callsign[7] = adsbCharset[app.getBits(me, 51, 56)] // bits 51-56 in ME
	callsign[8] = 0

	// Debug individual characters
	if app.verbose {
		for i := 0; i < 8; i++ {
			app.logger.Debugf("Char %d: raw=0x%02x (%d) -> '%c'", i, callsign[i], callsign[i], callsign[i])
		}
	}

	// Validate callsign (dump1090 style validation)
	valid := true
	for i := 0; i < 8; i++ {
		if !((callsign[i] >= 'A' && callsign[i] <= 'Z') ||
			(callsign[i] >= '0' && callsign[i] <= '9') ||
			callsign[i] == ' ') {
			valid = false
			break
		}
	}

	if !valid {
		if app.verbose {
			app.logger.Debugf("Invalid callsign characters detected")
		}
		return ""
	}

	result := strings.TrimSpace(string(callsign[:8]))
	if app.verbose {
		app.logger.Debugf("Extracted callsign: '%s'", result)
	}
	return result
}

// getBits extracts bits from data using 1-based indexing (like dump1090)
func (app *Application) getBits(data []byte, firstBit, lastBit int) uint8 {
	if firstBit < 1 || lastBit < firstBit || len(data) == 0 {
		return 0
	}

	// Convert to 0-based indexing
	fbi := firstBit - 1
	lbi := lastBit - 1
	nbi := lastBit - firstBit + 1

	if nbi > 8 {
		return 0 // Can't extract more than 8 bits into uint8
	}

	fby := fbi / 8
	lby := lbi / 8

	if lby >= len(data) {
		return 0
	}

	shift := 7 - (lbi % 8)
	topMask := uint8(0xFF >> (fbi % 8))

	if fby == lby {
		// All bits in the same byte
		return (data[fby] & topMask) >> shift
	} else if lby == fby+1 {
		// Bits span two bytes
		return ((data[fby] & topMask) << (8 - shift)) | (data[lby] >> shift)
	} else if lby == fby+2 {
		// Bits span three bytes (needed for callsign extraction)
		return ((data[fby] & topMask) << (16 - shift)) | (data[fby+1] << (8 - shift)) | (data[lby] >> shift)
	}

	// For even more complex cases (velocity extraction needs up to 10-bit values)
	var result uint32
	for i := fby; i <= lby && i < len(data); i++ {
		if i == fby {
			result = uint32(data[i] & topMask)
		} else {
			result = (result << 8) | uint32(data[i])
		}
	}

	// Handle larger bit extractions for velocity fields
	if nbi <= 32 {
		return uint8((result >> shift) & ((1 << nbi) - 1))
	}

	return uint8(result >> shift)
}

// getBitsUint16 extracts bits from data using 1-based indexing, returning uint16 for larger values
func (app *Application) getBitsUint16(data []byte, firstBit, lastBit int) uint16 {
	if firstBit < 1 || lastBit < firstBit || len(data) == 0 {
		return 0
	}

	// Convert to 0-based indexing
	fbi := firstBit - 1
	lbi := lastBit - 1
	nbi := lastBit - firstBit + 1

	if nbi > 16 {
		return 0 // Can't extract more than 16 bits into uint16
	}

	fby := fbi / 8
	lby := lbi / 8

	if lby >= len(data) {
		return 0
	}

	shift := 7 - (lbi % 8)
	topMask := uint8(0xFF >> (fbi % 8))

	var result uint32
	for i := fby; i <= lby && i < len(data); i++ {
		if i == fby {
			result = uint32(data[i] & topMask)
		} else {
			result = (result << 8) | uint32(data[i])
		}
	}

	return uint16((result >> shift) & ((1 << nbi) - 1))
}

// extractAltitude extracts altitude from surveillance or position messages
func (app *Application) extractAltitude(data []byte) int {
	if len(data) < 6 {
		return 0
	}

	// Extract 13-bit altitude field (different positions for different message types)
	df := (data[0] >> 3) & 0x1F

	var altCode uint16

	if df == 4 || df == 20 {
		// Surveillance altitude reply - bits 20-32
		altCode = (uint16(data[2]&0x1F) << 8) | uint16(data[3])
	} else if df == 17 || df == 18 {
		// Extended squitter - altitude is in ME field bits 9-20 (AC12 field)
		// ME starts at byte 4, so bits 9-20 of ME are in bytes 5-6 of the full message
		// Extract 12-bit AC12 field properly
		altCode = (uint16(data[5]&0x1F) << 7) | (uint16(data[6]) >> 1)
	} else {
		return 0
	}

	if altCode == 0 {
		return 0
	}

	// Decode altitude using dump1090's AC12 method
	// Check Q-bit (bit 4 of the 12-bit field)
	qBit := (altCode & 0x10) != 0

	if qBit {
		// 25-foot resolution encoding (dump1090's decodeAC12Field)
		// N is the 11 bit integer resulting from the removal of bit Q at bit 4
		n := ((altCode & 0x0FE0) >> 1) | (altCode & 0x000F)
		// The final altitude is the resulting number multiplied by 25, minus 1000
		return int(n)*25 - 1000
	} else {
		// 100-foot resolution (Gillham Mode C encoding)
		// Make N a 13 bit Gillham coded altitude by inserting M=0 at bit 6
		n13 := ((altCode & 0x0FC0) << 1) | (altCode & 0x003F)

		if n13 == 0 {
			return 0
		}

		// Improved Gillham Mode C decoding (based on dump1090's modeAToModeC)
		// This is still simplified but much more accurate than before

		// Gray code to binary conversion for altitude
		// Extract the individual bits for proper Gillham decoding
		c1 := int((n13 >> 11) & 1)
		a1 := int((n13 >> 10) & 1)
		c2 := int((n13 >> 9) & 1)
		a2 := int((n13 >> 8) & 1)
		c4 := int((n13 >> 7) & 1)
		a4 := int((n13 >> 6) & 1)
		// bit 6 is M (should be 0)
		b1 := int((n13 >> 5) & 1)
		b2 := int((n13 >> 4) & 1)

		// Basic validation - reject obviously invalid patterns
		if (c1 == 0 && a1 == 0 && c2 == 0 && a2 == 0) ||
			(c1 == 1 && a1 == 1 && c2 == 1 && a2 == 1) {
			return 0 // Invalid pattern
		}

		// Simplified conversion - convert to 500ft increments first
		hundreds := c1*4 + a1*2 + c2*1
		if a2 == 1 {
			hundreds = 7 - hundreds
		}

		fiveHundreds := c4*4 + a4*2 + b1*1
		if b2 == 1 {
			fiveHundreds = 7 - fiveHundreds
		}

		// Combine and convert to feet (each unit = 100ft)
		altitude := (fiveHundreds*5 + hundreds) * 100

		// Sanity check - reject unrealistic altitudes
		if altitude < -2000 || altitude > 60000 {
			return 0
		}

		return altitude
	}
}

// extractSquawk extracts squawk code from surveillance messages
func (app *Application) extractSquawk(data []byte) int {
	if len(data) < 4 {
		return 0
	}

	// Extract 13-bit identity field
	identity := (uint16(data[2]&0x1F) << 8) | uint16(data[3])

	// Convert to 4-digit squawk code
	squawk := 0
	squawk += int((identity>>SquawkA4A2A1Shift)&SquawkA4A2A1Mask) * SquawkAMultiplier // A4 A2 A1
	squawk += int((identity>>SquawkB4B2B1Shift)&SquawkB4B2B1Mask) * SquawkBMultiplier // B4 B2 B1
	squawk += int((identity>>SquawkC4C2C1Shift)&SquawkC4C2C1Mask) * SquawkCMultiplier // C4 C2 C1
	squawk += int((identity>>SquawkD4D2D1Shift)&SquawkD4D2D1Mask) * SquawkDMultiplier // D4 D2 D1

	return squawk
}

// extractVelocity extracts velocity information from airborne velocity messages
func (app *Application) extractVelocity(data []byte) (int, float64, int) {
	if len(data) < 11 {
		if app.verbose {
			app.logger.Debugf("Velocity extraction failed: data too short (%d bytes)", len(data))
		}
		return 0, 0, 0
	}

	// Extract velocity subtype
	subtype := (data[4] >> 1) & 0x07

	if app.verbose {
		app.logger.Debugf("Velocity message: subtype=%d, data=%x", subtype, data[:11])
	}

	if subtype < 1 || subtype > 4 {
		if app.verbose {
			app.logger.Debugf("Velocity extraction failed: unsupported subtype %d (only 1-4 supported)", subtype)
		}
		return 0, 0, 0 // Only handle groundspeed and airspeed subtypes (1-4)
	}

	var groundSpeed int
	var track float64

	if subtype == 1 || subtype == 2 {
		// Ground speed subtypes (dump1090 method)
		// ME field starts at data[4], so velocity bits are in ME[1-4]
		me := data[4:]

		// Extract east-west velocity (bits 15-24 of ME)
		ewRaw := app.getBitsUint16(me, 15, 24)
		// Extract north-south velocity (bits 26-35 of ME)
		nsRaw := app.getBitsUint16(me, 26, 35)

		if app.verbose {
			app.logger.Debugf("Ground speed components: ewDir=%d, ewVel=%d, nsDir=%d, nsVel=%d",
				app.getBits(me, 14, 14), ewRaw, app.getBits(me, 25, 25), nsRaw)
		}

		if ewRaw != 0 && nsRaw != 0 {
			// Convert to signed velocities (dump1090 style)
			ewVel := int(ewRaw-1) * (1 << (subtype - 1)) // subtype 1: *1, subtype 2: *4
			if app.getBits(me, 14, 14) != 0 {
				ewVel = -ewVel
			}

			nsVel := int(nsRaw-1) * (1 << (subtype - 1))
			if app.getBits(me, 25, 25) != 0 {
				nsVel = -nsVel
			}

      // Calculate ground speed and track (dump1090 method)
			groundSpeed = int(math.Sqrt(float64(nsVel*nsVel+ewVel*ewVel)) + 0.5)

			if groundSpeed > 0 {
				track = math.Atan2(float64(ewVel), float64(nsVel)) * 180.0 / math.Pi
				if track < 0 {
					track += 360
				}

				if app.verbose {
					app.logger.Debugf("Valid ground speed: %d kt, track: %.1f°", groundSpeed, track)
				}
			}
		}

	} else if subtype == 3 || subtype == 4 {
		// Airspeed subtypes (dump1090 method)
		me := data[4:]

		// Extract heading (bits 15-24 of ME)
		if app.getBits(me, 14, 14) != 0 {
			track = float64(app.getBitsUint16(me, 15, 24)) * 360.0 / 1024.0
		}

		// Extract airspeed (bits 26-35 of ME)
		airspeedRaw := app.getBitsUint16(me, 26, 35)
		if airspeedRaw != 0 {
			airspeed := int(airspeedRaw-1) * (1 << (subtype - 3)) // subtype 3: *1, subtype 4: *4

			// For airspeed messages, we don't get ground speed directly
			// But we can use airspeed as an approximation
			groundSpeed = airspeed

			if app.verbose {
				app.logger.Debugf("Airspeed data: airspeed=%d, heading=%.1f", airspeed, track)
				if groundSpeed > 0 {
					app.logger.Debugf("Using airspeed as ground speed: %d kt", groundSpeed)
				}
			}
		}
	}

	// Extract vertical rate (common for all subtypes) - dump1090 method
	me := data[4:]
	vrRaw := app.getBitsUint16(me, 38, 46) // bits 38-46 of ME

	var verticalRate int
	if vrRaw != 0 {
		verticalRate = int(vrRaw-1) * 64
		if app.getBits(me, 37, 37) != 0 { // sign bit 37
			verticalRate = -verticalRate
		}
	}

	if app.verbose {
		app.logger.Debugf("Velocity result: groundSpeed=%d, track=%.1f, verticalRate=%d", groundSpeed, track, verticalRate)
		if groundSpeed == 0 && track == 0 && verticalRate == 0 {
			app.logger.Debugf("All velocity values are zero - check message parsing")
		}
	}

	// Return data even if only partial information is available
	// For MSG,4 to be useful, we need at least speed, track, or vertical rate
	if groundSpeed > 0 || track > 0 || verticalRate != 0 {
		return groundSpeed, track, verticalRate
	}

	// Return partial data even if all values are zero, to help with debugging
	return groundSpeed, track, verticalRate
}

// extractPosition extracts latitude and longitude from position messages
func (app *Application) extractPosition(data []byte) (float64, float64) {
	if len(data) < 11 {
		return 0, 0
	}

	icao := app.extractICAO(data)

	// Extract F flag (odd/even)
	fFlag := (data[6] >> 2) & 0x01

	// Extract CPR latitude (17 bits)
	cprLatRaw := ((uint32(data[6]&0x03) << 15) | (uint32(data[7]) << 7) | (uint32(data[8]) >> 1)) & 0x1FFFF

	// Extract CPR longitude (17 bits)
	cprLonRaw := ((uint32(data[8]&0x01) << 16) | (uint32(data[9]) << 8) | uint32(data[10])) & 0x1FFFF

	if app.verbose {
		app.logger.Debugf("CPR position data: ICAO=%06X, F=%d, lat_cpr=%d (%.6f), lon_cpr=%d (%.6f)",
			icao, fFlag, cprLatRaw, float64(cprLatRaw)/CPR_LAT_MAX, cprLonRaw, float64(cprLonRaw)/CPR_LON_MAX)
	}

	// Use CPR decoder to get actual coordinates
	return app.decodeCPRPosition(icao, fFlag, cprLatRaw, cprLonRaw)
}

// extractICAO extracts the ICAO address from the message
func (app *Application) extractICAO(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return (uint32(data[1]) << 16) | (uint32(data[2]) << 8) | uint32(data[3])
}

// decodeCPRPosition decodes CPR coordinates to actual lat/lon using proper CPR algorithm
func (app *Application) decodeCPRPosition(icao uint32, fFlag uint8, latCPR, lonCPR uint32) (float64, float64) {
	now := time.Now()

	// Get or create aircraft position tracking
	app.positionMutex.Lock()
	aircraft, exists := app.aircraftPositions[icao]
	if !exists {
		aircraft = &AircraftPosition{
			ICAO:       icao,
			LastUpdate: now,
		}
		app.aircraftPositions[icao] = aircraft
	}
	app.positionMutex.Unlock()

	// Store the new frame
	newFrame := &CPRFrame{
		LatCPR:    latCPR,
		LonCPR:    lonCPR,
		FFlag:     fFlag,
		Timestamp: now,
	}

	if fFlag == 0 {
		aircraft.EvenFrame = newFrame
	} else {
		aircraft.OddFrame = newFrame
	}

	// Try to decode using both frames if available
	if aircraft.EvenFrame != nil && aircraft.OddFrame != nil {
		// Both frames available - use proper CPR decoding
		lat, lon := app.decodeCPRBothFrames(aircraft.EvenFrame, aircraft.OddFrame)
		if lat != 0 || lon != 0 {
			aircraft.LastPos = &Position{
				Latitude:  lat,
				Longitude: lon,
				Timestamp: now,
			}
			aircraft.LastUpdate = now

			if app.verbose {
				app.logger.Debugf("CPR decode: ICAO=%06X, both frames, lat=%.6f, lon=%.6f", icao, lat, lon)
			}
			return lat, lon
		}
	}

	// Single frame decoding (less accurate)
	lat, lon := app.decodeCPRSingleFrame(newFrame)
	if lat != 0 || lon != 0 {
		aircraft.LastPos = &Position{
			Latitude:  lat,
			Longitude: lon,
			Timestamp: now,
		}
		aircraft.LastUpdate = now

		if app.verbose {
			app.logger.Debugf("CPR decode: ICAO=%06X, single frame, lat=%.6f, lon=%.6f", icao, lat, lon)
		}
		return lat, lon
	}

	// Use last known position if available and recent
	if aircraft.LastPos != nil && now.Sub(aircraft.LastPos.Timestamp) < 30*time.Second {
		if app.verbose {
			app.logger.Debugf("CPR decode: ICAO=%06X, using last position, lat=%.6f, lon=%.6f", icao, aircraft.LastPos.Latitude, aircraft.LastPos.Longitude)
		}
		return aircraft.LastPos.Latitude, aircraft.LastPos.Longitude
	}

	return 0, 0
}

// cprModInt performs always positive MOD operation (dump1090 style)
func cprModInt(a, b int) int {
	res := a % b
	if res < 0 {
		res += b
	}
	return res
}

// decodeCPRBothFrames decodes position using both even and odd frames (dump1090 algorithm)
func (app *Application) decodeCPRBothFrames(evenFrame, oddFrame *CPRFrame) (float64, float64) {
	// Use dump1090's exact CPR algorithm
	const CPR_MAX = 131072.0 // 2^17

	AirDlat0 := 360.0 / 60.0 // 6.0 degrees for even frame
	AirDlat1 := 360.0 / 59.0 // ~6.101 degrees for odd frame

	lat0 := float64(evenFrame.LatCPR)
	lat1 := float64(oddFrame.LatCPR)
	lon0 := float64(evenFrame.LonCPR)
	lon1 := float64(oddFrame.LonCPR)

	// Compute the Latitude Index "j" (dump1090 method)
	j := int(math.Floor(((59*lat0 - 60*lat1) / CPR_MAX) + 0.5))

	rlat0 := AirDlat0 * (float64(cprModInt(j, 60)) + lat0/CPR_MAX)
	rlat1 := AirDlat1 * (float64(cprModInt(j, 59)) + lat1/CPR_MAX)

	// Normalize latitudes (dump1090 method)
	if rlat0 >= 270 {
		rlat0 -= 360
	}
	if rlat1 >= 270 {
		rlat1 -= 360
	}

	// Check to see that the latitude is in range: -90 .. +90
	if rlat0 < -90 || rlat0 > 90 || rlat1 < -90 || rlat1 > 90 {
		if app.verbose {
			app.logger.Debugf("CPR: bad latitude data, rlat0=%.6f, rlat1=%.6f", rlat0, rlat1)
		}
		return 0, 0 // bad data
	}

	// Check that both are in the same latitude zone, or abort
	if app.cprNLTable(rlat0) != app.cprNLTable(rlat1) {
		if app.verbose {
			app.logger.Debugf("CPR: positions crossed latitude zone, nl0=%d, nl1=%d", app.cprNLTable(rlat0), app.cprNLTable(rlat1))
		}
		return 0, 0 // positions crossed a latitude zone, try again later
	}

	// Determine which frame to use (use most recent)
	var rlat, rlon float64

	if oddFrame.Timestamp.After(evenFrame.Timestamp) {
		// Use odd packet
		ni := app.cprNFunction(rlat1, 1)
		m := int(math.Floor((((lon0 * float64(app.cprNLTable(rlat1)-1)) -
			(lon1 * float64(app.cprNLTable(rlat1)))) / CPR_MAX) + 0.5))
		rlon = app.cprDlonFunction(rlat1, 1) * (float64(cprModInt(m, ni)) + lon1/CPR_MAX)
		rlat = rlat1
	} else {
		// Use even packet
		ni := app.cprNFunction(rlat0, 0)
		m := int(math.Floor((((lon0 * float64(app.cprNLTable(rlat0)-1)) -
			(lon1 * float64(app.cprNLTable(rlat0)))) / CPR_MAX) + 0.5))
		rlon = app.cprDlonFunction(rlat0, 0) * (float64(cprModInt(m, ni)) + lon0/CPR_MAX)
		rlat = rlat0
	}

	// Renormalize longitude to -180 .. +180 (dump1090 method)
	rlon -= math.Floor((rlon+180)/360) * 360

	if app.verbose {
		app.logger.Debugf("Both frames CPR: lat=%.6f, lon=%.6f, j=%d", rlat, rlon, j)
	}

	return rlat, rlon
}

// cprNFunction returns the number of longitude zones (dump1090 style)
func (app *Application) cprNFunction(lat float64, fflag int) int {
	nl := app.cprNLTable(lat) - fflag
	if nl < 1 {
		nl = 1
	}
	return nl
}

// cprDlonFunction returns longitude zone width (dump1090 style)
func (app *Application) cprDlonFunction(lat float64, fflag int) float64 {
	return 360.0 / float64(app.cprNFunction(lat, fflag))
}

// decodeCPRSingleFrame decodes position using a single frame (less accurate, requires reference position)
func (app *Application) decodeCPRSingleFrame(frame *CPRFrame) (float64, float64) {
	// For single frame decoding, we need a reference position
	// Use a reasonable default for Brazil region: São Paulo area
	refLat := -23.5505 // São Paulo latitude
	refLon := -46.6333 // São Paulo longitude

	// Try to use a more recent known position if available
	app.positionMutex.Lock()
	for _, aircraft := range app.aircraftPositions {
		if aircraft.LastPos != nil && time.Since(aircraft.LastPos.Timestamp) < 5*time.Minute {
			refLat = aircraft.LastPos.Latitude
			refLon = aircraft.LastPos.Longitude
			break
		}
	}
	app.positionMutex.Unlock()

	const CPR_MAX = 131072.0 // 2^17

	// Use dump1090's single-frame algorithm with reference position
	lat := float64(frame.LatCPR)
	lon := float64(frame.LonCPR)

	// Calculate latitude zones
	AirDlat := 360.0 / 60.0
	if frame.FFlag == 1 {
		AirDlat = 360.0 / 59.0
	}

	// Calculate longitude zones
	j := int(math.Floor(refLat/AirDlat + 0.5))
	rlat := AirDlat * (float64(j) + lat/CPR_MAX)

	// Check if we need to adjust the latitude zone
	if (rlat - refLat) > (AirDlat / 2.0) {
		rlat -= AirDlat
	} else if (rlat - refLat) < -(AirDlat / 2.0) {
		rlat += AirDlat
	}

	// Calculate longitude
	ni := app.cprNFunction(rlat, int(frame.FFlag))
	if ni <= 0 {
		ni = 1
	}

	dlon := 360.0 / float64(ni)
	m := int(math.Floor(refLon/dlon + 0.5))
	rlon := dlon * (float64(m) + lon/CPR_MAX)

	// Check if we need to adjust the longitude zone
	if (rlon - refLon) > (dlon / 2.0) {
		rlon -= dlon
	} else if (rlon - refLon) < -(dlon / 2.0) {
		rlon += dlon
	}

	// Normalize longitude to -180 .. +180
	rlon -= math.Floor((rlon+180)/360) * 360

	// Validate the result
	if rlat < -90 || rlat > 90 {
		if app.verbose {
			app.logger.Debugf("Single frame CPR: invalid latitude %.6f", rlat)
		}
		return 0, 0
	}

	if app.verbose {
		app.logger.Debugf("Single frame CPR: lat=%.6f, lon=%.6f (ref: %.6f, %.6f)", rlat, rlon, refLat, refLon)
	}

	return rlat, rlon
}

// cprNLTable returns the number of longitude zones for a given latitude using lookup table
func (app *Application) cprNLTable(lat float64) int {
	// NL lookup table based on latitude (more accurate than calculation)
	absLat := math.Abs(lat)

	if absLat < 10.47047130 {
		return 59
	}
	if absLat < 14.82817437 {
		return 58
	}
	if absLat < 18.18626357 {
		return 57
	}
	if absLat < 21.02939493 {
		return 56
	}
	if absLat < 23.54504487 {
		return 55
	}
	if absLat < 25.82924707 {
		return 54
	}
	if absLat < 27.93898710 {
		return 53
	}
	if absLat < 29.91135686 {
		return 52
	}
	if absLat < 31.77209708 {
		return 51
	}
	if absLat < 33.53993436 {
		return 50
	}
	if absLat < 35.22899598 {
		return 49
	}
	if absLat < 36.85025108 {
		return 48
	}
	if absLat < 38.41241892 {
		return 47
	}
	if absLat < 39.92256684 {
		return 46
	}
	if absLat < 41.38651832 {
		return 45
	}
	if absLat < 42.80914012 {
		return 44
	}
	if absLat < 44.19454951 {
		return 43
	}
	if absLat < 45.54626723 {
		return 42
	}
	if absLat < 46.86733252 {
		return 41
	}
	if absLat < 48.16039128 {
		return 40
	}
	if absLat < 49.42776439 {
		return 39
	}
	if absLat < 50.67150166 {
		return 38
	}
	if absLat < 51.89342469 {
		return 37
	}
	if absLat < 53.09516153 {
		return 36
	}
	if absLat < 54.27817472 {
		return 35
	}
	if absLat < 55.44378444 {
		return 34
	}
	if absLat < 56.59318756 {
		return 33
	}
	if absLat < 57.72747354 {
		return 32
	}
	if absLat < 58.84763776 {
		return 31
	}
	if absLat < 59.95459277 {
		return 30
	}
	if absLat < 61.04917774 {
		return 29
	}
	if absLat < 62.13216659 {
		return 28
	}
	if absLat < 63.20427479 {
		return 27
	}
	if absLat < 64.26616523 {
		return 26
	}
	if absLat < 65.31845310 {
		return 25
	}
	if absLat < 66.36171008 {
		return 24
	}
	if absLat < 67.39646774 {
		return 23
	}
	if absLat < 68.42322022 {
		return 22
	}
	if absLat < 69.44242631 {
		return 21
	}
	if absLat < 70.45451075 {
		return 20
	}
	if absLat < 71.45986473 {
		return 19
	}
	if absLat < 72.45884545 {
		return 18
	}
	if absLat < 73.45177442 {
		return 17
	}
	if absLat < 74.43893416 {
		return 16
	}
	if absLat < 75.42056257 {
		return 15
	}
	if absLat < 76.39684391 {
		return 14
	}
	if absLat < 77.36789461 {
		return 13
	}
	if absLat < 78.33374083 {
		return 12
	}
	if absLat < 79.29428225 {
		return 11
	}
	if absLat < 80.24923213 {
		return 10
	}
	if absLat < 81.19801349 {
		return 9
	}
	if absLat < 82.13956981 {
		return 8
	}
	if absLat < 83.07199445 {
		return 7
	}
	if absLat < 83.99173563 {
		return 6
	}
	if absLat < 84.89166191 {
		return 5
	}
	if absLat < 85.75541621 {
		return 4
	}
	if absLat < 86.53536998 {
		return 3
	}
	if absLat < 87.00000000 {
		return 2
	}
	return 1
}

// extractAlertSPI extracts Alert and SPI flags from surveillance messages
func (app *Application) extractAlertSPI(data []byte) (bool, bool) {
	if len(data) < 2 {
		return false, false
	}

	df := (data[0] >> 3) & 0x1F

	// Alert and SPI are only available in surveillance messages (DF4, DF5, DF20, DF21)
	if df != 4 && df != 5 && df != 20 && df != 21 {
		return false, false
	}

	// Extract FS (Flight Status) field - bits 6-8
	fs := (data[0] >> 3) & 0x07

	var alert, spi bool

	switch fs {
	case 2, 3: // Alert conditions
		alert = true
	case 4: // Alert + SPI condition
		alert = true
		spi = true
	case 5: // SPI condition
		spi = true
	}

	return alert, spi
}

// extractEmergency extracts emergency status from ADS-B messages
func (app *Application) extractEmergency(data []byte) string {
	if len(data) < 11 {
		return ""
	}

	df := (data[0] >> 3) & 0x1F
	if df != 17 && df != 18 {
		return ""
	}

	typeCode := (data[4] >> 3) & 0x1F

	// Emergency status is in aircraft status messages (type code 28)
	if typeCode == 28 {
		// Extract emergency state from ME field
		subtype := data[4] & 0x07
		if subtype == 1 {
			// Emergency/priority status
			emergencyState := (data[5] >> 5) & 0x07
			switch emergencyState {
			case 1:
				return "general"
			case 2:
				return "lifeguard"
			case 3:
				return "minfuel"
			case 4:
				return "nordo"
			case 5:
				return "unlawful"
			case 6:
				return "downed"
			default:
				return "reserved"
			}
		}
	}

	return ""
}

// extractGroundState extracts ground/airborne state with improved accuracy
func (app *Application) extractGroundState(data []byte) string {
	if len(data) < 5 {
		return "0" // Default to airborne
	}

	df := (data[0] >> 3) & 0x1F

	// For surveillance messages, check VS and FS bits
	if df == 4 || df == 5 || df == 20 || df == 21 {
		// VS (Vertical Status) bit - bit 6
		vs := (data[0] >> 2) & 0x01
		if vs == 1 {
			return "1" // On ground
		}

		// Also check FS (Flight Status)
		fs := (data[0] >> 3) & 0x07
		if fs == 1 || fs == 3 {
			return "1" // On ground
		}
	}

	// For extended squitter messages
	if df == 17 || df == 18 {
		typeCode := (data[4] >> 3) & 0x1F

		// Surface position messages (type codes 5-8)
		if typeCode >= 5 && typeCode <= 8 {
			return "1" // On ground
		}

		// Check CA (Capability) field for DF17
		if df == 17 {
			ca := data[0] & 0x07
			if ca == 4 {
				return "1" // Ground vehicle
			} else if ca == 5 {
				return "0" // Airborne
			}
		}
	}

	return "0" // Default to airborne
}

// reportStatistics reports processing statistics periodically
func (app *Application) reportStatistics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			total, preambles, valid, corrected, singleBit, twoBit := app.adsbProcessor.GetStats()
			app.logger.WithFields(logrus.Fields{
				"total_processed":    total,
				"preambles_found":    preambles,
				"valid_messages":     valid,
				"corrected_messages": corrected,
				"single_bit_errors":  singleBit,
				"two_bit_errors":     twoBit,
				"success_rate":       fmt.Sprintf("%.2f%%", float64(valid)/float64(preambles)*100),
			}).Info("Enhanced ADS-B processing statistics (dump1090-style)")
		}
	}
}

// shutdown gracefully shuts down the application
func (app *Application) shutdown() {
	app.logger.Info("Shutting down application")
	app.cancel()

	done := make(chan struct{})
	go func() {
		app.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		app.logger.Info("All goroutines finished")
	case <-time.After(5 * time.Second):
		app.logger.Warn("Shutdown timeout, forcing exit")
	}

	// Cleanup resources
	if app.rtlsdr != nil {
		app.rtlsdr.Close()
	}
	if app.logRotator != nil {
		app.logRotator.Close()
	}

	app.logger.Info("Shutdown completed")
}

// showVersion displays version information
func showVersion() {
	fmt.Printf("Go1090 ADS-B Decoder (dump1090-style)\n")
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Build Time: %s\n", BuildTime)
	fmt.Printf("Git Commit: %s\n", GitCommit)
}

// CLI setup
func main() {
	var config Config

	rootCmd := &cobra.Command{
		Use:   "go1090",
		Short: "ADS-B Decoder (dump1090-style)",
		Long: `ADS-B Decoder using RTL-SDR (dump1090-style implementation).

Captures I/Q samples from RTL-SDR at 2.4MHz, demodulates ADS-B messages using 
dump1090's correlation-based approach with proper phase tracking and scoring,
validates CRC, and outputs in BaseStation (SBS) format.

Example usage:
  go1090 --frequency 1090000000 --sample-rate 2400000 --gain 40 --device 0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if config.ShowVersion {
				showVersion()
				return nil
			}

			app := NewApplication(config)
			return app.Start()
		},
	}

	rootCmd.Flags().Uint32VarP(&config.Frequency, "frequency", "f", DefaultFrequency, "Frequency to tune to (Hz)")
	rootCmd.Flags().Uint32VarP(&config.SampleRate, "sample-rate", "s", DefaultSampleRate, "Sample rate (Hz)")
	rootCmd.Flags().IntVarP(&config.Gain, "gain", "g", DefaultGain, "Gain setting (0 for auto)")
	rootCmd.Flags().IntVarP(&config.DeviceIndex, "device", "d", 0, "RTL-SDR device index")
	rootCmd.Flags().StringVarP(&config.LogDir, "log-dir", "l", "./logs", "Log directory")
	rootCmd.Flags().BoolVarP(&config.LogRotateUTC, "utc", "u", true, "Use UTC for log rotation")
	rootCmd.Flags().BoolVarP(&config.Verbose, "verbose", "v", false, "Verbose logging")
	rootCmd.Flags().BoolVar(&config.ShowVersion, "version", false, "Show version information")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
