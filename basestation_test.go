package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestBaseStationWriter_WriteMessage(t *testing.T) {
	// Create temporary directory for testing
	tmpdir, err := os.MkdirTemp("", "basestation_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		t.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	writer := NewBaseStationWriter(logRotator, logger)

	// Test message
	message := &BeastMessage{
		MessageType: BeastModeS,
		Timestamp:   time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Signal:      150,
		Data:        []byte{0x8D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x12, 0x34, 0x56},
	}

	err = writer.WriteMessage(message)
	if err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Check that a log file was created
	files, err := filepath.Glob(filepath.Join(tmpdir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("No log files created")
	}

	// Read the log file content
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(content) == 0 {
		t.Fatalf("Log file is empty")
	}

	// Check that content contains expected BaseStation format
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) == 0 {
		t.Fatalf("No lines in log file")
	}

	// Basic validation - should start with MSG
	if !strings.HasPrefix(lines[0], "MSG,") {
		t.Errorf("Expected line to start with 'MSG,', got: %s", lines[0])
	}

	// Check ICAO is present (should be 484412 from test data)
	if !strings.Contains(lines[0], "484412") {
		t.Errorf("Expected ICAO 484412 in output, got: %s", lines[0])
	}
}

func TestBaseStationWriter_MultipleModes(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "basestation_multi_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		t.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	writer := NewBaseStationWriter(logRotator, logger)

	testCases := []struct {
		name        string
		messageType byte
		data        []byte
	}{
		{
			name:        "Mode S Short",
			messageType: BeastModeS,
			data:        []byte{0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
		},
		{
			name:        "Mode S Long",
			messageType: BeastModeSLong,
			data:        []byte{0x8D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x12, 0x34, 0x56},
		},
		{
			name:        "Mode A/C",
			messageType: BeastModeAC,
			data:        []byte{0x02, 0x34},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			message := &BeastMessage{
				MessageType: tc.messageType,
				Timestamp:   time.Now(),
				Signal:      100,
				Data:        tc.data,
			}

			err := writer.WriteMessage(message)
			if err != nil {
				t.Errorf("Failed to write %s message: %v", tc.name, err)
			}
		})
	}
}

func TestBaseStationWriter_ConcurrentWrite(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "basestation_concurrent_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		t.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	writer := NewBaseStationWriter(logRotator, logger)

	// Write messages concurrently
	numGoroutines := 10
	messagesPerGoroutine := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < messagesPerGoroutine; j++ {
				message := &BeastMessage{
					MessageType: BeastModeS,
					Timestamp:   time.Now(),
					Signal:      byte(100 + goroutineID),
					Data:        []byte{0x5D, 0x48, 0x44, byte(goroutineID), byte(j), 0x56, 0x78},
				}

				err := writer.WriteMessage(message)
				if err != nil {
					t.Errorf("Concurrent write failed: %v", err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Check that log files were created and contain data
	files, err := filepath.Glob(filepath.Join(tmpdir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("No log files created")
	}

	// Count total lines written
	totalLines := 0
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Failed to read file %s: %v", file, err)
		}

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			totalLines += len(lines)
		}
	}

	expectedLines := numGoroutines * messagesPerGoroutine
	if totalLines != expectedLines {
		t.Errorf("Expected %d lines total, got %d", expectedLines, totalLines)
	}
}

func TestBaseStationWriter_InvalidMessages(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "basestation_invalid_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		t.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	writer := NewBaseStationWriter(logRotator, logger)

	testCases := []struct {
		name    string
		message *BeastMessage
		wantErr bool
	}{
		{
			name:    "Nil message",
			message: nil,
			wantErr: true,
		},
		{
			name: "Empty data",
			message: &BeastMessage{
				MessageType: BeastModeS,
				Timestamp:   time.Now(),
				Signal:      100,
				Data:        []byte{},
			},
			wantErr: true, // Empty data is invalid
		},
		{
			name: "Valid message",
			message: &BeastMessage{
				MessageType: BeastModeS,
				Timestamp:   time.Now(),
				Signal:      100,
				Data:        []byte{0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78},
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := writer.WriteMessage(tc.message)

			if tc.wantErr && err == nil {
				t.Errorf("Expected error, got none")
			}

			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func BenchmarkBaseStationWriter_WriteMessage(b *testing.B) {
	tmpdir, err := os.MkdirTemp("", "basestation_bench_*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		b.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	writer := NewBaseStationWriter(logRotator, logger)

	message := &BeastMessage{
		MessageType: BeastModeS,
		Timestamp:   time.Now(),
		Signal:      150,
		Data:        []byte{0x8D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x12, 0x34, 0x56},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := writer.WriteMessage(message)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}
}
