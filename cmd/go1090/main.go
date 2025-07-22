package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"go1090/internal/app"
)

func main() {
	var config app.Config

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
				app.ShowVersion()
				return nil
			}

			application := app.NewApplication(config)
			return application.Start()
		},
	}

	rootCmd.Flags().Uint32VarP(&config.Frequency, "frequency", "f", app.DefaultFrequency, "Frequency to tune to (Hz)")
	rootCmd.Flags().Uint32VarP(&config.SampleRate, "sample-rate", "s", app.DefaultSampleRate, "Sample rate (Hz)")
	rootCmd.Flags().IntVarP(&config.Gain, "gain", "g", app.DefaultGain, "Gain setting (0 for auto)")
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
