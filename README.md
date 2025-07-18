# Go1090 - ADS-B Beast Mode Decoder

A simplified ADS-B decoder written in Go that captures I/Q samples from RTL-SDR devices and attempts to convert them to BaseStation (SBS) format.

## Features

- **RTL-SDR Integration**: Direct integration with RTL-SDR devices using the gortlsdr library
- **Beast Mode Processing**: Decodes Beast mode ADS-B messages  
- **SBS Output**: Converts messages to BaseStation (SBS) format for compatibility with other ADS-B tools
- **Real-time Processing**: Processes I/Q samples in real-time
- **Configurable Parameters**: Adjustable frequency, sample rate, and gain settings

## Prerequisites

### macOS (Apple Silicon/Intel)

1. **Install Homebrew** (if not already installed):
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```

2. **Install librtlsdr**:
   ```bash
   brew install librtlsdr
   ```

3. **Install Go** (if not already installed):
   ```bash
   brew install go
   ```

### Linux

1. **Install librtlsdr development package**:
   ```bash
   # Ubuntu/Debian
   sudo apt-get update
   sudo apt-get install librtlsdr-dev pkg-config

   # CentOS/RHEL/Fedora
   sudo yum install rtl-sdr-devel pkgconfig
   # or
   sudo dnf install rtl-sdr-devel pkgconfig
   ```

## Building

### macOS
```bash
# Clone the repository
git clone <repository-url>
cd go1090

# Use the provided build script
./build.sh
```

### Linux
```bash
# Clone the repository
git clone <repository-url>
cd go1090

# Build with standard go build
go build .
```

## Usage

### Basic Usage
```bash
# Use default settings (1090 MHz, auto gain)
./go1090

# Specify custom frequency and gain
./go1090 --frequency 1090000000 --gain 40 --device 0

# Enable verbose logging
./go1090 --verbose

# Use a different log directory
./go1090 --log-dir /path/to/logs
```

### Command Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `-f, --frequency` | 1090000000 | Frequency to tune to (Hz) |
| `-s, --sample-rate` | 2000000 | Sample rate (Hz) |
| `-g, --gain` | 40 | Gain setting (0 for auto) |
| `-d, --device` | 0 | RTL-SDR device index |
| `-l, --log-dir` | ./logs | Log directory |
| `-u, --utc` | true | Use UTC for log rotation |
| `-v, --verbose` | false | Verbose logging |
| `--version` | false | Show version information |

## Output Format

The tool outputs messages in BaseStation (SBS) format, which is compatible with many ADS-B applications. Messages are logged to files in the specified log directory with automatic rotation.

Example SBS message:
```
MSG,3,111,11111,A12345,FlightID,2023/12/25,10:30:45.123,2023/12/25,10:30:45.123,UAL123,35000,450,180,40.7128,-74.0060,0,0400,0,0,0,0
```

## Current Limitations

⚠️ **Important Note**: This is a simplified implementation with the following limitations:

1. **Basic ADS-B Demodulation**: The current ADS-B demodulation is very basic and may not detect all messages correctly.

2. **No Proper Preamble Detection**: The implementation lacks sophisticated ADS-B preamble detection and framing.

3. **Limited Message Processing**: Only basic Beast mode message processing is implemented.

4. **No Position Decoding**: CPR (Compact Position Reporting) decoding is not fully implemented.

## How It Works

1. **RTL-SDR Capture**: Captures I/Q samples at 2 MHz sample rate on 1090 MHz
2. **Envelope Detection**: Converts I/Q samples to envelope (magnitude) data
3. **Simple Demodulation**: Basic threshold-based bit detection
4. **Beast Mode Processing**: Attempts to decode the demodulated data as Beast mode messages
5. **SBS Conversion**: Converts valid messages to BaseStation format
6. **Logging**: Writes output to rotating log files

## Troubleshooting

### "no RTL-SDR devices found"
- Ensure your RTL-SDR device is connected
- Check that librtlsdr can detect the device: `rtl_test`
- Try running with sudo (may be needed for device access)

### Build Errors
- Ensure librtlsdr is properly installed
- On macOS, make sure you're using the build script that sets proper CGO flags
- Verify Go version compatibility (requires Go 1.21+)

### No Messages Detected
- The current implementation is basic and may miss many messages
- Try adjusting the gain setting
- Ensure you have a proper ADS-B antenna connected
- Consider using established tools like dump1090 for comparison

## For Production Use

For production ADS-B decoding, consider using established tools like:
- **dump1090**: Well-established ADS-B decoder
- **rtl_adsb**: Simple ADS-B decoder included with rtl-sdr
- **readsb**: Modern fork of dump1090

This Go implementation serves as a learning tool and starting point for custom ADS-B processing applications.

## License

This project is provided as-is for educational and experimental purposes. 