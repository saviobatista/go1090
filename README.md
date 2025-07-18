# Go1090 - ADS-B Decoder (dump1090-style)

A Go implementation of an ADS-B decoder that mimics the functionality of dump1090. Captures I/Q samples from RTL-SDR devices, performs proper ADS-B demodulation with preamble detection, validates messages with CRC, and outputs in BaseStation (SBS) format.

## ✨ What's New

This implementation now works **just like dump1090**:

- ✅ **Proper ADS-B Demodulation**: Real PPM (Pulse Position Modulation) demodulation
- ✅ **Preamble Detection**: Detects actual ADS-B preamble patterns
- ✅ **CRC Validation**: Validates messages using ADS-B CRC-24 checksum
- ✅ **Message Parsing**: Extracts callsigns, altitudes, velocities, squawk codes
- ✅ **Real-time Output**: Prints valid messages to stdout like dump1090
- ✅ **SBS Format**: Compatible with existing ADS-B tools and databases

## Features

- **RTL-SDR Integration**: Direct integration with RTL-SDR devices using gortlsdr
- **dump1090-style Processing**: Implements the same demodulation approach as dump1090
- **Real-time Decoding**: Processes I/Q samples in real-time with proper timing
- **Message Validation**: CRC-24 validation ensures message integrity
- **Multiple Output**: Both file logging and stdout output (like dump1090)
- **Statistics Reporting**: Shows processing statistics every 30 seconds

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
# Use default settings (1090 MHz, manual gain 40)
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

The tool outputs messages in BaseStation (SBS) format, which is compatible with many ADS-B applications. Messages are both:
- **Printed to stdout** (like dump1090)
- **Logged to files** with automatic rotation

Example SBS output:
```
MSG,1,1,1,4CA2B6,1,2023/12/25,10:30:45.123,2023/12/25,10:30:45.123,UAL123,,,,,,,,,,,0
MSG,3,1,1,4CA2B6,1,2023/12/25,10:30:46.456,2023/12/25,10:30:46.456,,35000,,,,,,,,,,,0
MSG,4,1,1,4CA2B6,1,2023/12/25,10:30:47.789,2023/12/25,10:30:47.789,,,450,180.5,,,2048,,,,,0
```

### Message Types

| Type | Description | Fields |
|------|-------------|--------|
| MSG,1 | Aircraft Identification | Callsign |
| MSG,3 | Airborne Position | Altitude, Position (when available) |
| MSG,4 | Airborne Velocity | Ground Speed, Track, Vertical Rate |
| MSG,5 | Surveillance | Altitude, Squawk Code |

## How It Works (dump1090-style)

1. **RTL-SDR Capture**: Captures I/Q samples at 2 MHz sample rate on 1090 MHz
2. **Envelope Detection**: Converts I/Q samples to envelope (magnitude) data
3. **Adaptive Thresholding**: Dynamically adjusts detection threshold based on signal levels
4. **Preamble Detection**: Scans for the specific ADS-B preamble pattern (high at 0, 2, 7, 9 μs)
5. **PPM Demodulation**: Uses Pulse Position Modulation to extract 112-bit messages
6. **CRC Validation**: Validates each message using ADS-B CRC-24 polynomial (0xFFF409)
7. **Message Parsing**: Extracts information based on Downlink Format and Type Code
8. **SBS Output**: Converts to BaseStation format and outputs to stdout/files

## Comparison with dump1090

| Feature | dump1090 | go1090 | Status |
|---------|----------|--------|--------|
| RTL-SDR Support | ✅ | ✅ | Complete |
| Preamble Detection | ✅ | ✅ | Complete |
| PPM Demodulation | ✅ | ✅ | Complete |
| CRC Validation | ✅ | ✅ | Complete |
| Message Parsing | ✅ | ✅ | Basic |
| SBS Output | ✅ | ✅ | Complete |
| Position Decoding (CPR) | ✅ | ❌ | Not implemented |
| Web Interface | ✅ | ❌ | Not implemented |
| Network Clients | ✅ | ❌ | Not implemented |

## Real ADS-B Reception

With your RTL-SDR and ADS-B antenna connected:

```bash
# Start receiving ADS-B messages
./go1090 --verbose

# You should see output like:
# MSG,1,1,1,A12345,1,2023/12/25,10:30:45.123,2023/12/25,10:30:45.123,UAL123,,,,,,,,,,,0
# MSG,3,1,1,A12345,1,2023/12/25,10:30:46.456,2023/12/25,10:30:46.456,,35000,,,,,,,,,,,0
```

### Expected Performance

With a good antenna setup, you should expect:
- **Detection range**: 100-300+ km depending on antenna height and aircraft altitude
- **Message rate**: 50-1000+ messages per minute in busy airspace
- **Message types**: Aircraft identification, position, velocity, surveillance

## Troubleshooting

### No Messages Detected
- **Check antenna connection**: Ensure antenna is properly connected to RTL-SDR
- **Verify antenna tuning**: Should be tuned for 1090 MHz
- **Adjust gain**: Try different gain settings (`--gain 20` to `--gain 50`)
- **Check frequency**: Ensure using exactly 1090000000 Hz
- **Test with rtl_test**: Verify RTL-SDR is working: `rtl_test`

### Poor Reception
- **Antenna placement**: Higher is better, avoid obstacles
- **Antenna type**: Use a proper 1090 MHz ADS-B antenna
- **Gain settings**: Too high can cause overload, too low misses weak signals
- **Interference**: Move away from WiFi routers, computers, other electronics

### Build Errors
- Ensure librtlsdr is properly installed
- On macOS, use the build script that sets proper CGO flags
- Verify Go version compatibility (requires Go 1.21+)

## Performance Tips

1. **Optimal Gain**: Start with gain 40, adjust based on your environment
2. **Antenna Height**: Higher antenna placement significantly improves range
3. **Antenna Type**: Use a dedicated 1090 MHz antenna for best results
4. **Ground Plane**: Ensure proper ground plane for antenna
5. **Interference**: Keep away from strong RF sources

## Comparison with Other Tools

### Use go1090 when:
- Learning ADS-B protocol implementation
- Integrating ADS-B into Go applications
- Customizing message processing
- Educational purposes

### Use dump1090 when:
- Production ADS-B decoding
- Need web interface
- Feeding ADS-B networks
- Maximum performance required

## Future Enhancements

Planned improvements:
- [ ] CPR position decoding
- [ ] Web interface
- [ ] Network client support
- [ ] Beast mode output
- [ ] JSON output format
- [ ] Statistics web page

## License

This project is provided as-is for educational and experimental purposes. 