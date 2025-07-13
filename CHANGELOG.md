# Changelog

All notable changes to Go1090 will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Nothing yet

### Changed
- Nothing yet

### Fixed
- Nothing yet

## [1.0.0] - TBD

### Added
- Initial release of Go1090 ADS-B Beast Mode Decoder
- RTL-SDR integration with librtlsdr
- Beast mode message decoding (Mode A/C, Mode S short/long)
- BaseStation format CSV output
- Daily log rotation with UTC timing
- Automatic gzip compression of rotated logs
- Auto-restart functionality on device failures
- Graceful shutdown handling (SIGINT/SIGTERM)
- Cross-platform support (Linux, macOS, Windows*)
- Command-line interface with configurable options:
  - Frequency tuning (default 1090MHz)
  - Sample rate configuration (default 2MHz)
  - Gain control (manual/automatic)
  - Device selection (multiple RTL-SDR support)
  - Custom log directory
  - UTC/local time for log rotation
  - Verbose logging mode
- Comprehensive error handling and logging
- Version information display

### Technical Features
- Concurrent goroutines for data processing
- Thread-safe device management
- Memory-efficient buffer handling
- Signal strength extraction
- ICAO address extraction
- Aircraft callsign decoding (basic)
- Squawk code extraction
- Altitude and velocity decoding (basic)

### Documentation
- Complete README with installation instructions
- Platform-specific dependency installation guides
- Troubleshooting section
- Configuration examples
- systemd and launchd service examples
- MIT License for maximum compatibility

### Build System
- Go modules support
- GitHub Actions CI/CD
- Multi-platform automated builds
- Static linking for Linux
- Cross-compilation support

### Known Limitations
- Windows builds have limited RTL-SDR support (CGO complexity)
- Position decoding not fully implemented (requires CPR decoding)
- No built-in web interface
- No network distribution of data

---

## Release Notes Template

### [X.Y.Z] - YYYY-MM-DD

#### Added
- New features

#### Changed
- Changes in existing functionality

#### Deprecated
- Soon-to-be removed features

#### Removed
- Now removed features

#### Fixed
- Bug fixes

#### Security
- Security improvements

---

## Links

- [Compare v1.0.0...HEAD](https://github.com/[username]/go1090/compare/v1.0.0...HEAD)
- [Compare v0.9.0...v1.0.0](https://github.com/[username]/go1090/compare/v0.9.0...v1.0.0) 