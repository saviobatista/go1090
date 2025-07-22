package app

// Default configuration constants
const (
	DefaultFrequency  = 1090000000 // 1090 MHz
	DefaultSampleRate = 2400000    // 2.4 MHz (same as dump1090)
	DefaultGain       = 40         // Manual gain
)

// Config holds application configuration
type Config struct {
	Frequency    uint32
	SampleRate   uint32
	Gain         int
	DeviceIndex  int
	LogDir       string
	LogRotateUTC bool
	Verbose      bool
	ShowVersion  bool
}
