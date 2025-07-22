package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"go1090/internal/adsb"
	"go1090/internal/basestation"
	"go1090/internal/logging"
	"go1090/internal/rtlsdr"
)

// Application represents the main application
type Application struct {
	config        Config
	logger        *logrus.Logger
	rtlsdr        *rtlsdr.RTLSDRDevice
	adsbProcessor *adsb.ADSBProcessor
	baseStation   *basestation.Writer
	logRotator    *logging.LogRotator
	cprDecoder    *adsb.CPRDecoder
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	verbose       bool

	// Aircraft position tracking for CPR decoding
	aircraftPositions map[uint32]*adsb.AircraftPosition
	positionMutex     sync.RWMutex
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
		aircraftPositions: make(map[uint32]*adsb.AircraftPosition),
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
	app.rtlsdr, err = rtlsdr.NewRTLSDRDevice(app.config.DeviceIndex)
	if err != nil {
		return fmt.Errorf("failed to initialize RTL-SDR: %w", err)
	}

	// Configure RTL-SDR
	if err := app.rtlsdr.Configure(app.config.Frequency, app.config.SampleRate, app.config.Gain); err != nil {
		return fmt.Errorf("failed to configure RTL-SDR: %w", err)
	}

	// Initialize ADS-B processor
	app.adsbProcessor = adsb.NewADSBProcessor(app.config.SampleRate, app.logger)

	// Initialize CPR decoder
	app.cprDecoder = adsb.NewCPRDecoder(app.logger, app.verbose)

	// Initialize log rotator
	app.logRotator, err = logging.NewLogRotator(app.config.LogDir, app.config.LogRotateUTC, app.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize log rotator: %w", err)
	}

	// Initialize BaseStation writer
	app.baseStation = basestation.NewWriter(app.logRotator, app.logger)

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
			iqSamples := app.bytesToIQ(data)

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

// Helper: Convert raw bytes to complex128 I/Q samples (unsigned 8-bit to signed)
func (app *Application) bytesToIQ(data []byte) []complex128 {
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

// writeADSBMessage converts ADS-B message to SBS format and writes it
func (app *Application) writeADSBMessage(msg *adsb.ADSBMessage) error {
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
func (app *Application) convertToSBS(msg *adsb.ADSBMessage) string {
	now := time.Now().UTC()
	dateStr := now.Format("2006/01/02")
	timeStr := now.Format("15:04:05.000")

	icao := fmt.Sprintf("%06X", msg.GetICAO())
	df := msg.GetDF()

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
		alert := ""
		emergency := ""
		spi := ""
		isOnGround := app.extractGroundState(msg.Data[:])

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

		return fmt.Sprintf("MSG,%s,%s,%s,%s,%s,%s,%s,%s,%s,,%s,,,,,%s,%s,%s,%s,%s",
			transmissionType, sessionID, aircraftID, icao, flightID,
			dateStr, timeStr, dateStr, timeStr,
			altitude, squawk, alert, emergency, spi, isOnGround)
	}

	return "" // Unsupported message type
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
