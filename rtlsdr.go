//go:build cgo

package main

/*
#cgo CFLAGS: -I/opt/homebrew/opt/librtlsdr/include
#cgo LDFLAGS: -L/opt/homebrew/opt/librtlsdr/lib -lrtlsdr
#include <rtl-sdr.h>
#include <stdlib.h>
#include <stdint.h>

// Callback function for RTL-SDR
extern void goRTLSDRCallback(unsigned char *buf, uint32_t len, void *ctx);

// C wrapper function
static void rtlsdr_callback_wrapper(unsigned char *buf, uint32_t len, void *ctx) {
    goRTLSDRCallback(buf, len, ctx);
}

// Helper function to get the callback
static rtlsdr_read_async_cb_t get_callback_func() {
    return rtlsdr_callback_wrapper;
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"
)

// RTLSDRDevice represents an RTL-SDR device
type RTLSDRDevice struct {
	dev         *C.rtlsdr_dev_t
	deviceIndex int
	isRunning   bool
	dataChan    chan []byte
	deviceID    uintptr
}

// NewRTLSDRDevice creates a new RTL-SDR device
func NewRTLSDRDevice(deviceIndex int) (*RTLSDRDevice, error) {
	device := &RTLSDRDevice{
		deviceIndex: deviceIndex,
		isRunning:   false,
	}

	// Check if device exists
	deviceCount := int(C.rtlsdr_get_device_count())
	if deviceCount == 0 {
		return nil, fmt.Errorf("no RTL-SDR devices found")
	}

	if deviceIndex >= deviceCount {
		return nil, fmt.Errorf("device index %d out of range (0-%d)", deviceIndex, deviceCount-1)
	}

	// Open device
	ret := C.rtlsdr_open(&device.dev, C.uint32_t(deviceIndex))
	if ret != 0 {
		return nil, fmt.Errorf("failed to open RTL-SDR device %d: %d", deviceIndex, ret)
	}

	return device, nil
}

// Configure configures the RTL-SDR device
func (d *RTLSDRDevice) Configure(frequency uint32, sampleRate uint32, gain int) error {
	if d.dev == nil {
		return fmt.Errorf("device not initialized")
	}

	// Set frequency
	ret := C.rtlsdr_set_center_freq(d.dev, C.uint32_t(frequency))
	if ret != 0 {
		return fmt.Errorf("failed to set frequency: %d", ret)
	}

	// Set sample rate
	ret = C.rtlsdr_set_sample_rate(d.dev, C.uint32_t(sampleRate))
	if ret != 0 {
		return fmt.Errorf("failed to set sample rate: %d", ret)
	}

	// Set gain
	if gain == 0 {
		// Enable automatic gain control
		ret = C.rtlsdr_set_agc_mode(d.dev, 1)
		if ret != 0 {
			return fmt.Errorf("failed to enable AGC: %d", ret)
		}
	} else {
		// Disable automatic gain control
		ret = C.rtlsdr_set_agc_mode(d.dev, 0)
		if ret != 0 {
			return fmt.Errorf("failed to disable AGC: %d", ret)
		}

		// Set manual gain
		ret = C.rtlsdr_set_tuner_gain(d.dev, C.int(gain))
		if ret != 0 {
			return fmt.Errorf("failed to set gain: %d", ret)
		}
	}

	// Reset buffer
	ret = C.rtlsdr_reset_buffer(d.dev)
	if ret != 0 {
		return fmt.Errorf("failed to reset buffer: %d", ret)
	}

	return nil
}

// StartCapture starts capturing data from the RTL-SDR device
func (d *RTLSDRDevice) StartCapture(ctx context.Context, dataChan chan []byte) error {
	if d.dev == nil {
		return fmt.Errorf("device not initialized")
	}

	if d.isRunning {
		return fmt.Errorf("capture already running")
	}

	d.dataChan = dataChan
	d.isRunning = true

	// Register this device instance for the callback
	rtlsdrDevicesMutex.Lock()
	rtlsdrDeviceCounter++
	d.deviceID = rtlsdrDeviceCounter
	rtlsdrDevices[d.deviceID] = d
	rtlsdrDevicesMutex.Unlock()

	// Start async reading
	go func() {
		defer func() {
			d.isRunning = false

			// Unregister device
			rtlsdrDevicesMutex.Lock()
			delete(rtlsdrDevices, d.deviceID)
			rtlsdrDevicesMutex.Unlock()
		}()

		ret := C.rtlsdr_read_async(d.dev, C.get_callback_func(), unsafe.Pointer(d.deviceID), 0, 0)
		if ret != 0 {
			// Handle error - this will be logged by the calling function
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Cancel async reading
	C.rtlsdr_cancel_async(d.dev)

	return nil
}

// Close closes the RTL-SDR device
func (d *RTLSDRDevice) Close() error {
	if d.dev == nil {
		return nil
	}

	if d.isRunning {
		C.rtlsdr_cancel_async(d.dev)
		d.isRunning = false
	}

	// Unregister device
	rtlsdrDevicesMutex.Lock()
	delete(rtlsdrDevices, d.deviceID)
	rtlsdrDevicesMutex.Unlock()

	ret := C.rtlsdr_close(d.dev)
	d.dev = nil

	if ret != 0 {
		return fmt.Errorf("failed to close RTL-SDR device: %d", ret)
	}

	return nil
}

// GetDeviceInfo returns information about the device
func (d *RTLSDRDevice) GetDeviceInfo() (string, error) {
	if d.deviceIndex < 0 {
		return "", fmt.Errorf("invalid device index")
	}

	deviceCount := int(C.rtlsdr_get_device_count())
	if d.deviceIndex >= deviceCount {
		return "", fmt.Errorf("device index out of range")
	}

	name := C.rtlsdr_get_device_name(C.uint32_t(d.deviceIndex))
	if name == nil {
		return "", fmt.Errorf("failed to get device name")
	}

	return C.GoString(name), nil
}

// Global map to track RTL-SDR devices for callbacks
var rtlsdrDevices = make(map[uintptr]*RTLSDRDevice)
var rtlsdrDevicesMutex sync.RWMutex
var rtlsdrDeviceCounter uintptr = 1

// RTL-SDR callback function (called from C)
//
//export goRTLSDRCallback
func goRTLSDRCallback(buf *C.uchar, length C.uint32_t, ctx unsafe.Pointer) {
	// Get device ID from context
	deviceID := uintptr(ctx)

	rtlsdrDevicesMutex.RLock()
	device, exists := rtlsdrDevices[deviceID]
	rtlsdrDevicesMutex.RUnlock()

	if !exists || device == nil || device.dataChan == nil {
		return
	}

	// Convert C buffer to Go slice
	data := C.GoBytes(unsafe.Pointer(buf), C.int(length))

	// Send data to channel (non-blocking)
	select {
	case device.dataChan <- data:
	default:
		// Channel is full, drop the data
	}
}
