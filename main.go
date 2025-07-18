package main

import (
	"context"
	"fmt"
	"math/cmplx"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	DefaultFrequency  = 1090000000 // 1090 MHz
	DefaultSampleRate = 2000000    // 2 MHz
	DefaultGain       = 40         // Manual gain
	BeastSyncByte     = 0x1A       // Beast mode sync byte
)

// Version information (set by build flags)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// Application represents the main application
type Application struct {
	config       Config
	logger       *logrus.Logger
	rtlsdr       *RTLSDRDevice
	beastDecoder *BeastDecoder
	baseStation  *BaseStationWriter
	logRotator   *LogRotator
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
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
		config: config,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start starts the application
func (app *Application) Start() error {
	app.logger.WithFields(logrus.Fields{
		"version":    Version,
		"build_time": BuildTime,
		"git_commit": GitCommit,
	}).Info("Starting ADS-B Beast Mode Decoder with RTL-SDR")

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

	// Initialize Beast decoder
	app.beastDecoder = NewBeastDecoder(app.logger)

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

// Simple envelope detector for ADS-B demodulation
func demodulateADSB(samples []complex128) []byte {
	// Calculate envelope (magnitude)
	envelope := make([]float64, len(samples))
	maxEnv := 0.0
	for i, sample := range samples {
		envelope[i] = cmplx.Abs(sample)
		if envelope[i] > maxEnv {
			maxEnv = envelope[i]
		}
	}

	// Normalize and threshold
	if maxEnv == 0 {
		return nil
	}

	threshold := maxEnv * 0.4
	bits := make([]byte, 0, len(envelope))

	// Simple bit detection - this is a very basic approach
	for i := 0; i < len(envelope); i++ {
		if envelope[i] > threshold {
			bits = append(bits, 1)
		} else {
			bits = append(bits, 0)
		}
	}

	return bits
}

// processIQData processes incoming I/Q data from RTL-SDR
func (app *Application) processIQData(dataChan <-chan []byte) {
	messageCount := 0
	lastLogTime := time.Now()

	for {
		select {
		case <-app.ctx.Done():
			app.logger.Info("I/Q data processing stopped")
			return
		case data := <-dataChan:
			if data == nil {
				continue
			}

			// Convert raw bytes to I/Q samples
			iqSamples := bytesToIQ(data)

			// Simple ADS-B demodulation (this is very basic)
			demodData := demodulateADSB(iqSamples)
			if len(demodData) == 0 {
				continue
			}

			// Try to decode as Beast mode messages
			// For now, we'll treat the demodulated data as if it were Beast mode
			// In reality, you'd need proper ADS-B preamble detection and framing
			messages, err := app.beastDecoder.Decode(demodData)
			if err != nil {
				app.logger.WithError(err).Debug("Failed to decode as Beast data")
				continue
			}

			// Convert and write each message to SBS format
			for _, msg := range messages {
				if err := app.baseStation.WriteMessage(msg); err != nil {
					app.logger.WithError(err).Debug("Failed to write SBS message")
					continue
				}

				messageCount++

				// Log statistics every 30 seconds
				if time.Since(lastLogTime) >= 30*time.Second {
					app.logger.WithFields(logrus.Fields{
						"total_messages": messageCount,
						"message_type":   fmt.Sprintf("0x%02x", msg.MessageType),
						"signal":         msg.Signal,
					}).Info("ADS-B processing statistics")
					lastLogTime = time.Now()
				}
			}
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
	fmt.Printf("Go1090 ADS-B Beast Mode Decoder\n")
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Build Time: %s\n", BuildTime)
	fmt.Printf("Git Commit: %s\n", GitCommit)
}

// CLI setup
func main() {
	var config Config

	rootCmd := &cobra.Command{
		Use:   "go1090",
		Short: "ADS-B Beast Mode Decoder",
		Long: `ADS-B Beast Mode Decoder using RTL-SDR.

Captures I/Q samples from RTL-SDR, demodulates ADS-B messages,
and converts them to BaseStation (SBS) format.

Example usage:
  go1090 --frequency 1090000000 --gain 40 --device 0`,
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
