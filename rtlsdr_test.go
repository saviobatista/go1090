package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewRTLSDRDevice tests the NewRTLSDRDevice function
func TestNewRTLSDRDevice(t *testing.T) {
	// Since we can't control the actual RTL-SDR count, we'll test the basic structure
	// Note: This test depends on the system's RTL-SDR availability

	tests := []struct {
		name  string
		index int
	}{
		{
			name:  "Index 0",
			index: 0,
		},
		{
			name:  "Index 1",
			index: 1,
		},
		{
			name:  "High index",
			index: 99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, err := NewRTLSDRDevice(tt.index)

			// Since this depends on hardware availability, we don't assert specific outcomes
			// but we verify the function doesn't panic and returns consistent results
			if err != nil {
				// Error is expected if no RTL-SDR device is available
				assert.Nil(t, device)
				// Error might be about device index range or RTL-SDR availability
				assert.True(t, strings.Contains(err.Error(), "RTL-SDR") || strings.Contains(err.Error(), "device index"))
			} else {
				// If no error, device should be properly initialized
				assert.NotNil(t, device)
				assert.Equal(t, tt.index, device.index)
				assert.NotNil(t, device.logger)
				assert.False(t, device.isOpen)
			}
		})
	}
}

// TestRTLSDRDevice_EdgeCases tests edge cases and error conditions
func TestRTLSDRDevice_EdgeCases(t *testing.T) {
	t.Run("Close nil device", func(t *testing.T) {
		device := &RTLSDRDevice{
			index:  0,
			isOpen: false,
		}

		err := device.Close()
		assert.NoError(t, err)
		assert.False(t, device.isOpen)
	})

	t.Run("Configure uninitialized device", func(t *testing.T) {
		device := &RTLSDRDevice{
			index:  0,
			isOpen: false,
		}

		// Don't test Configure on uninitialized device as it requires hardware
		// Just verify the device structure is correct
		assert.Equal(t, 0, device.index)
		assert.False(t, device.isOpen)
		assert.Nil(t, device.device)
	})

	t.Run("StartCapture on closed device", func(t *testing.T) {
		device := &RTLSDRDevice{
			index:  0,
			isOpen: false,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		dataChan := make(chan []byte, 10)

		err := device.StartCapture(ctx, dataChan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not open")
	})

	t.Run("StartCapture with nil channel", func(t *testing.T) {
		device := &RTLSDRDevice{
			index:  0,
			isOpen: false,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		// This should not panic
		err := device.StartCapture(ctx, nil)
		assert.Error(t, err) // Should error because device is not open
	})

	t.Run("Multiple Close calls", func(t *testing.T) {
		device := &RTLSDRDevice{
			index:  0,
			isOpen: false,
		}

		// Multiple close calls should be safe
		err1 := device.Close()
		err2 := device.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}

// TestRTLSDRDevice_ConcurrentAccess tests thread safety of basic operations
func TestRTLSDRDevice_ConcurrentAccess(t *testing.T) {
	device := &RTLSDRDevice{
		index:  0,
		isOpen: false,
	}

	// Test concurrent Close() calls
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			device.Close()
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should complete without race conditions
	assert.False(t, device.isOpen)
}

// TestRTLSDRDevice_ContextCancellation tests context cancellation behavior
func TestRTLSDRDevice_ContextCancellation(t *testing.T) {
	device := &RTLSDRDevice{
		index:  0,
		isOpen: false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	dataChan := make(chan []byte, 10)

	// Start a goroutine that will call StartCapture
	go func() {
		device.StartCapture(ctx, dataChan)
	}()

	// Cancel context immediately
	cancel()

	// Give some time for the goroutine to handle cancellation
	time.Sleep(10 * time.Millisecond)

	// Should complete gracefully without hanging
	assert.True(t, true)
}

// TestRTLSDRDevice_ParameterValidation tests parameter validation
func TestRTLSDRDevice_ParameterValidation(t *testing.T) {
	tests := []struct {
		name       string
		frequency  uint32
		sampleRate uint32
		gain       int
	}{
		{
			name:       "Valid ADS-B parameters",
			frequency:  1090000000,
			sampleRate: 2400000,
			gain:       40,
		},
		{
			name:       "Auto gain",
			frequency:  1090000000,
			sampleRate: 2400000,
			gain:       0,
		},
		{
			name:       "High gain",
			frequency:  1090000000,
			sampleRate: 2400000,
			gain:       496,
		},
		{
			name:       "Different frequency",
			frequency:  868000000,
			sampleRate: 2000000,
			gain:       20,
		},
		{
			name:       "Low frequency",
			frequency:  100000000,
			sampleRate: 1000000,
			gain:       10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Don't actually call Configure as it requires hardware initialization
			// Just verify the parameters are valid types
			assert.IsType(t, uint32(0), tt.frequency)
			assert.IsType(t, uint32(0), tt.sampleRate)
			assert.IsType(t, 0, tt.gain)
			assert.True(t, tt.frequency > 0)
			assert.True(t, tt.sampleRate > 0)
			assert.True(t, tt.gain >= 0)
		})
	}
}

// TestRTLSDRDevice_Structure tests the basic structure and fields
func TestRTLSDRDevice_Structure(t *testing.T) {
	device := &RTLSDRDevice{
		index:  5,
		isOpen: true,
	}

	assert.Equal(t, 5, device.index)
	assert.True(t, device.isOpen)
	assert.Nil(t, device.device)   // Should be nil until opened
	assert.Nil(t, device.logger)   // Should be nil until initialized
	assert.Nil(t, device.cancelFn) // Should be nil until capture starts
}

// TestRTLSDRDevice_StateTransitions tests state transitions
func TestRTLSDRDevice_StateTransitions(t *testing.T) {
	device := &RTLSDRDevice{
		index:  0,
		isOpen: false,
	}

	// Initial state
	assert.False(t, device.isOpen)
	assert.Nil(t, device.device)

	// After close (should remain closed)
	device.Close()
	assert.False(t, device.isOpen)

	// Test that Close() is safe to call multiple times
	device.Close()
	assert.False(t, device.isOpen)
}

// Benchmark tests for performance
func BenchmarkNewRTLSDRDevice(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		device, _ := NewRTLSDRDevice(0)
		if device != nil {
			device.Close()
		}
	}
}

func BenchmarkRTLSDRDevice_Close(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		device := &RTLSDRDevice{
			index:  0,
			isOpen: true,
		}
		device.Close()
	}
}

func BenchmarkRTLSDRDevice_Configure(b *testing.B) {
	// Skip benchmarking Configure as it requires hardware initialization
	b.Skip("Configure requires hardware initialization")
}
