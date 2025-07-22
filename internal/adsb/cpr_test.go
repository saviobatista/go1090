package adsb

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestNewCPRDecoder tests the CPR decoder constructor
func TestNewCPRDecoder(t *testing.T) {
	logger := logrus.New()
	decoder := NewCPRDecoder(logger, false)
	assert.NotNil(t, decoder)
	assert.NotNil(t, decoder.aircraftPositions)
}

// TestCPRNFunction tests the NL (Number of Longitude Zones) function
func TestCPRNFunction(t *testing.T) {
	logger := logrus.New()
	decoder := NewCPRDecoder(logger, false)

	tests := []struct {
		name     string
		latitude float64
		fflag    int
	}{
		{
			name:     "Equator, even frame",
			latitude: 0.0,
			fflag:    0,
		},
		{
			name:     "Equator, odd frame",
			latitude: 0.0,
			fflag:    1,
		},
		{
			name:     "Latitude 30°, even frame",
			latitude: 30.0,
			fflag:    0,
		},
		{
			name:     "Latitude 30°, odd frame",
			latitude: 30.0,
			fflag:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decoder.cprNFunction(tt.latitude, tt.fflag)
			// Just test that it returns a reasonable value
			assert.Greater(t, result, 0)
			assert.LessOrEqual(t, result, 59)
			t.Logf("NL(%.1f, %d) = %d", tt.latitude, tt.fflag, result)
		})
	}
}

// TestCPRDlonFunction tests the Dlon (longitude zone width) function
func TestCPRDlonFunction(t *testing.T) {
	logger := logrus.New()
	decoder := NewCPRDecoder(logger, false)

	tests := []struct {
		name     string
		latitude float64
		fflag    int
	}{
		{
			name:     "Equator, even frame",
			latitude: 0.0,
			fflag:    0,
		},
		{
			name:     "Equator, odd frame",
			latitude: 0.0,
			fflag:    1,
		},
		{
			name:     "Latitude 30°, even frame",
			latitude: 30.0,
			fflag:    0,
		},
		{
			name:     "Latitude 30°, odd frame",
			latitude: 30.0,
			fflag:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decoder.cprDlonFunction(tt.latitude, tt.fflag)
			// Just test that it returns a reasonable value
			assert.Greater(t, result, 0.0)
			assert.LessOrEqual(t, result, 360.0)
			t.Logf("Dlon(%.1f, %d) = %.6f", tt.latitude, tt.fflag, result)
		})
	}
}

// TestDecodeCPRPosition tests basic position decoding
func TestDecodeCPRPosition(t *testing.T) {
	logger := logrus.New()
	decoder := NewCPRDecoder(logger, true) // verbose for debugging

	tests := []struct {
		name        string
		icao        uint32
		fFlag       uint8
		cprLat      uint32
		cprLon      uint32
		expectValid bool
	}{
		{
			name:        "Even frame",
			icao:        0x484412,
			fFlag:       0,
			cprLat:      0x5D4A4,
			cprLon:      0x2F8B4,
			expectValid: true, // Should store frame even if can't decode yet
		},
		{
			name:        "Odd frame same aircraft",
			icao:        0x484412,
			fFlag:       1,
			cprLat:      0x5D4A5,
			cprLon:      0x2F8B5,
			expectValid: true, // Should now be able to decode with both frames
		},
		{
			name:        "Different aircraft",
			icao:        0x123456,
			fFlag:       0,
			cprLat:      0x3D4A4,
			cprLon:      0x1F8B4,
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon := decoder.DecodeCPRPosition(tt.icao, tt.fFlag, tt.cprLat, tt.cprLon)

			// Basic validation
			if lat != 0 || lon != 0 {
				assert.True(t, lat >= -90.0 && lat <= 90.0, "Latitude should be in valid range")
				assert.True(t, lon >= -180.0 && lon <= 180.0, "Longitude should be in valid range")
			}

			t.Logf("ICAO %06X fFlag %d: lat=%.6f, lon=%.6f", tt.icao, tt.fFlag, lat, lon)
		})
	}
}

// TestCPRConcurrentAccess tests concurrent access to the CPR decoder
func TestCPRConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	decoder := NewCPRDecoder(logger, false)

	// Test concurrent decoding with different ICAOs
	const numGoroutines = 5
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(icao uint32) {
			defer func() { done <- true }()

			// Decode some positions for this ICAO
			decoder.DecodeCPRPosition(icao, 0, 0x5D4A4, 0x2F8B4)
			decoder.DecodeCPRPosition(icao, 1, 0x5D4A5, 0x2F8B5)
		}(uint32(0x484410 + i))
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should have aircraft positions for all ICAOs
	assert.Len(t, decoder.aircraftPositions, numGoroutines)
}

// TestCPRConstants tests CPR-related constants
func TestCPRConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant interface{}
		expected interface{}
	}{
		{
			name:     "CPR_LAT_MAX",
			constant: CPR_LAT_MAX,
			expected: int(131072), // 2^17
		},
		{
			name:     "CPR_LON_MAX",
			constant: CPR_LON_MAX,
			expected: int(131072), // 2^17
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.constant)
		})
	}
}
