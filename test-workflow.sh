#!/bin/bash

# Test GitHub Actions workflow components locally
# This script simulates what GitHub Actions would do

set -e

echo "🚀 Testing GitHub Actions Workflow Locally"
echo "==========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    local status=$1
    local message=$2
    case $status in
        "success") echo -e "${GREEN}✅ $message${NC}" ;;
        "error") echo -e "${RED}❌ $message${NC}" ;;
        "info") echo -e "${YELLOW}ℹ️  $message${NC}" ;;
    esac
}

# Test 1: Check dependencies
print_status "info" "Testing dependency checks..."
if make check-deps; then
    print_status "success" "Dependencies check passed"
else
    print_status "error" "Dependencies check failed"
fi

# Test 2: Build for current platform
print_status "info" "Testing build for current platform..."
if make build; then
    print_status "success" "Build for current platform passed"
else
    print_status "error" "Build for current platform failed"
fi

# Test 3: Run tests
print_status "info" "Testing Go tests..."
if make test; then
    print_status "success" "Go tests passed"
else
    print_status "error" "Go tests failed"
fi

# Test 4: Test binary functionality
print_status "info" "Testing binary functionality..."
if ./go1090 --help > /dev/null 2>&1; then
    print_status "success" "Binary help works"
else
    print_status "error" "Binary help failed"
fi

if ./go1090 --version > /dev/null 2>&1; then
    print_status "success" "Binary version works"
else
    print_status "error" "Binary version failed"
fi

# Test 5: Test with act (if available)
if command -v act &> /dev/null; then
    print_status "info" "Testing with act..."
    if act --job test --container-architecture linux/amd64 --dryrun; then
        print_status "success" "Act dry-run passed"
    else
        print_status "error" "Act dry-run failed"
    fi
else
    print_status "info" "Act not available, skipping container tests"
fi

# Test 6: Simulate release preparation
print_status "info" "Testing release preparation..."
if make release-prep; then
    print_status "success" "Release preparation passed"
else
    print_status "error" "Release preparation failed"
fi

# Test 7: Check code formatting
print_status "info" "Testing code formatting..."
if make fmt && git diff --exit-code; then
    print_status "success" "Code formatting is correct"
else
    print_status "error" "Code formatting issues found"
fi

# Test 8: Run code checks
print_status "info" "Testing code checks..."
if make vet; then
    print_status "success" "Code vet passed"
else
    print_status "error" "Code vet failed"
fi

print_status "info" "All local tests completed!"
echo ""
echo "🐳 To test with Docker containers (like GitHub Actions):"
echo "   act --job test --container-architecture linux/amd64"
echo ""
echo "📋 To test specific matrix combinations:"
echo "   act --matrix os:linux --matrix arch:amd64 --job build"
echo ""
echo "🔧 To test individual steps:"
echo "   act --job test --step 'Set up Go'"
echo ""
echo "📝 To see what would run without executing:"
echo "   act --dryrun" 