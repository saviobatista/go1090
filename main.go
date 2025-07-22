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
		isOnGround := "0"

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

		return fmt.Sprintf("MSG,%s,%s,%s,%s,%s,%s,%s,%s,%s,,%s,,,,,%s,,,,%s",
			transmissionType, sessionID, aircraftID, icao, flightID,
			dateStr, timeStr, dateStr, timeStr,
			altitude, squawk, "0")

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

	// For even more complex cases
	var result uint32
	for i := fby; i <= lby && i < len(data); i++ {
		if i == fby {
			result = uint32(data[i] & topMask)
		} else {
			result = (result << 8) | uint32(data[i])
		}
	}
	return uint8(result >> shift)
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
		// Extended squitter - altitude is in bits 40-52 (bytes 5-6)
		// Extract 13 bits: 5 bits from byte 5, 8 bits from byte 6
		altCode = (uint16(data[5]&0x1F) << 8) | uint16(data[6])
	} else {
		return 0
	}

	if altCode == 0 {
		return 0
	}

	// Mode S altitude encoding (not Gray code for ADS-B)
	// Remove the Q bit (bit 4) and decode
	qBit := (altCode >> 4) & 0x01

	if qBit == 1 {
		// 25-foot resolution
		altValue := ((altCode & 0x1FE0) >> 1) | (altCode & 0x0F)
		return int(altValue)*25 - 1000
	} else {
		// 100-foot resolution (Gray code)
		// This is more complex, for now return simple calculation
		return int(altCode)*25 - 1000
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

	if subtype != 1 && subtype != 2 && subtype != 3 && subtype != 4 {
		if app.verbose {
			app.logger.Debugf("Velocity extraction failed: unsupported subtype %d", subtype)
		}
		return 0, 0, 0 // Only handle groundspeed and airspeed subtypes
	}

	var ewSpeed, nsSpeed float64
	var groundSpeed int
	var track float64

	if subtype == 1 || subtype == 2 {
		// Ground speed subtypes
		// Extract east-west velocity
		ewDir := (data[5] >> 2) & 0x01
		ewVel := ((uint16(data[5]&0x03) << 8) | uint16(data[6])) - 1

		// Extract north-south velocity
		nsDir := (data[7] >> 7) & 0x01
		nsVel := (((uint16(data[7]&0x7F) << 3) | (uint16(data[8]) >> 5)) & 0x3FF) - 1

		if app.verbose {
			app.logger.Debugf("Ground speed components: ewDir=%d, ewVel=%d, nsDir=%d, nsVel=%d", ewDir, ewVel, nsDir, nsVel)
		}

		// Convert to signed values
		if ewDir == 1 {
			ewSpeed = -float64(ewVel)
		} else {
			ewSpeed = float64(ewVel)
		}

		if nsDir == 1 {
			nsSpeed = -float64(nsVel)
		} else {
			nsSpeed = float64(nsVel)
		}

		// Calculate ground speed and track
		groundSpeed = int(math.Sqrt(ewSpeed*ewSpeed + nsSpeed*nsSpeed))
		track = math.Atan2(ewSpeed, nsSpeed) * 180.0 / math.Pi
		if track < 0 {
			track += 360
		}

		// Only return valid data if we have reasonable values
		if groundSpeed > 0 && groundSpeed < 1000 {
			if app.verbose {
				app.logger.Debugf("Valid ground speed: %d kt, track: %.1f°", groundSpeed, track)
			}
		} else {
			groundSpeed = 0
			track = 0
		}

	} else if subtype == 3 || subtype == 4 {
		// Airspeed subtypes
		// Extract airspeed
		airspeedAvail := (data[5] >> 2) & 0x01
		airspeed := ((uint16(data[5]&0x03) << 8) | uint16(data[6])) - 1

		// Extract heading
		headingAvail := (data[7] >> 2) & 0x01
		heading := float64(((uint16(data[7]&0x03)<<8)|uint16(data[8]))*360) / 1024.0

		if app.verbose {
			app.logger.Debugf("Airspeed data: airspeedAvail=%d, airspeed=%d, headingAvail=%d, heading=%.1f",
				airspeedAvail, airspeed, headingAvail, heading)
		}

		// For airspeed subtypes, treat airspeed as ground speed approximation
		if airspeedAvail == 1 && airspeed > 0 && airspeed < 1000 {
			groundSpeed = int(airspeed)
			if app.verbose {
				app.logger.Debugf("Using airspeed as ground speed: %d kt", groundSpeed)
			}
		}
		if headingAvail == 1 {
			track = heading
		}
	}

	// Extract vertical rate (common for all subtypes)
	vrSign := (data[8] >> 3) & 0x01
	vrValue := ((uint16(data[8]&0x07) << 6) | (uint16(data[9]) >> 2)) & 0x1FF

	var verticalRate int
	if vrValue != 0 {
		verticalRate = int(vrValue-1) * 64
		if vrSign == 1 {
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

// decodeCPRBothFrames decodes position using both even and odd frames (most accurate)
func (app *Application) decodeCPRBothFrames(evenFrame, oddFrame *CPRFrame) (float64, float64) {
	// Extract normalized CPR values
	lat0 := float64(evenFrame.LatCPR) / CPR_LAT_MAX // Even frame latitude
	lat1 := float64(oddFrame.LatCPR) / CPR_LAT_MAX  // Odd frame latitude
	lon0 := float64(evenFrame.LonCPR) / CPR_LON_MAX // Even frame longitude
	lon1 := float64(oddFrame.LonCPR) / CPR_LON_MAX  // Odd frame longitude

	const nz = 15.0
	dlat0 := 360.0 / (4.0 * nz)     // 6.0 degrees for even frame
	dlat1 := 360.0 / (4.0*nz - 1.0) // ~6.101 degrees for odd frame

	// Calculate latitude index
	j := int(math.Floor(59*lat0 - 60*lat1 + 0.5))

	// Calculate latitude for both frames
	rlat0 := dlat0 * (float64(j%60) + lat0) // Even frame latitude
	rlat1 := dlat1 * (float64(j%59) + lat1) // Odd frame latitude

	// Use the most recent frame's latitude
	var lat float64
	if evenFrame.Timestamp.After(oddFrame.Timestamp) {
		lat = rlat0
	} else {
		lat = rlat1
	}

	// Check for valid latitude
	if lat < -90 || lat > 90 {
		// Try alternative zones
		for offset := -1; offset <= 1; offset++ {
			testJ := j + offset
			testLat0 := dlat0 * (float64(testJ%60) + lat0)
			testLat1 := dlat1 * (float64(testJ%59) + lat1)

			if testLat0 >= -90 && testLat0 <= 90 {
				lat = testLat0
				j = testJ
				break
			} else if testLat1 >= -90 && testLat1 <= 90 {
				lat = testLat1
				j = testJ
				break
			}
		}
	}

	// Calculate longitude zones
	nl0 := app.cprNLTable(rlat0) // NL for even frame
	nl1 := app.cprNLTable(rlat1) // NL for odd frame

	// Check if NL values are compatible
	if nl0 != nl1 {
		if app.verbose {
			app.logger.Debugf("CPR: NL mismatch, nl0=%d, nl1=%d, using single frame", nl0, nl1)
		}
		// Fall back to single frame decoding
		if evenFrame.Timestamp.After(oddFrame.Timestamp) {
			return app.decodeCPRSingleFrame(evenFrame)
		} else {
			return app.decodeCPRSingleFrame(oddFrame)
		}
	}

	nl := nl0
	if nl < 1 {
		return 0, 0
	}

	// Calculate longitude index
	m := int(math.Floor(float64(nl)*lon0 - float64(nl-1)*lon1 + 0.5))

	// Calculate longitude
	dlon0 := 360.0 / float64(nl)
	dlon1 := 360.0 / float64(nl-1)

	rlon0 := dlon0 * (float64(m%nl) + lon0)     // Even frame longitude
	rlon1 := dlon1 * (float64(m%(nl-1)) + lon1) // Odd frame longitude

	// Use the most recent frame's longitude
	var lon float64
	if evenFrame.Timestamp.After(oddFrame.Timestamp) {
		lon = rlon0
	} else {
		lon = rlon1
	}

	// Normalize longitude to -180 to +180
	for lon > 180 {
		lon -= 360
	}
	for lon < -180 {
		lon += 360
	}

	if app.verbose {
		app.logger.Debugf("Both frames CPR: lat=%.6f, lon=%.6f, j=%d, m=%d, nl=%d", lat, lon, j, m, nl)
	}

	return lat, lon
}

// decodeCPRSingleFrame decodes position using a single frame (less accurate)
func (app *Application) decodeCPRSingleFrame(frame *CPRFrame) (float64, float64) {
	latCPRf := float64(frame.LatCPR) / CPR_LAT_MAX
	lonCPRf := float64(frame.LonCPR) / CPR_LON_MAX

	const nz = 15.0
	dlat0 := 360.0 / (4.0 * nz)     // 6.0 degrees for even frame
	dlat1 := 360.0 / (4.0*nz - 1.0) // ~6.101 degrees for odd frame

	var lat float64
	if frame.FFlag == 0 {
		// Even frame
		lat = dlat0 * latCPRf
	} else {
		// Odd frame
		lat = dlat1 * latCPRf
	}

	// Calculate longitude zones based on latitude
	nl := app.cprNLTable(lat)
	var dlon float64

	if frame.FFlag == 0 {
		// Even frame
		if nl > 0 {
			dlon = 360.0 / float64(nl)
		} else {
			dlon = 360.0
		}
	} else {
		// Odd frame
		if nl > 1 {
			dlon = 360.0 / float64(nl-1)
		} else {
			dlon = 360.0
		}
	}

	lon := dlon * lonCPRf

	// Normalize longitude to -180 to +180
	if lon > 180 {
		lon -= 360
	}

	// For single frame, we need to make an educated guess about the zone
	// Try different zone combinations to find a reasonable position
	// Start with the most likely zones based on the raw coordinates

	// Calculate the most likely zone based on the raw position (for future use)
	_ = int(math.Floor(lat / dlat0))
	_ = int(math.Floor(lon / dlon))

	// Try the most likely zone first, then nearby zones
	zonesToTry := []int{-2, -1, 0, 1, 2}

	for _, latOffset := range zonesToTry {
		for _, lonOffset := range zonesToTry {
			testLat := lat + float64(latOffset)*dlat0
			testLon := lon + float64(lonOffset)*dlon

			// Normalize longitude
			if testLon > 180 {
				testLon -= 360
			} else if testLon < -180 {
				testLon += 360
			}

			// Check if this position is reasonable
			if testLat >= -90.0 && testLat <= 90.0 && testLon >= -180.0 && testLon <= 180.0 {
				// For Brazil region, prefer positions in the expected range
				if testLat >= -35.0 && testLat <= -5.0 && testLon >= -75.0 && testLon <= -30.0 {
					if app.verbose {
						app.logger.Debugf("Single frame CPR: found Brazil region position lat=%.6f, lon=%.6f", testLat, testLon)
					}
					return testLat, testLon
				}
			}
		}
	}

	// If no Brazil region position found, return the most reasonable position
	// Try to find any position that's not obviously wrong
	for _, latOffset := range zonesToTry {
		for _, lonOffset := range zonesToTry {
			testLat := lat + float64(latOffset)*dlat0
			testLon := lon + float64(lonOffset)*dlon

			// Normalize longitude
			if testLon > 180 {
				testLon -= 360
			} else if testLon < -180 {
				testLon += 360
			}

			// Accept any reasonable position
			if testLat >= -90.0 && testLat <= 90.0 && testLon >= -180.0 && testLon <= 180.0 {
				if app.verbose {
					app.logger.Debugf("Single frame CPR: using fallback position lat=%.6f, lon=%.6f", testLat, testLon)
				}
				return testLat, testLon
			}
		}
	}

	// Last resort: return original coordinates
	if app.verbose {
		app.logger.Debugf("Single frame CPR: using original coordinates lat=%.6f, lon=%.6f", lat, lon)
	}
	return lat, lon
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
