package main

import (
	"context"
	"fmt"
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
	DefaultGain       = 40         // Auto gain
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
	config      Config
	logger      *logrus.Logger
	rtlsdr      *RTLSDRDevice
	decoder     *BeastDecoder
	baseStation *BaseStationWriter
	logRotator  *LogRotator
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	restartChan chan struct{}
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
		config:      config,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		restartChan: make(chan struct{}, 1),
	}
}

// Start starts the application
func (app *Application) Start() error {
	app.logger.WithFields(logrus.Fields{
		"version":    Version,
		"build_time": BuildTime,
		"git_commit": GitCommit,
	}).Info("Starting ADS-B Beast Mode Decoder")

	// Initialize components
	if err := app.initializeComponents(); err != nil {
		return fmt.Errorf("failed to initialize components: %w", err)
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Main loop with restart capability
	for {
		select {
		case <-sigChan:
			app.logger.Info("Received shutdown signal")
			app.shutdown()
			return nil
		case <-app.restartChan:
			app.logger.Info("Restarting application due to error")
			app.restart()
		default:
			if err := app.run(); err != nil {
				app.logger.WithError(err).Error("Application error, scheduling restart")
				time.Sleep(5 * time.Second) // Wait before restart
				app.triggerRestart()
			}
		}
	}
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
	app.decoder = NewBeastDecoder(app.logger)

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
	app.logger.Info("Starting data capture")

	// Start RTL-SDR data capture
	dataChan := make(chan []byte, 1000)

	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		if err := app.rtlsdr.StartCapture(app.ctx, dataChan); err != nil {
			app.logger.WithError(err).Error("RTL-SDR capture failed")
			app.triggerRestart()
		}
	}()

	// Start log rotation
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		app.logRotator.Start(app.ctx)
	}()

	// Process data
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		app.processData(dataChan)
	}()

	// Wait for context cancellation
	<-app.ctx.Done()
	app.wg.Wait()

	return nil
}

// processData processes incoming data from RTL-SDR
func (app *Application) processData(dataChan <-chan []byte) {
	for {
		select {
		case <-app.ctx.Done():
			return
		case data := <-dataChan:
			if data == nil {
				continue
			}

			// Decode beast messages
			messages, err := app.decoder.Decode(data)
			if err != nil {
				app.logger.WithError(err).Debug("Failed to decode beast messages")
				continue
			}

			// Write to BaseStation format
			for _, msg := range messages {
				if err := app.baseStation.WriteMessage(msg); err != nil {
					app.logger.WithError(err).Error("Failed to write BaseStation message")
				}
			}
		}
	}
}

// triggerRestart triggers an application restart
func (app *Application) triggerRestart() {
	select {
	case app.restartChan <- struct{}{}:
	default:
		// Channel is full, restart already pending
	}
}

// restart restarts the application
func (app *Application) restart() {
	app.logger.Info("Restarting application components")

	// Cancel current context
	app.cancel()
	app.wg.Wait()

	// Cleanup
	app.cleanup()

	// Create new context
	app.ctx, app.cancel = context.WithCancel(context.Background())

	// Reinitialize components
	if err := app.initializeComponents(); err != nil {
		app.logger.WithError(err).Error("Failed to reinitialize components")
		time.Sleep(10 * time.Second)
		app.triggerRestart()
	}
}

// cleanup cleans up resources
func (app *Application) cleanup() {
	if app.rtlsdr != nil {
		app.rtlsdr.Close()
	}
	if app.logRotator != nil {
		app.logRotator.Close()
	}
}

// shutdown gracefully shuts down the application
func (app *Application) shutdown() {
	app.logger.Info("Shutting down application")
	app.cancel()
	app.wg.Wait()
	app.cleanup()
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
		Long:  "Captures ADS-B messages from RTL-SDR and logs them in BaseStation format",
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
	rootCmd.Flags().IntVarP(&config.Gain, "gain", "g", DefaultGain, "Gain setting")
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
