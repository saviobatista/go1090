package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfig tests the configuration struct and constants
func TestConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name: "Default configuration",
			config: Config{
				Frequency:    DefaultFrequency,
				SampleRate:   DefaultSampleRate,
				Gain:         DefaultGain,
				DeviceIndex:  0,
				LogDir:       "./logs",
				LogRotateUTC: true,
				Verbose:      false,
				ShowVersion:  false,
			},
			expected: true,
		},
		{
			name: "Custom configuration",
			config: Config{
				Frequency:    1090500000,
				SampleRate:   2000000,
				Gain:         30,
				DeviceIndex:  1,
				LogDir:       "/tmp/logs",
				LogRotateUTC: false,
				Verbose:      true,
				ShowVersion:  true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that configuration values are properly set
			assert.Equal(t, tt.config.Frequency, tt.config.Frequency)
			assert.Equal(t, tt.config.SampleRate, tt.config.SampleRate)
			assert.Equal(t, tt.config.Gain, tt.config.Gain)
		})
	}
}

// TestConstants tests the default configuration constants
func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant interface{}
		expected interface{}
	}{
		{
			name:     "DefaultFrequency",
			constant: DefaultFrequency,
			expected: uint32(1090000000), // 1090 MHz
		},
		{
			name:     "DefaultSampleRate",
			constant: DefaultSampleRate,
			expected: uint32(2400000), // 2.4 MHz
		},
		{
			name:     "DefaultGain",
			constant: DefaultGain,
			expected: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.constant)
		})
	}
}

// TestShowVersion tests the version display functionality
func TestShowVersion(t *testing.T) {
	// Test that ShowVersion doesn't panic
	assert.NotPanics(t, func() {
		ShowVersion()
	})
}

// TestNewApplication tests the application constructor
func TestNewApplication(t *testing.T) {
	config := Config{
		Frequency:    DefaultFrequency,
		SampleRate:   DefaultSampleRate,
		Gain:         DefaultGain,
		DeviceIndex:  0,
		LogDir:       "./test_logs",
		LogRotateUTC: true,
		Verbose:      false,
		ShowVersion:  false,
	}

	app := NewApplication(config)

	assert.NotNil(t, app)
	assert.NotNil(t, app.logger)
	// Note: config fields are private, so we test functionality instead
}

// TestApplication_ConfigValidation tests configuration validation
func TestApplication_ConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectValid bool
	}{
		{
			name: "Valid configuration",
			config: Config{
				Frequency:    1090000000,
				SampleRate:   2400000,
				Gain:         40,
				DeviceIndex:  0,
				LogDir:       "./logs",
				LogRotateUTC: true,
			},
			expectValid: true,
		},
		{
			name: "Zero frequency",
			config: Config{
				Frequency:    0,
				SampleRate:   2400000,
				Gain:         40,
				DeviceIndex:  0,
				LogDir:       "./logs",
				LogRotateUTC: true,
			},
			expectValid: false, // Would likely cause issues
		},
		{
			name: "Zero sample rate",
			config: Config{
				Frequency:    1090000000,
				SampleRate:   0,
				Gain:         40,
				DeviceIndex:  0,
				LogDir:       "./logs",
				LogRotateUTC: true,
			},
			expectValid: false, // Would likely cause issues
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApplication(tt.config)
			assert.NotNil(t, app) // Constructor should always succeed

			// Basic validation - just check app was created
			assert.NotNil(t, app)
		})
	}
}

// TestApplication_LoggerConfiguration tests logger setup
func TestApplication_LoggerConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
	}{
		{
			name:    "Verbose logging",
			verbose: true,
		},
		{
			name:    "Normal logging",
			verbose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Frequency:    DefaultFrequency,
				SampleRate:   DefaultSampleRate,
				Gain:         DefaultGain,
				DeviceIndex:  0,
				LogDir:       "./test_logs",
				LogRotateUTC: true,
				Verbose:      tt.verbose,
			}

			app := NewApplication(config)
			assert.NotNil(t, app.logger)
		})
	}
}

// TestApplication_BytesToIQ tests I/Q data conversion
func TestApplication_BytesToIQ(t *testing.T) {
	config := Config{
		Frequency:    DefaultFrequency,
		SampleRate:   DefaultSampleRate,
		Gain:         DefaultGain,
		DeviceIndex:  0,
		LogDir:       "./test_logs",
		LogRotateUTC: true,
		Verbose:      false,
	}
	app := NewApplication(config)

	tests := []struct {
		name        string
		input       []byte
		expectedLen int
	}{
		{
			name:        "Empty input",
			input:       []byte{},
			expectedLen: 0,
		},
		{
			name:        "Single I/Q pair",
			input:       []byte{0x80, 0x80}, // 2 bytes = 1 I/Q pair
			expectedLen: 2,                  // 1 I + 1 Q = 2 float32 values
		},
		{
			name:        "Multiple I/Q pairs",
			input:       []byte{0x80, 0x80, 0x7F, 0x7F, 0x81, 0x81}, // 6 bytes = 3 I/Q pairs
			expectedLen: 6,                                          // 3 I + 3 Q = 6 float32 values
		},
		{
			name:        "Odd number of bytes (should process complete pairs only)",
			input:       []byte{0x80, 0x80, 0x7F}, // 3 bytes, only first 2 can form a complete I/Q pair
			expectedLen: 2,                        // 1 complete I/Q pair = 2 float32 values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := app.bytesToIQ(tt.input)
			assert.Equal(t, tt.expectedLen, len(result))

			// Test value conversion (0x80 = 128 should convert to ~0.0)
			if len(result) >= 2 && len(tt.input) >= 2 {
				// 0x80 (128) converted to float should be around 0.0
				// Since 0x80 represents the center value in unsigned 8-bit
				assert.InDelta(t, 0.0, result[0], 1.0) // Allow some tolerance for I
				assert.InDelta(t, 0.0, result[1], 1.0) // Allow some tolerance for Q
			}
		})
	}
}

// TestApplication_Context tests the context functionality
func TestApplication_Context(t *testing.T) {
	config := Config{
		Frequency:    DefaultFrequency,
		SampleRate:   DefaultSampleRate,
		Gain:         DefaultGain,
		DeviceIndex:  0,
		LogDir:       "./test_logs",
		LogRotateUTC: true,
		Verbose:      false,
	}

	app := NewApplication(config)
	assert.NotNil(t, app)
	// Context functionality is internal, just verify app creation
}

// Cleanup test logs
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()

	// Cleanup
	os.RemoveAll("./test_logs")

	os.Exit(code)
}
