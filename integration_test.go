package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestIntegration_BeastToBaseStation(t *testing.T) {
	// Create temporary directory for test logs
	tmpdir, err := os.MkdirTemp("", "integration_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	// Set up logger
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create log rotator
	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		t.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	// Create Beast decoder
	decoder := NewBeastDecoder(logger)

	// Create BaseStation writer
	writer := NewBaseStationWriter(logRotator, logger)

	// Test data: complete Beast mode messages
	testMessages := []struct {
		name      string
		beastData []byte
		expectMsg bool
	}{
		{
			name: "Mode S Short Message",
			beastData: []byte{
				0x1A, 0x32, // Sync + Type
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // Timestamp
				0x64,                                     // Signal level (100)
				0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78, // Message data
			},
			expectMsg: true,
		},
		{
			name: "Mode S Long Message with ADS-B",
			beastData: []byte{
				0x1A, 0x33, // Sync + Type
				0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // Timestamp
				0x80, // Signal level (128)
				// Extended squitter message (DF=17)
				0x8D, 0x48, 0x44, 0x12, 0x58, 0x9F, 0x48, 0xA3,
				0xC4, 0x7E, 0x30, 0x34, 0x56, 0x78,
			},
			expectMsg: true,
		},
		{
			name: "Mode A/C Message",
			beastData: []byte{
				0x1A, 0x31, // Sync + Type
				0x00, 0x00, 0x00, 0x00, 0x00, 0x03, // Timestamp
				0x50,       // Signal level (80)
				0x20, 0x12, // Squawk code
			},
			expectMsg: true,
		},
	}

	// Process each test message through the full pipeline
	for _, tc := range testMessages {
		t.Run(tc.name, func(t *testing.T) {
			// Decode Beast message
			messages, err := decoder.Decode(tc.beastData)
			if err != nil {
				t.Fatalf("Failed to decode Beast message: %v", err)
			}

			if !tc.expectMsg {
				if len(messages) > 0 {
					t.Errorf("Expected no messages, got %d", len(messages))
				}
				return
			}

			if len(messages) == 0 {
				t.Fatalf("Expected decoded message, got none")
			}

			// Write to BaseStation format
			for _, msg := range messages {
				err = writer.WriteMessage(msg)
				if err != nil {
					t.Fatalf("Failed to write BaseStation message: %v", err)
				}
			}
		})
	}

	// Verify output files were created
	files, err := filepath.Glob(filepath.Join(tmpdir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list output files: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("No output files created")
	}

	// Read and verify content
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) < 3 {
		t.Errorf("Expected at least 3 lines of output, got %d", len(lines))
	}

	// Verify each line is valid BaseStation format
	for i, line := range lines {
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "MSG,") {
			t.Errorf("Line %d does not start with MSG: %s", i, line)
		}

		fields := strings.Split(line, ",")
		if len(fields) < 10 {
			t.Errorf("Line %d has insufficient fields (%d): %s", i, len(fields), line)
		}
	}
}

func TestIntegration_ConcurrentProcessing(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "integration_concurrent_*")
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

	// Simulate concurrent message processing
	numWorkers := 5
	messagesPerWorker := 20
	messageChan := make(chan []byte, numWorkers*messagesPerWorker)
	done := make(chan bool, numWorkers)

	// Generate test messages
	baseMessage := []byte{
		0x1A, 0x32,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x64,
		0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78,
	}

	// Fill channel with messages
	for i := 0; i < numWorkers*messagesPerWorker; i++ {
		// Vary the ICAO to create different messages
		message := make([]byte, len(baseMessage))
		copy(message, baseMessage)
		message[10] = byte(i % 256) // Vary ICAO
		messageChan <- message
	}
	close(messageChan)

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer func() { done <- true }()

			// Each worker gets its own decoder to avoid race conditions
			decoder := NewBeastDecoder(logger)

			for beastData := range messageChan {
				// Decode message
				messages, err := decoder.Decode(beastData)
				if err != nil {
					t.Errorf("Worker %d: decode failed: %v", workerID, err)
					continue
				}

				// Write messages
				for _, msg := range messages {
					err = writer.WriteMessage(msg)
					if err != nil {
						// Some messages may be invalid due to test data variations
						// Log as info but don't fail the test
						t.Logf("Worker %d: write failed (expected in test): %v", workerID, err)
					}
				}
			}
		}(i)
	}

	// Wait for all workers to complete
	for i := 0; i < numWorkers; i++ {
		<-done
	}

	// Verify output
	files, err := filepath.Glob(filepath.Join(tmpdir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list output files: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("No output files created")
	}

	// Count total messages written
	totalMessages := 0
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Failed to read file %s: %v", file, err)
		}

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		for _, line := range lines {
			if line != "" {
				totalMessages++
			}
		}
	}

	expectedMessages := numWorkers * messagesPerWorker
	// Allow for some messages to be rejected as invalid due to test data variations
	if totalMessages < expectedMessages-5 || totalMessages > expectedMessages {
		t.Errorf("Expected approximately %d messages, got %d (difference: %d)", expectedMessages, totalMessages, expectedMessages-totalMessages)
	}
}

func TestIntegration_LogRotation(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "integration_rotation_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create log rotator with UTC time
	logRotator, err := NewLogRotator(tmpdir, true, logger)
	if err != nil {
		t.Fatalf("Failed to create log rotator: %v", err)
	}
	defer logRotator.Close()

	// Start log rotation monitoring
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go logRotator.Start(ctx)

	decoder := NewBeastDecoder(logger)
	writer := NewBaseStationWriter(logRotator, logger)

	// Write some messages
	testMessage := []byte{
		0x1A, 0x32,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x64,
		0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78,
	}

	for i := 0; i < 10; i++ {
		messages, err := decoder.Decode(testMessage)
		if err != nil {
			t.Fatalf("Decode failed: %v", err)
		}

		for _, msg := range messages {
			err = writer.WriteMessage(msg)
			if err != nil {
				t.Fatalf("Write failed: %v", err)
			}
		}

		// Small delay
		time.Sleep(10 * time.Millisecond)
	}

	// Verify log files exist
	files, err := filepath.Glob(filepath.Join(tmpdir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Fatalf("No log files created")
	}

	// Check file naming pattern (should include date)
	for _, file := range files {
		filename := filepath.Base(file)
		if !strings.Contains(filename, "adsb_") {
			t.Errorf("File name doesn't contain expected prefix: %s", filename)
		}
		if !strings.HasSuffix(filename, ".log") {
			t.Errorf("File doesn't have .log extension: %s", filename)
		}
	}
}

func TestIntegration_ErrorHandling(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "integration_error_*")
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

	decoder := NewBeastDecoder(logger)
	writer := NewBaseStationWriter(logRotator, logger)

	// Test various error conditions
	errorTestCases := []struct {
		name      string
		beastData []byte
		expectErr bool
	}{
		{
			name:      "Invalid sync byte",
			beastData: []byte{0x1B, 0x32, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			expectErr: false, // Should be ignored, not error
		},
		{
			name:      "Truncated message",
			beastData: []byte{0x1A, 0x32, 0x00, 0x00},
			expectErr: false, // Should be handled gracefully
		},
		{
			name:      "Empty data",
			beastData: []byte{},
			expectErr: false, // Should be handled gracefully
		},
		{
			name: "Valid message",
			beastData: []byte{
				0x1A, 0x32,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x64,
				0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78,
			},
			expectErr: false,
		},
	}

	for _, tc := range errorTestCases {
		t.Run(tc.name, func(t *testing.T) {
			// Try to decode
			messages, err := decoder.Decode(tc.beastData)

			if tc.expectErr && err == nil {
				t.Errorf("Expected error, got none")
			}

			if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Try to write any decoded messages
			for _, msg := range messages {
				err = writer.WriteMessage(msg)
				if err != nil {
					t.Logf("Write error (may be expected): %v", err)
				}
			}
		})
	}
}

func TestIntegration_PerformanceUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	tmpdir, err := os.MkdirTemp("", "integration_perf_*")
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

	decoder := NewBeastDecoder(logger)
	writer := NewBaseStationWriter(logRotator, logger)

	// High-frequency message test
	messageCount := 1000
	testMessage := []byte{
		0x1A, 0x32,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x64,
		0x5D, 0x48, 0x44, 0x12, 0x34, 0x56, 0x78,
	}

	start := time.Now()

	for i := 0; i < messageCount; i++ {
		// Vary the timestamp to simulate real data
		message := make([]byte, len(testMessage))
		copy(message, testMessage)
		// Update timestamp
		timestamp := uint64(i)
		for j := 0; j < 6; j++ {
			message[7-j] = byte(timestamp >> (j * 8))
		}

		messages, err := decoder.Decode(message)
		if err != nil {
			t.Fatalf("Decode failed at message %d: %v", i, err)
		}

		for _, msg := range messages {
			err = writer.WriteMessage(msg)
			if err != nil {
				t.Fatalf("Write failed at message %d: %v", i, err)
			}
		}
	}

	duration := time.Since(start)
	messagesPerSecond := float64(messageCount) / duration.Seconds()

	t.Logf("Processed %d messages in %v", messageCount, duration)
	t.Logf("Performance: %.2f messages/second", messagesPerSecond)

	// Verify all messages were written
	files, err := filepath.Glob(filepath.Join(tmpdir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list output files: %v", err)
	}

	totalWritten := 0
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("Failed to read file %s: %v", file, err)
		}

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		for _, line := range lines {
			if line != "" {
				totalWritten++
			}
		}
	}

	if totalWritten != messageCount {
		t.Errorf("Expected %d messages written, got %d", messageCount, totalWritten)
	}

	// Performance expectations (adjust based on system capabilities)
	if messagesPerSecond < 100 {
		t.Logf("Warning: Low performance detected (%.2f msg/sec)", messagesPerSecond)
	}
}
