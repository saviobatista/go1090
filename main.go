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

const (
	DefaultFrequency  = 1090000000 // 1090 MHz
	DefaultSampleRate = 2400000    // 2.4 MHz (same as dump1090)
	DefaultGain       = 40         // Manual gain
)

// Version information (set by build flags)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

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
		config:  config,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		verbose: config.Verbose,
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
	for i := 0; i < len(data)-1; i += 2 {
		iSample := float64(data[i]) - 127.5
		qSample := float64(data[i+1]) - 127.5
		samples[i/2] = complex(iSample, qSample)
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

		case typeCode >= 9 && typeCode <= 18:
			// Airborne position
			transmissionType = "3"
			if alt := app.extractAltitude(msg.Data[:]); alt != 0 {
				altitude = fmt.Sprintf("%d", alt)
			}
			// Position decoding would go here (requires CPR)

		case typeCode >= 19 && typeCode <= 22:
			// Airborne velocity
			transmissionType = "4"
			if speed, trk, vrate := app.extractVelocity(msg.Data[:]); speed != 0 {
				groundSpeed = fmt.Sprintf("%d", speed)
				track = fmt.Sprintf("%.1f", trk)
				if vrate != 0 {
					verticalRate = fmt.Sprintf("%d", vrate)
				}
			}
		}

		return fmt.Sprintf("MSG,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s",
			transmissionType, sessionID, aircraftID, icao, flightID,
			dateStr, timeStr, dateStr, timeStr,
			callsign, altitude, groundSpeed, track, latitude, longitude,
			verticalRate, squawk, alert, emergency, spi, isOnGround)

	case 4, 5, 20, 21: // Surveillance replies
		transmissionType := "5" // Surveillance

		altitude := ""
		squawk := ""

		if df == 4 || df == 20 {
			if alt := app.extractAltitude(msg.Data[:]); alt != 0 {
				altitude = fmt.Sprintf("%d", alt)
			}
		}

		if df == 5 || df == 21 {
			if sq := app.extractSquawk(msg.Data[:]); sq != 0 {
				squawk = fmt.Sprintf("%04d", sq)
			}
		}

		return fmt.Sprintf("MSG,%s,%s,%s,%s,%s,%s,%s,%s,%s,,%s,,,,,%s,,,,%s",
			transmissionType, sessionID, aircraftID, icao, flightID,
			dateStr, timeStr, dateStr, timeStr,
			altitude, squawk, "0")
	}

	return "" // Unsupported message type
}

// extractCallsign extracts callsign from aircraft identification message
func (app *Application) extractCallsign(data []byte) string {
	if len(data) < 11 {
		return ""
	}

	// Debug: print the raw data for analysis
	if app.verbose {
		app.logger.Debugf("Callsign raw data: %x", data[:11])
	}

	// Callsign is in bits 40-87 (8 characters, 6 bits each)
	// ADS-B uses a specific 6-bit character set: space, A-Z, 0-9
	charset := "@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_ !\"#$%&'()*+,-./0123456789:;<=>?"
	callsign := make([]byte, 8)

	for i := 0; i < 8; i++ {
		// Calculate bit position: start at bit 40, each character is 6 bits
		bitStart := 40 + i*6
		byteIdx := bitStart / 8
		bitOffset := bitStart % 8

		if byteIdx >= len(data) {
			break
		}

		var char uint8
		if bitOffset <= 2 {
			// Character fits in single byte
			char = (data[byteIdx] >> (2 - bitOffset)) & 0x3F
		} else {
			// Character spans two bytes
			if byteIdx+1 >= len(data) {
				break
			}
			char = ((data[byteIdx] << (bitOffset - 2)) | (data[byteIdx+1] >> (10 - bitOffset))) & 0x3F
		}

		// Debug individual characters
		if app.verbose {
			app.logger.Debugf("Char %d: raw=0x%02x (%d)", i, char, char)
		}

		// Convert using ADS-B 6-bit character set
		if char < uint8(len(charset)) {
			callsign[i] = charset[char]
		} else {
			callsign[i] = '?'
		}
	}

	result := strings.TrimSpace(string(callsign))
	if app.verbose {
		app.logger.Debugf("Extracted callsign: '%s'", result)
	}
	return result
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
		// Surveillance altitude reply
		altCode = (uint16(data[2]&0x1F) << 8) | uint16(data[3])
	} else if df == 17 || df == 18 {
		// Extended squitter
		altCode = (uint16(data[5]&0x1F) << 8) | uint16(data[6])
	} else {
		return 0
	}

	if altCode == 0 {
		return 0
	}

	// Convert from Gray code to binary and calculate altitude
	return int(altCode)*25 - 1000
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
	squawk += int((identity>>9)&0x07) * 1000 // A4 A2 A1
	squawk += int((identity>>6)&0x07) * 100  // B4 B2 B1
	squawk += int((identity>>3)&0x07) * 10   // C4 C2 C1
	squawk += int(identity & 0x07)           // D4 D2 D1

	return squawk
}

// extractVelocity extracts velocity information from airborne velocity messages
func (app *Application) extractVelocity(data []byte) (int, float64, int) {
	if len(data) < 11 {
		return 0, 0, 0
	}

	// Extract velocity subtype
	subtype := (data[4] >> 1) & 0x07

	if subtype != 1 && subtype != 2 {
		return 0, 0, 0 // Only handle groundspeed subtypes
	}

	// Extract east-west velocity
	ewDir := (data[5] >> 2) & 0x01
	ewVel := ((uint16(data[5]&0x03) << 8) | uint16(data[6])) - 1

	// Extract north-south velocity
	nsDir := (data[7] >> 7) & 0x01
	nsVel := (((uint16(data[7]&0x7F) << 3) | (uint16(data[8]) >> 5)) & 0x3FF) - 1

	// Convert to signed values
	var ewSpeed, nsSpeed float64
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
	groundSpeed := int(math.Sqrt(ewSpeed*ewSpeed + nsSpeed*nsSpeed))
	track := math.Atan2(ewSpeed, nsSpeed) * 180.0 / math.Pi
	if track < 0 {
		track += 360
	}

	// Extract vertical rate
	vrSign := (data[8] >> 3) & 0x01
	vrValue := ((uint16(data[8]&0x07) << 6) | (uint16(data[9]) >> 2)) & 0x1FF

	var verticalRate int
	if vrValue != 0 {
		verticalRate = int(vrValue-1) * 64
		if vrSign == 1 {
			verticalRate = -verticalRate
		}
	}

	return groundSpeed, track, verticalRate
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
			total, preambles, valid := app.adsbProcessor.GetStats()
			app.logger.WithFields(logrus.Fields{
				"total_processed": total,
				"preambles_found": preambles,
				"valid_messages":  valid,
			}).Info("ADS-B processing statistics")
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
