// Copyright (c) 2012-2017 Joseph D Poirier
// Distributable under the terms of The New BSD License
// that can be found in the LICENSE file.

// Package gortlsdr wraps librtlsdr, which turns your Realtek RTL2832 based
// DVB dongle into a SDR receiver.

package rtlsdr

import (
	"context"
	"errors"
	"fmt"

	rtlsdr "github.com/jpoirier/gortlsdr"
	"github.com/sirupsen/logrus"
)

// Buffer size constants for RTL-SDR data capture
const (
	BufferChunkSize = 16384 // 16KB chunk size for RTL-SDR buffer
)

// RTLSDRDevice represents an RTL-SDR device
type RTLSDRDevice struct {
	device   *rtlsdr.Context
	logger   *logrus.Logger
	index    int
	isOpen   bool
	cancelFn context.CancelFunc
}

// NewRTLSDRDevice creates a new RTL-SDR device
func NewRTLSDRDevice(index int) (*RTLSDRDevice, error) {
	logger := logrus.New()

	// Check if device exists
	count := rtlsdr.GetDeviceCount()
	if count == 0 {
		return nil, errors.New("no RTL-SDR devices found")
	}

	if index >= count {
		return nil, fmt.Errorf("device index %d out of range (0-%d)", index, count-1)
	}

	return &RTLSDRDevice{
		logger: logger,
		index:  index,
		isOpen: false,
	}, nil
}

// Configure configures the RTL-SDR device
func (r *RTLSDRDevice) Configure(frequency, sampleRate uint32, gain int) error {
	var err error

	// Open device
	r.device, err = rtlsdr.Open(r.index)
	if err != nil {
		return fmt.Errorf("failed to open device: %w", err)
	}
	r.isOpen = true

	// Set frequency
	if err := r.device.SetCenterFreq(int(frequency)); err != nil {
		return fmt.Errorf("failed to set frequency: %w", err)
	}

	// Set sample rate
	if err := r.device.SetSampleRate(int(sampleRate)); err != nil {
		return fmt.Errorf("failed to set sample rate: %w", err)
	}

	// Set gain
	if gain == 0 {
		// Auto gain
		if err := r.device.SetTunerGainMode(false); err != nil {
			return fmt.Errorf("failed to set auto gain: %w", err)
		}
	} else {
		// Manual gain
		if err := r.device.SetTunerGainMode(true); err != nil {
			return fmt.Errorf("failed to set manual gain mode: %w", err)
		}

		// Convert gain to tenths of dB
		gainTenths := gain * 10
		if err := r.device.SetTunerGain(gainTenths); err != nil {
			return fmt.Errorf("failed to set gain: %w", err)
		}
	}

	// Reset buffer
	if err := r.device.ResetBuffer(); err != nil {
		return fmt.Errorf("failed to reset buffer: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"device_index": r.index,
		"frequency":    frequency,
		"sample_rate":  sampleRate,
		"gain":         gain,
	}).Info("RTL-SDR device configured successfully")

	return nil
}

// StartCapture starts capturing data from the RTL-SDR device
func (r *RTLSDRDevice) StartCapture(ctx context.Context, dataChan chan<- []byte) error {
	if !r.isOpen {
		return errors.New("device not open")
	}

	// Create a cancelable context
	captureCtx, cancel := context.WithCancel(ctx)
	r.cancelFn = cancel

	// Buffer for reading data
	bufLen := 16 * BufferChunkSize // 256KB buffer

	// Callback function for async reads
	callback := func(data []byte) {
		select {
		case dataChan <- data:
		case <-captureCtx.Done():
			return
		default:
			// Drop data if channel is full
			r.logger.Debug("Dropping data, channel full")
		}
	}

	r.logger.Info("Starting RTL-SDR capture")

	// Start async reading in a goroutine
	go func() {
		defer func() {
			if panicData := recover(); panicData != nil {
				r.logger.WithField("panic", panicData).Error("RTL-SDR capture panic")
			}
		}()

		// This will block until canceled
		if err := r.device.ReadAsync(callback, nil, 0, bufLen); err != nil {
			r.logger.WithError(err).Error("RTL-SDR read async failed")
		}
	}()

	// Wait for context cancellation
	<-captureCtx.Done()

	// Cancel async reading
	if err := r.device.CancelAsync(); err != nil {
		r.logger.WithError(err).Error("Failed to cancel async reading")
	}

	return nil
}

// Close closes the RTL-SDR device
func (r *RTLSDRDevice) Close() error {
	if r.cancelFn != nil {
		r.cancelFn()
	}

	if r.device != nil && r.isOpen {
		if err := r.device.Close(); err != nil {
			return fmt.Errorf("failed to close device: %w", err)
		}
		r.isOpen = false
		r.logger.Info("RTL-SDR device closed")
	}

	return nil
}
