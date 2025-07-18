#!/bin/bash

# Build script for go1090 with RTL-SDR support
# This sets the proper CGO flags for macOS with Homebrew

set -e

echo "Building go1090 with RTL-SDR support..."

# Set CGO flags for Homebrew on macOS
export CGO_CFLAGS="-I/opt/homebrew/include"
export CGO_LDFLAGS="-L/opt/homebrew/lib"

# Build the project
go build -v .

echo "Build completed successfully!"
echo "To run: ./go1090 --help" 