package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LogRotator handles log rotation with gzip compression
type LogRotator struct {
	logDir      string
	useUTC      bool
	logger      *logrus.Logger
	currentFile *os.File
	currentDate string
	mutex       sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewLogRotator creates a new log rotator
func NewLogRotator(logDir string, useUTC bool, logger *logrus.Logger) (*LogRotator, error) {
	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	rotator := &LogRotator{
		logDir: logDir,
		useUTC: useUTC,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize current log file
	if err := rotator.rotateLogFile(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize log file: %w", err)
	}

	return rotator, nil
}

// Start starts the log rotation scheduler
func (r *LogRotator) Start(ctx context.Context) {
	r.logger.Info("Starting log rotator")

	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("Log rotator stopping")
			return
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.checkRotation()
		}
	}
}

// checkRotation checks if log rotation is needed
func (r *LogRotator) checkRotation() {
	var now time.Time
	if r.useUTC {
		now = time.Now().UTC()
	} else {
		now = time.Now()
	}

	currentDate := now.Format("2006-01-02")

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.currentDate != currentDate {
		r.logger.WithFields(logrus.Fields{
			"old_date": r.currentDate,
			"new_date": currentDate,
		}).Info("Rotating log file")

		if err := r.rotateLogFile(); err != nil {
			r.logger.WithError(err).Error("Failed to rotate log file")
		}
	}
}

// rotateLogFile performs log rotation
func (r *LogRotator) rotateLogFile() error {
	var now time.Time
	if r.useUTC {
		now = time.Now().UTC()
	} else {
		now = time.Now()
	}

	newDate := now.Format("2006-01-02")

	// Close current file if it exists
	if r.currentFile != nil {
		oldFile := r.currentFile
		oldDate := r.currentDate

		// Close the file
		if err := oldFile.Close(); err != nil {
			r.logger.WithError(err).Error("Failed to close old log file")
		}

		// Compress the old file in a goroutine
		go r.compressLogFile(oldDate)
	}

	// Create new log file
	filename := fmt.Sprintf("adsb_%s.log", newDate)
	filepath := filepath.Join(r.logDir, filename)

	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file %s: %w", filepath, err)
	}

	r.currentFile = file
	r.currentDate = newDate

	r.logger.WithField("file", filepath).Info("Created new log file")

	return nil
}

// compressLogFile compresses a log file with gzip
func (r *LogRotator) compressLogFile(date string) {
	logFile := filepath.Join(r.logDir, fmt.Sprintf("adsb_%s.log", date))
	gzipFile := filepath.Join(r.logDir, fmt.Sprintf("adsb_%s.log.gz", date))

	r.logger.WithFields(logrus.Fields{
		"source": logFile,
		"target": gzipFile,
	}).Info("Compressing log file")

	// Check if source file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		r.logger.WithField("file", logFile).Debug("Log file doesn't exist, skipping compression")
		return
	}

	// Open source file
	src, err := os.Open(logFile)
	if err != nil {
		r.logger.WithError(err).WithField("file", logFile).Error("Failed to open source file for compression")
		return
	}
	defer src.Close()

	// Create compressed file
	dst, err := os.Create(gzipFile)
	if err != nil {
		r.logger.WithError(err).WithField("file", gzipFile).Error("Failed to create compressed file")
		return
	}
	defer dst.Close()

	// Create gzip writer
	gzWriter := gzip.NewWriter(dst)
	defer gzWriter.Close()

	// Set file header
	gzWriter.Name = filepath.Base(logFile)
	gzWriter.ModTime = time.Now()

	// Copy data
	if _, err := io.Copy(gzWriter, src); err != nil {
		r.logger.WithError(err).Error("Failed to compress log file")
		return
	}

	// Close gzip writer to flush data
	if err := gzWriter.Close(); err != nil {
		r.logger.WithError(err).Error("Failed to close gzip writer")
		return
	}

	// Close destination file
	if err := dst.Close(); err != nil {
		r.logger.WithError(err).Error("Failed to close compressed file")
		return
	}

	// Remove original file
	if err := os.Remove(logFile); err != nil {
		r.logger.WithError(err).WithField("file", logFile).Error("Failed to remove original log file")
		return
	}

	r.logger.WithField("file", gzipFile).Info("Log file compressed successfully")
}

// GetWriter returns the current log writer
func (r *LogRotator) GetWriter() (io.Writer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if r.currentFile == nil {
		return nil, fmt.Errorf("no current log file")
	}

	return r.currentFile, nil
}

// Close closes the log rotator
func (r *LogRotator) Close() error {
	r.logger.Info("Closing log rotator")

	r.cancel()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.currentFile != nil {
		if err := r.currentFile.Close(); err != nil {
			r.logger.WithError(err).Error("Failed to close current log file")
			return err
		}
		r.currentFile = nil
	}

	return nil
}

// GetCurrentLogFile returns the current log file path
func (r *LogRotator) GetCurrentLogFile() string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if r.currentDate == "" {
		return ""
	}

	return filepath.Join(r.logDir, fmt.Sprintf("adsb_%s.log", r.currentDate))
}

// GetLogFiles returns a list of all log files (including compressed ones)
func (r *LogRotator) GetLogFiles() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(r.logDir, "adsb_*.log*"))
	if err != nil {
		return nil, fmt.Errorf("failed to list log files: %w", err)
	}

	return files, nil
}

// CleanupOldLogs removes log files older than the specified number of days
func (r *LogRotator) CleanupOldLogs(maxDays int) error {
	if maxDays <= 0 {
		return fmt.Errorf("maxDays must be positive")
	}

	files, err := r.GetLogFiles()
	if err != nil {
		return fmt.Errorf("failed to get log files: %w", err)
	}

	var cutoff time.Time
	if r.useUTC {
		cutoff = time.Now().UTC().AddDate(0, 0, -maxDays)
	} else {
		cutoff = time.Now().AddDate(0, 0, -maxDays)
	}

	removed := 0
	for _, file := range files {
		// Skip current log file
		if file == r.GetCurrentLogFile() {
			continue
		}

		// Get file info
		info, err := os.Stat(file)
		if err != nil {
			r.logger.WithError(err).WithField("file", file).Warn("Failed to stat log file")
			continue
		}

		// Check if file is old enough to remove
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(file); err != nil {
				r.logger.WithError(err).WithField("file", file).Error("Failed to remove old log file")
			} else {
				r.logger.WithField("file", file).Info("Removed old log file")
				removed++
			}
		}
	}

	r.logger.WithField("count", removed).Info("Cleaned up old log files")
	return nil
}
