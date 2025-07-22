package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogRotator_NewLogRotator tests the creation of new log rotator
func TestLogRotator_NewLogRotator(t *testing.T) {
	tests := []struct {
		name    string
		logDir  string
		useUTC  bool
		wantErr bool
	}{
		{
			name:    "Valid directory creation",
			logDir:  "test_logs",
			useUTC:  false,
			wantErr: false,
		},
		{
			name:    "UTC timezone",
			logDir:  "test_logs_utc",
			useUTC:  true,
			wantErr: false,
		},
		{
			name:    "Nested directory creation",
			logDir:  "nested/test/logs",
			useUTC:  false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up before and after test
			defer os.RemoveAll(tt.logDir)
			os.RemoveAll(tt.logDir)

			logger := logrus.New()
			logger.SetOutput(io.Discard) // Suppress log output during tests

			rotator, err := NewLogRotator(tt.logDir, tt.useUTC, logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, rotator)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rotator)
			defer rotator.Close()

			// Verify directory was created
			assert.DirExists(t, tt.logDir)

			// Verify log file was created
			writer, err := rotator.GetWriter()
			assert.NoError(t, err)
			assert.NotNil(t, writer)

			// Verify current log file exists
			currentFile := rotator.GetCurrentLogFile()
			assert.NotEmpty(t, currentFile)
			assert.FileExists(t, currentFile)
		})
	}
}

// TestLogRotator_GetWriter tests the GetWriter method
func TestLogRotator_GetWriter(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	writer, err := rotator.GetWriter()
	require.NoError(t, err)
	require.NotNil(t, writer)

	// Test writing to the writer
	testData := "Test log entry\n"
	n, err := writer.Write([]byte(testData))
	assert.NoError(t, err)
	assert.Equal(t, len(testData), n)

	// Verify data was written to file
	currentFile := rotator.GetCurrentLogFile()
	content, err := os.ReadFile(currentFile)
	assert.NoError(t, err)
	assert.Equal(t, testData, string(content))
}

// TestLogRotator_GetLogFiles tests the GetLogFiles method
func TestLogRotator_GetLogFiles(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Create additional test files
	testFiles := []string{
		"adsb_2023-01-01.log",
		"adsb_2023-01-02.log.gz",
		"adsb_2023-01-03.log",
	}

	for _, filename := range testFiles {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	files, err := rotator.GetLogFiles()
	require.NoError(t, err)

	// Should include current log file plus test files
	assert.GreaterOrEqual(t, len(files), len(testFiles))

	// Check that all test files are included
	fileSet := make(map[string]bool)
	for _, file := range files {
		fileSet[filepath.Base(file)] = true
	}

	for _, testFile := range testFiles {
		assert.True(t, fileSet[testFile], "Expected file %s not found", testFile)
	}
}

// TestLogRotator_CleanupOldLogs tests the CleanupOldLogs method
func TestLogRotator_CleanupOldLogs(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Create old log files with different timestamps
	oldFile := filepath.Join(tempDir, "adsb_2023-01-01.log")
	err = os.WriteFile(oldFile, []byte("old content"), 0644)
	require.NoError(t, err)

	// Set old modification time
	oldTime := time.Now().AddDate(0, 0, -10) // 10 days ago
	err = os.Chtimes(oldFile, oldTime, oldTime)
	require.NoError(t, err)

	// Create recent log file
	recentFile := filepath.Join(tempDir, "adsb_2023-12-31.log")
	err = os.WriteFile(recentFile, []byte("recent content"), 0644)
	require.NoError(t, err)

	// Test cleanup with maxDays = 5
	err = rotator.CleanupOldLogs(5)
	assert.NoError(t, err)

	// Old file should be removed
	assert.NoFileExists(t, oldFile)

	// Recent file should still exist
	assert.FileExists(t, recentFile)

	// Current log file should still exist
	currentFile := rotator.GetCurrentLogFile()
	assert.FileExists(t, currentFile)
}

// TestLogRotator_CleanupOldLogs_InvalidMaxDays tests error handling
func TestLogRotator_CleanupOldLogs_InvalidMaxDays(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Test with invalid maxDays
	err = rotator.CleanupOldLogs(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maxDays must be positive")

	err = rotator.CleanupOldLogs(-1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maxDays must be positive")
}

// TestLogRotator_Close tests the Close method
func TestLogRotator_Close(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)

	// Write some data
	writer, err := rotator.GetWriter()
	require.NoError(t, err)
	_, err = writer.Write([]byte("test data"))
	require.NoError(t, err)

	// Close should work without error
	err = rotator.Close()
	assert.NoError(t, err)

	// After closing, GetWriter should return error
	writer, err = rotator.GetWriter()
	assert.Error(t, err)
	assert.Nil(t, writer)
}

// TestLogRotator_CompressLogFile tests compression functionality
func TestLogRotator_CompressLogFile(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Create test log file
	testDate := "2023-01-01"
	testFile := filepath.Join(tempDir, fmt.Sprintf("adsb_%s.log", testDate))
	testContent := "Test log content\nLine 2\nLine 3\n"
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	require.NoError(t, err)

	// Call compress function
	rotator.compressLogFile(testDate)

	// Wait a bit for compression to complete
	time.Sleep(100 * time.Millisecond)

	// Original file should be removed
	assert.NoFileExists(t, testFile)

	// Compressed file should exist
	compressedFile := filepath.Join(tempDir, fmt.Sprintf("adsb_%s.log.gz", testDate))
	assert.FileExists(t, compressedFile)

	// Verify compressed content
	gzFile, err := os.Open(compressedFile)
	require.NoError(t, err)
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	require.NoError(t, err)
	defer gzReader.Close()

	decompressed, err := io.ReadAll(gzReader)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(decompressed))
}

// TestLogRotator_DateRotation tests date-based log rotation
func TestLogRotator_DateRotation(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Get initial log file
	initialFile := rotator.GetCurrentLogFile()
	assert.NotEmpty(t, initialFile)

	// Write some data
	writer, err := rotator.GetWriter()
	require.NoError(t, err)
	_, err = writer.Write([]byte("initial content"))
	require.NoError(t, err)

	// Manually trigger rotation (simulating date change)
	err = rotator.rotateLogFile()
	assert.NoError(t, err)

	// Current file should be the same (since date hasn't actually changed)
	currentFile := rotator.GetCurrentLogFile()
	assert.Equal(t, initialFile, currentFile)

	// File should still exist and be writable
	writer, err = rotator.GetWriter()
	assert.NoError(t, err)
	_, err = writer.Write([]byte("new content"))
	assert.NoError(t, err)
}

// TestLogRotator_ConcurrentAccess tests concurrent access to log rotator
func TestLogRotator_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Test concurrent writes and reads
	done := make(chan bool)
	numGoroutines := 10
	numOps := 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOps; j++ {
				// Concurrent GetWriter calls
				writer, err := rotator.GetWriter()
				if err != nil {
					t.Errorf("GetWriter failed: %v", err)
					return
				}

				// Write some data
				data := fmt.Sprintf("goroutine-%d-op-%d\n", id, j)
				_, err = writer.Write([]byte(data))
				if err != nil {
					t.Errorf("Write failed: %v", err)
					return
				}

				// Concurrent GetCurrentLogFile calls
				currentFile := rotator.GetCurrentLogFile()
				if currentFile == "" {
					t.Error("GetCurrentLogFile returned empty string")
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify log file exists and has content
	currentFile := rotator.GetCurrentLogFile()
	assert.FileExists(t, currentFile)

	content, err := os.ReadFile(currentFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, content)

	// Should contain entries from all goroutines
	contentStr := string(content)
	assert.Contains(t, contentStr, "goroutine-0-op-0")
	assert.Contains(t, contentStr, fmt.Sprintf("goroutine-%d-op-%d", numGoroutines-1, numOps-1))
}

// TestLogRotator_UTCTimezone tests UTC timezone handling
func TestLogRotator_UTCTimezone(t *testing.T) {
	tempDir := t.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, true, logger)
	require.NoError(t, err)
	defer rotator.Close()

	// Current file should exist
	currentFile := rotator.GetCurrentLogFile()
	assert.NotEmpty(t, currentFile)
	assert.FileExists(t, currentFile)

	// File name should contain current UTC date
	expectedDate := time.Now().UTC().Format("2006-01-02")
	assert.Contains(t, currentFile, expectedDate)
}

// BenchmarkLogRotator_Write benchmarks writing performance
func BenchmarkLogRotator_Write(b *testing.B) {
	tempDir := b.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(b, err)
	defer rotator.Close()

	writer, err := rotator.GetWriter()
	require.NoError(b, err)

	data := []byte("benchmark test data\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := writer.Write(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLogRotator_GetWriter benchmarks GetWriter performance
func BenchmarkLogRotator_GetWriter(b *testing.B) {
	tempDir := b.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(b, err)
	defer rotator.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer, err := rotator.GetWriter()
		if err != nil {
			b.Fatal(err)
		}
		if writer == nil {
			b.Fatal("writer is nil")
		}
	}
}

// BenchmarkLogRotator_GetLogFiles benchmarks GetLogFiles performance
func BenchmarkLogRotator_GetLogFiles(b *testing.B) {
	tempDir := b.TempDir()
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	rotator, err := NewLogRotator(tempDir, false, logger)
	require.NoError(b, err)
	defer rotator.Close()

	// Create some test files
	for i := 0; i < 10; i++ {
		filename := fmt.Sprintf("adsb_2023-01-%02d.log", i+1)
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte("test"), 0644)
		require.NoError(b, err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		files, err := rotator.GetLogFiles()
		if err != nil {
			b.Fatal(err)
		}
		if len(files) == 0 {
			b.Fatal("no files returned")
		}
	}
}
