//go:build !cgo

package main

import (
	"context"
	"fmt"
)

// RTLSDRDevice represents a stub RTL-SDR device for Windows
type RTLSDRDevice struct {
	deviceIndex int
}

// NewRTLSDRDevice creates a stub RTL-SDR device that returns an error
func NewRTLSDRDevice(deviceIndex int) (*RTLSDRDevice, error) {
	return nil, fmt.Errorf("RTL-SDR hardware support is not available on Windows builds. Please use Linux or macOS for full RTL-SDR functionality")
}

// Configure returns an error for stub implementation
func (d *RTLSDRDevice) Configure(frequency uint32, sampleRate uint32, gain int) error {
	return fmt.Errorf("RTL-SDR hardware support is not available on Windows builds")
}

// StartCapture returns an error for stub implementation
func (d *RTLSDRDevice) StartCapture(ctx context.Context, dataChan chan []byte) error {
	return fmt.Errorf("RTL-SDR hardware support is not available on Windows builds")
}

// Close returns an error for stub implementation
func (d *RTLSDRDevice) Close() error {
	return fmt.Errorf("RTL-SDR hardware support is not available on Windows builds")
}

// GetDeviceInfo returns an error for stub implementation
func (d *RTLSDRDevice) GetDeviceInfo() (string, error) {
	return "", fmt.Errorf("RTL-SDR hardware support is not available on Windows builds")
}
