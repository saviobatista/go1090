# Go1090 - ADS-B Beast Mode Decoder

Go1090 is a Go application that captures ADS-B (Automatic Dependent Surveillance-Broadcast) messages from RTL-SDR devices, decodes Beast mode messages, and logs them in BaseStation format with automatic daily log rotation and compression.

## Features

- **RTL-SDR Integration**: Direct interface with RTL2832U USB devices via librtlsdr
- **Beast Mode Decoding**: Decodes Beast mode messages (Mode A/C, Mode S short/long)
- **BaseStation Format**: Outputs logs in industry-standard BaseStation CSV format
- **Auto-Restart**: Automatically restarts on failures for continuous operation
- **Daily Log Rotation**: Rotates logs daily at 00:00 UTC with gzip compression
- **Configurable**: Command-line options for frequency, gain, device selection, etc.

## Requirements

### Hardware
- RTL2832U USB SDR dongle (RTL-SDR)
- Antenna suitable for 1090MHz reception

### Software Dependencies

#### macOS
```bash
# Install librtlsdr using Homebrew
brew install librtlsdr

# Install Go 1.21 or later
brew install go
```

#### Ubuntu/Debian
```bash
# Install librtlsdr development libraries
sudo apt-get update
sudo apt-get install librtlsdr-dev librtlsdr0 rtl-sdr

# Install Go 1.21 or later
sudo apt-get install golang-go
```

#### CentOS/RHEL/Fedora
```bash
# Install librtlsdr development libraries
sudo yum install rtl-sdr-devel rtl-sdr
# Or for newer versions:
sudo dnf install rtl-sdr-devel rtl-sdr

# Install Go 1.21 or later
sudo yum install golang
# Or for newer versions:
sudo dnf install golang
```

#### Windows
```
⚠️ **Limited Support**: Windows builds currently have RTL-SDR hardware support disabled.
The Windows executable will compile and run but cannot connect to RTL-SDR devices.

For full RTL-SDR functionality on Windows, use:
- Windows Subsystem for Linux (WSL) with Linux build
- Docker container with Linux build
- Or help contribute full Windows RTL-SDR support (see Contributing section)
```

## Installation

1. **Clone or download the project**:
   ```bash
   git clone <repository-url>
   cd go1090
   ```

2. **Install Go dependencies**:
   ```bash
   go mod tidy
   ```

3. **Build the application**:
   ```bash
   go build -o go1090
   ```

## Usage

### Basic Usage

```bash
# Run with default settings (1090MHz, device 0)
./go1090

# Run with verbose logging
./go1090 --verbose

# Specify custom frequency and gain
./go1090 --frequency 1090000000 --gain 40

# Use a different RTL-SDR device
./go1090 --device 1

# Custom log directory
./go1090 --log-dir /var/log/adsb
```

### Command Line Options

```
Usage:
  go1090 [flags]

Flags:
  -d, --device int          RTL-SDR device index (default 0)
  -f, --frequency uint32    Frequency to tune to (Hz) (default 1090000000)
  -g, --gain int            Gain setting (default 40)
  -h, --help                help for go1090
  -l, --log-dir string      Log directory (default "./logs")
  -s, --sample-rate uint32  Sample rate (Hz) (default 2000000)
  -u, --utc                 Use UTC for log rotation (default true)
  -v, --verbose             Verbose logging
```

### Finding Your RTL-SDR Device

To list available RTL-SDR devices:
```bash
# Using rtl_test utility
rtl_test

# Or check with lsusb
lsusb | grep RTL
```

## Configuration

### Optimal Settings

For ADS-B reception, the following settings are recommended:

- **Frequency**: 1090000000 Hz (1090MHz - ADS-B frequency)
- **Sample Rate**: 2000000 Hz (2MHz - sufficient for ADS-B)
- **Gain**: 40 (adjust based on your antenna and local environment)

### Gain Settings

- **0**: Automatic gain control (AGC)
- **1-49**: Manual gain settings (device-dependent)
- **40**: Good starting point for most setups

You may need to experiment with gain settings based on your local RF environment and antenna setup.

## Log Format

The application outputs logs in BaseStation format, which is a CSV format with the following fields:

```
MSG,transmission_type,session_id,aircraft_id,hex_ident,flight_id,date_generated,time_generated,date_logged,time_logged,callsign,altitude,ground_speed,track,latitude,longitude,vertical_rate,squawk,alert,emergency,spi,is_on_ground
```

### Example Output

```
MSG,3,1,1,4CA2B6,1,2023/12/07,14:30:25.123,2023/12/07,14:30:25.123,,35000,450,180.5,40.123456,-74.567890,,,,,0,0
MSG,1,1,1,4CA2B6,1,2023/12/07,14:30:26.456,2023/12/07,14:30:26.456,UAL123,,,,,,,,,,,0
```

## Log Rotation

- **Daily Rotation**: Logs are rotated daily at 00:00 UTC (configurable)
- **Compression**: Old logs are automatically compressed with gzip
- **Naming**: Log files are named `adsb_YYYY-MM-DD.log`
- **Compressed**: Old files become `adsb_YYYY-MM-DD.log.gz`

## Error Handling

The application includes robust error handling:

- **Auto-restart**: Automatically restarts on RTL-SDR failures
- **Graceful shutdown**: Handles SIGINT/SIGTERM signals
- **Log rotation**: Continues even if individual components fail
- **Device recovery**: Attempts to reinitialize RTL-SDR on errors

## Troubleshooting

### Common Issues

1. **"no RTL-SDR devices found"**:
   - Ensure RTL-SDR device is plugged in
   - Check that librtlsdr is installed
   - Verify device permissions (may need udev rules)

2. **"failed to open RTL-SDR device"**:
   - Device may be in use by another application
   - Check device permissions
   - Try a different device index

3. **Build errors about librtlsdr**:
   - Ensure librtlsdr-dev is installed
   - Check that CGO is enabled: `go env CGO_ENABLED`

4. **Permission denied errors**:
   - Add user to 'dialout' group (Linux): `sudo usermod -a -G dialout $USER`
   - Create udev rules for RTL-SDR device
   - Run with sudo (not recommended for production)

### Creating udev Rules (Linux)

Create `/etc/udev/rules.d/20-rtlsdr.rules`:
```
SUBSYSTEM=="usb", ATTRS{idVendor}=="0bda", ATTRS{idProduct}=="2832", GROUP="adm", MODE="0666", SYMLINK+="rtl_sdr"
SUBSYSTEM=="usb", ATTRS{idVendor}=="0bda", ATTRS{idProduct}=="2838", GROUP="adm", MODE="0666", SYMLINK+="rtl_sdr"
```

Then reload udev rules:
```bash
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Running as a Service

### systemd Service (Linux)

Create `/etc/systemd/system/go1090.service`:
```ini
[Unit]
Description=Go1090 ADS-B Beast Mode Decoder
After=network.target

[Service]
Type=simple
User=adsb
Group=adsb
WorkingDirectory=/opt/go1090
ExecStart=/opt/go1090/go1090 --log-dir /var/log/go1090
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable go1090
sudo systemctl start go1090
```

### launchd Service (macOS)

Create `~/Library/LaunchAgents/com.go1090.plist`:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.go1090</string>
    <key>ProgramArguments</key>
    <array>
        <string>/path/to/go1090</string>
        <string>--log-dir</string>
        <string>/var/log/go1090</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

Load:
```bash
launchctl load ~/Library/LaunchAgents/com.go1090.plist
```

## Performance Notes

- **CPU Usage**: Moderate CPU usage during active reception
- **Memory**: Low memory footprint (~10-50MB)
- **Disk I/O**: Continuous writing to log files
- **Network**: No network usage (local processing only)

## Contributing

Contributions are welcome! Please feel free to submit pull requests or open issues for bugs and feature requests.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

The MIT License was chosen to maximize adoption in the aviation community and allow both commercial and educational use.

## Acknowledgments

- RTL-SDR project for the excellent SDR library
- ADS-B decoding community for protocol documentation
- Go community for excellent libraries and tools 