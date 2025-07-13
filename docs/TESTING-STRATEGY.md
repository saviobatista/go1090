# Testing Strategy for Go1090

This document outlines the comprehensive testing approach for the Go1090 ADS-B Beast Mode Decoder.

## Overview

Go1090 presents unique testing challenges due to:
- **Hardware dependencies** (RTL-SDR devices)
- **Real-time data processing** (Beast mode messages)
- **CGO bindings** (librtlsdr)
- **Concurrent operations** (goroutines, channels)
- **File I/O** (log rotation, BaseStation output)
- **Signal handling** (graceful shutdown)
- **Protocol compliance** (Beast mode, BaseStation format)

## Testing Pyramid

```
    ┌─────────────────┐
    │   E2E Tests     │  ← Few, expensive, realistic
    │   (Hardware)    │
    ├─────────────────┤
    │ Integration     │  ← Some, medium cost
    │ Tests           │
    ├─────────────────┤
    │   Unit Tests    │  ← Many, fast, isolated
    └─────────────────┘
```

## 1. Unit Tests

### What to Test
- **Protocol parsing** (Beast mode decoder)
- **Data transformation** (Beast to BaseStation)
- **Utility functions** (time conversion, logging)
- **Configuration handling** (CLI flags, validation)
- **Individual component logic**

### Coverage Goals
- **80%+ code coverage** for business logic
- **100% coverage** for critical parsing functions
- **Edge cases** and error conditions

### Test Structure
```go
func TestBeastModeDecoder(t *testing.T) {
    tests := []struct {
        name     string
        input    []byte
        expected Message
        wantErr  bool
    }{
        // Test cases
    }
}
```

## 2. Integration Tests

### What to Test
- **Component interactions** (parser → formatter → writer)
- **File operations** (log rotation, BaseStation writing)
- **Configuration integration** (CLI → components)
- **Error propagation** between components
- **Graceful shutdown** scenarios

### Mock Hardware
- **Simulated RTL-SDR** device responses
- **Fake USB device** interactions
- **Controlled data streams**

## 3. Protocol Compliance Tests

### Beast Mode Protocol
- **Message format validation**
- **Timestamp handling**
- **CRC verification**
- **Multiple message types** (Mode A/C, Mode S)
- **Malformed message handling**

### BaseStation Format
- **CSV format compliance**
- **Field accuracy**
- **Timestamp conversion**
- **Special character handling**

## 4. Hardware Simulation Tests

### Simulated Environments
- **No RTL-SDR device** present
- **Device disconnection** during operation
- **USB permission** issues
- **Multiple devices** available

### Data Injection
- **Pre-recorded** Beast mode data
- **Synthetic** message generation
- **Stress testing** with high message rates

## 5. Performance Tests

### Throughput Testing
- **Message processing rate**
- **Memory usage** under load
- **CPU utilization**
- **File I/O performance**

### Stress Testing
- **High message volume**
- **Extended runtime**
- **Memory leak detection**
- **Resource cleanup**

## 6. Error Handling Tests

### Hardware Errors
- **Device not found**
- **USB disconnection**
- **Permission denied**
- **Device busy**

### Data Errors
- **Malformed messages**
- **Incomplete data**
- **CRC failures**
- **Timestamp issues**

### System Errors
- **Disk full**
- **File permission errors**
- **Signal interruption**
- **Out of memory**

## Test Implementation

### Test Structure

```
go1090/
├── beast_test.go               # Protocol parsing tests
├── basestation_test.go         # Format conversion tests
├── logrotator_test.go          # File rotation tests
├── integration_test.go         # End-to-end pipeline tests
├── tests/
│   └── testdata/
│       ├── beast_messages.bin      # Sample Beast mode data
│       ├── basestation_expected.csv # Expected output samples
│       └── malformed_data.bin      # Error condition data
└── main.go                     # Application entry point
```

**Note**: Test files are co-located with the main package code (Go best practice for main package testing). The `tests/testdata/` directory contains sample data files for testing scenarios.

### Running Tests

#### Basic Testing
```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests only
make test-integration

# Run with coverage
make test-coverage
```

#### Advanced Testing
```bash
# Race condition detection
make test-race

# Performance benchmarks
make test-bench

# Memory/CPU profiling
make test-profile

# Timeout protection
make test-timeout
```

### Test Data Sources

#### Real Beast Mode Data
- **dump1090 captures**: Use actual RTL-SDR captures
- **FlightAware samples**: Known good baseline data
- **Synthetic generation**: Controlled test scenarios

#### Error Condition Data
- **Malformed messages**: Invalid sync bytes, truncated data
- **Edge cases**: Boundary conditions, unusual timestamps
- **Hardware failures**: Simulated USB disconnections

### Mock/Stub Strategy

#### Hardware Mocking
```go
type MockRTLSDR struct {
    data []byte
    pos  int
}

func (m *MockRTLSDR) Read(p []byte) (int, error) {
    // Simulate RTL-SDR data stream
}
```

#### File System Mocking
```go
type MockFileSystem struct {
    files map[string][]byte
}

func (m *MockFileSystem) WriteFile(name string, data []byte) error {
    // Simulate file operations
}
```

### Continuous Integration

#### GitHub Actions Integration
```yaml
name: Test Suite
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v4
    - run: make test-coverage
    - run: make test-race
    - run: make test-bench
```

### Test Metrics & Goals

#### Coverage Targets
- **Unit Tests**: 85%+ code coverage
- **Integration Tests**: 70%+ system coverage
- **Critical Paths**: 100% coverage (parsing, file I/O)

#### Performance Benchmarks
- **Message Processing**: >1000 messages/second
- **Memory Usage**: <50MB steady state
- **File I/O**: <10ms average write latency

#### Quality Gates
- **Zero race conditions** detected
- **No memory leaks** in 24-hour runs
- **Graceful degradation** under load

### Test Automation

#### Pre-commit Hooks
```bash
#!/bin/bash
# .git/hooks/pre-commit
make test-short
make test-race
```

#### Release Testing
```bash
# Full test suite before release
make test-coverage
make test-integration
make test-bench
make test-timeout
```

### Troubleshooting Tests

#### Common Issues
1. **Timing-dependent tests**: Use `testing.Short()` for CI
2. **Resource cleanup**: Always defer cleanup in tests
3. **Flaky tests**: Use `make test-count` to verify stability

#### Debug Tools
```bash
# Verbose test output
make test-verbose

# CPU profiling
make test-profile
go tool pprof cpu.prof

# Memory profiling  
go tool pprof mem.prof
```

### Test Environment Setup

#### Development Environment
```bash
# Install test dependencies
sudo apt-get install librtlsdr-dev pkg-config

# Create test data directory
mkdir -p testdata
```

#### CI Environment
```yaml
# GitHub Actions setup
- name: Install RTL-SDR
  run: |
    sudo apt-get update
    sudo apt-get install librtlsdr-dev pkg-config
```

### Best Practices

#### Test Organization
1. **Arrange-Act-Assert**: Structure all tests consistently
2. **Table-driven tests**: Use for multiple similar cases
3. **Descriptive names**: Clear test and subtest names
4. **Isolated tests**: No dependencies between tests

#### Test Data Management
1. **Deterministic data**: Avoid randomness in tests
2. **Version control**: Include test data in repository
3. **Data cleanup**: Remove temporary files after tests
4. **Realistic scenarios**: Use actual aviation data patterns

#### Performance Considerations
1. **Benchmark baseline**: Establish performance expectations
2. **Memory profiling**: Monitor for leaks and growth
3. **Timeout protection**: Prevent hanging tests
4. **Resource limits**: Test under constrained conditions

This comprehensive testing strategy ensures Go1090 maintains high quality, reliability, and performance standards suitable for critical aviation applications. 