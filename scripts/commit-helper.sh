#!/bin/bash

# Commit Helper Script
# Helps create properly formatted commit messages for automatic version bumping

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 Commit Helper - Conventional Commits${NC}"
echo -e "${BLUE}====================================${NC}"
echo ""

# Function to print colored output
print_info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Show current status
print_info "Current git status:"
git status --short

echo ""
print_info "Conventional commit types:"
echo "  feat:     New feature (triggers MINOR version bump)"
echo "  fix:      Bug fix (triggers PATCH version bump)"
echo "  docs:     Documentation changes (triggers PATCH version bump)"
echo "  style:    Code style changes (triggers PATCH version bump)"
echo "  refactor: Code refactoring (triggers PATCH version bump)"
echo "  test:     Test changes (triggers PATCH version bump)"
echo "  chore:    Maintenance tasks (triggers PATCH version bump)"
echo "  BREAKING CHANGE: or !: Breaking changes (triggers MAJOR version bump)"

echo ""
print_info "Examples:"
echo "  feat: add new RTL-SDR device support"
echo "  fix: resolve Beast mode parsing issue"
echo "  feat!: change CLI interface (breaking change)"
echo "  docs: update installation instructions"

echo ""

# Get commit type
echo -e "${BLUE}Select commit type:${NC}"
echo "1) feat (new feature)"
echo "2) fix (bug fix)"
echo "3) docs (documentation)"
echo "4) style (formatting, etc.)"
echo "5) refactor (code refactoring)"
echo "6) test (testing)"
echo "7) chore (maintenance)"
echo "8) custom (enter manually)"

read -p "Enter choice (1-8): " choice

case $choice in
    1) commit_type="feat" ;;
    2) commit_type="fix" ;;
    3) commit_type="docs" ;;
    4) commit_type="style" ;;
    5) commit_type="refactor" ;;
    6) commit_type="test" ;;
    7) commit_type="chore" ;;
    8) read -p "Enter commit type: " commit_type ;;
    *) print_error "Invalid choice"; exit 1 ;;
esac

# Check if breaking change
read -p "Is this a breaking change? (y/n): " breaking
if [[ $breaking =~ ^[Yy]$ ]]; then
    commit_type="${commit_type}!"
fi

# Get scope (optional)
read -p "Enter scope (optional, e.g., 'parser', 'cli', 'build'): " scope
if [ -n "$scope" ]; then
    commit_type="${commit_type}(${scope})"
fi

# Get commit message
read -p "Enter commit message: " message

# Construct full commit message
if [ -n "$message" ]; then
    full_message="${commit_type}: ${message}"
else
    print_error "Commit message cannot be empty"
    exit 1
fi

# Get body (optional)
echo ""
print_info "Enter commit body (optional, press Enter on empty line to finish):"
body=""
while IFS= read -r line; do
    if [ -z "$line" ]; then
        break
    fi
    body="${body}${line}\n"
done

# Get footer (optional)
read -p "Enter footer (optional, e.g., 'Closes #123'): " footer

# Construct complete commit message
complete_message="$full_message"
if [ -n "$body" ]; then
    complete_message="${complete_message}\n\n${body}"
fi
if [ -n "$footer" ]; then
    complete_message="${complete_message}\n\n${footer}"
fi

# Show preview
echo ""
print_info "Commit message preview:"
echo -e "${GREEN}${complete_message}${NC}"

# Confirm and commit
echo ""
read -p "Commit with this message? (y/n): " confirm
if [[ $confirm =~ ^[Yy]$ ]]; then
    echo -e "$complete_message" | git commit -F -
    print_success "Committed successfully!"
    
    # Show what version bump this will trigger
    if [[ $commit_type == *"!"* ]] || [[ $body == *"BREAKING CHANGE"* ]]; then
        print_info "This commit will trigger a MAJOR version bump (e.g., 1.0.0 -> 2.0.0)"
    elif [[ $commit_type == "feat"* ]]; then
        print_info "This commit will trigger a MINOR version bump (e.g., 1.0.0 -> 1.1.0)"
    else
        print_info "This commit will trigger a PATCH version bump (e.g., 1.0.0 -> 1.0.1)"
    fi
else
    print_error "Commit cancelled"
    exit 1
fi 