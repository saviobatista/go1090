#!/bin/bash

# Test Auto-Release Workflow
# Verifies that the auto-release system is working correctly

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Testing Auto-Release System${NC}"
echo -e "${BLUE}=============================${NC}"
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

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    print_error "Not in a git repository"
    exit 1
fi

print_info "Testing auto-release system..."

# Test 1: Check if workflow files exist
print_info "Checking workflow files..."
if [ -f ".github/workflows/auto-release.yml" ]; then
    print_success "Advanced auto-release workflow exists"
else
    print_warning "Advanced auto-release workflow not found"
fi

if [ -f ".github/workflows/simple-auto-release.yml" ]; then
    print_success "Simple auto-release workflow exists"
else
    print_warning "Simple auto-release workflow not found"
fi

# Test 2: Check helper scripts
print_info "Checking helper scripts..."
if [ -f "scripts/commit-helper.sh" ] && [ -x "scripts/commit-helper.sh" ]; then
    print_success "Commit helper script exists and is executable"
else
    print_warning "Commit helper script not found or not executable"
fi

if [ -f "scripts/setup-auto-release.sh" ] && [ -x "scripts/setup-auto-release.sh" ]; then
    print_success "Setup script exists and is executable"
else
    print_warning "Setup script not found or not executable"
fi

# Test 3: Check current git status
print_info "Checking git status..."
current_branch=$(git branch --show-current)
if [ "$current_branch" = "main" ]; then
    print_success "On main branch"
else
    print_warning "Not on main branch (current: $current_branch)"
fi

# Test 4: Check for existing tags
print_info "Checking version tags..."
latest_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -n "$latest_tag" ]; then
    print_success "Latest tag: $latest_tag"
else
    print_warning "No version tags found"
fi

# Test 5: Validate workflow syntax using act (if available)
if command -v act &> /dev/null; then
    print_info "Validating workflow syntax with act..."
    
    if [ -f ".github/workflows/auto-release.yml" ]; then
        if act --list --workflows .github/workflows/auto-release.yml > /dev/null 2>&1; then
            print_success "Advanced workflow syntax is valid"
        else
            print_error "Advanced workflow syntax is invalid"
        fi
    fi
    
    if [ -f ".github/workflows/simple-auto-release.yml" ]; then
        if act --list --workflows .github/workflows/simple-auto-release.yml > /dev/null 2>&1; then
            print_success "Simple workflow syntax is valid"
        else
            print_error "Simple workflow syntax is invalid"
        fi
    fi
else
    print_info "Act not available, skipping workflow validation"
fi

# Test 6: Test version calculation logic
print_info "Testing version calculation..."
if [ -n "$latest_tag" ]; then
    # Test version parsing
    version=${latest_tag#v}
    IFS='.' read -r major minor patch <<< "$version"
    
    if [[ $major =~ ^[0-9]+$ ]] && [[ $minor =~ ^[0-9]+$ ]] && [[ $patch =~ ^[0-9]+$ ]]; then
        print_success "Version format is valid: $major.$minor.$patch"
        
        # Calculate next versions
        next_patch="$major.$minor.$((patch + 1))"
        next_minor="$major.$((minor + 1)).0"
        next_major="$((major + 1)).0.0"
        
        print_info "Next versions:"
        echo "  Patch: v$next_patch"
        echo "  Minor: v$next_minor"
        echo "  Major: v$next_major"
    else
        print_error "Invalid version format: $version"
    fi
else
    print_info "No existing tags to test version calculation"
fi

# Test 7: Test build process
print_info "Testing build process..."
if make build > /dev/null 2>&1; then
    print_success "Build process works"
else
    print_error "Build process failed"
fi

# Test 8: Test commit message parsing
print_info "Testing commit message parsing..."
test_messages=(
    "feat: add new feature"
    "fix: resolve bug"
    "feat!: breaking change"
    "docs: update documentation"
    "chore: update dependencies"
)

for msg in "${test_messages[@]}"; do
    if echo "$msg" | grep -q "feat!"; then
        bump_type="major"
    elif echo "$msg" | grep -q "feat:"; then
        bump_type="minor"
    else
        bump_type="patch"
    fi
    
    print_info "\"$msg\" → $bump_type bump"
done

# Test 9: Check GitHub CLI availability
print_info "Checking GitHub CLI..."
if command -v gh &> /dev/null; then
    print_success "GitHub CLI is available"
    
    # Test if we can access the repository
    if gh repo view > /dev/null 2>&1; then
        print_success "Can access GitHub repository"
    else
        print_warning "Cannot access GitHub repository (authentication needed?)"
    fi
else
    print_warning "GitHub CLI not found - releases will need manual upload"
fi

# Test 10: Simulate version bump
print_info "Simulating version bump..."
if [ -n "$latest_tag" ]; then
    version=${latest_tag#v}
    IFS='.' read -r major minor patch <<< "$version"
    
    # Simulate different bump types
    simulated_patch="v$major.$minor.$((patch + 1))"
    simulated_minor="v$major.$((minor + 1)).0"
    simulated_major="v$((major + 1)).0.0"
    
    print_info "Simulated version bumps from $latest_tag:"
    echo "  Patch: $simulated_patch"
    echo "  Minor: $simulated_minor"
    echo "  Major: $simulated_major"
else
    print_info "No existing tags for version bump simulation"
fi

# Summary
echo ""
print_info "Test Summary:"
echo "  Current branch: $current_branch"
echo "  Latest tag: ${latest_tag:-"None"}"
echo "  Build status: $(make build > /dev/null 2>&1 && echo "✅ Working" || echo "❌ Failed")"
echo "  GitHub CLI: $(command -v gh &> /dev/null && echo "✅ Available" || echo "❌ Not found")"
echo "  Act tool: $(command -v act &> /dev/null && echo "✅ Available" || echo "❌ Not found")"

echo ""
print_info "Next Steps:"
echo "1. Run './scripts/setup-auto-release.sh' to configure auto-releases"
echo "2. Choose between simple or advanced workflow"
echo "3. Make a test commit to verify the system works"
echo "4. Check GitHub Actions tab for workflow execution"

echo ""
print_info "For more information, see docs/AUTO-RELEASE.md"
print_success "Auto-release system test complete! 🎉" 