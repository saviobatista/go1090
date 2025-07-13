#!/bin/bash

# Setup Auto-Release System
# Helps configure automatic versioning and releases

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 Auto-Release Setup${NC}"
echo -e "${BLUE}===================${NC}"
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
    print_error "Not in a git repository. Please run this script from your project root."
    exit 1
fi

# Check if main branch exists
if ! git show-ref --verify --quiet refs/heads/main; then
    print_warning "Main branch not found. Creating it..."
    git checkout -b main 2>/dev/null || git checkout main
fi

print_info "Setting up automatic releases for your project..."
echo ""

# Choose workflow type
echo -e "${BLUE}Choose your auto-release workflow:${NC}"
echo "1) Simple Auto-Release (patch version bump on every merge)"
echo "2) Advanced Auto-Release (semantic versioning based on commit messages)"
echo "3) Manual releases only (disable auto-release)"
echo ""

read -p "Enter choice (1-3): " workflow_choice

case $workflow_choice in
    1)
        workflow_type="simple"
        workflow_file="simple-auto-release.yml"
        ;;
    2)
        workflow_type="advanced"
        workflow_file="auto-release.yml"
        ;;
    3)
        workflow_type="manual"
        workflow_file="release.yml"
        ;;
    *)
        print_error "Invalid choice"
        exit 1
        ;;
esac

# Create .github/workflows directory if it doesn't exist
mkdir -p .github/workflows

# Copy appropriate workflow file
if [ "$workflow_type" = "simple" ]; then
    if [ -f ".github/workflows/simple-auto-release.yml" ]; then
        cp ".github/workflows/simple-auto-release.yml" ".github/workflows/release.yml"
        print_success "Simple auto-release workflow configured"
    else
        print_error "Simple auto-release workflow file not found"
        exit 1
    fi
elif [ "$workflow_type" = "advanced" ]; then
    if [ -f ".github/workflows/auto-release.yml" ]; then
        cp ".github/workflows/auto-release.yml" ".github/workflows/release.yml"
        print_success "Advanced auto-release workflow configured"
    else
        print_error "Advanced auto-release workflow file not found"
        exit 1
    fi
else
    print_success "Manual release workflow will be used"
fi

# Set up commit helper for advanced workflow
if [ "$workflow_type" = "advanced" ]; then
    echo ""
    read -p "Would you like to set up the commit helper script? (y/n): " setup_commit_helper
    
    if [[ $setup_commit_helper =~ ^[Yy]$ ]]; then
        # Make commit helper executable
        chmod +x scripts/commit-helper.sh
        
        # Create git alias for easier usage
        git config alias.commit-helper '!./scripts/commit-helper.sh'
        
        print_success "Commit helper configured. Use 'git commit-helper' to create properly formatted commits."
    fi
fi

# Set up initial version tag if none exists
echo ""
current_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -z "$current_tag" ]; then
    read -p "No existing tags found. Create initial tag v0.1.0? (y/n): " create_initial_tag
    
    if [[ $create_initial_tag =~ ^[Yy]$ ]]; then
        git tag -a "v0.1.0" -m "Initial release"
        print_success "Created initial tag v0.1.0"
        
        read -p "Push tag to remote? (y/n): " push_tag
        if [[ $push_tag =~ ^[Yy]$ ]]; then
            git push origin v0.1.0
            print_success "Tag pushed to remote"
        fi
    fi
else
    print_info "Found existing tag: $current_tag"
fi

# Configure branch protection (optional)
echo ""
read -p "Would you like to configure branch protection for main? (y/n): " setup_protection

if [[ $setup_protection =~ ^[Yy]$ ]]; then
    echo ""
    print_info "Branch protection should be configured in GitHub settings:"
    echo "1. Go to Settings > Branches"
    echo "2. Add rule for 'main' branch"
    echo "3. Enable 'Require pull request reviews'"
    echo "4. Enable 'Require status checks'"
    echo "5. Enable 'Require branches to be up to date'"
    echo "6. Enable 'Include administrators'"
fi

# Test the setup
echo ""
read -p "Would you like to test the setup? (y/n): " test_setup

if [[ $test_setup =~ ^[Yy]$ ]]; then
    print_info "Testing setup..."
    
    # Test if workflow file is valid
    if [ -f ".github/workflows/release.yml" ]; then
        print_success "Workflow file exists"
    else
        print_error "Workflow file not found"
    fi
    
    # Test if required tools are available
    if command -v gh &> /dev/null; then
        print_success "GitHub CLI (gh) is available"
    else
        print_warning "GitHub CLI (gh) not found. Install it for better GitHub integration."
    fi
    
    # Test if we can build the project
    if make build &> /dev/null; then
        print_success "Project builds successfully"
    else
        print_warning "Project build failed. Check your build configuration."
    fi
fi

# Summary
echo ""
print_info "Setup Summary:"
echo "  Workflow Type: $workflow_type"
echo "  Active Workflow: .github/workflows/release.yml"
if [ "$workflow_type" = "simple" ]; then
    echo "  Behavior: Patch version bump on every merge to main"
elif [ "$workflow_type" = "advanced" ]; then
    echo "  Behavior: Semantic versioning based on commit messages"
    echo "  Commit Helper: Use 'git commit-helper' for proper commit formatting"
fi

echo ""
print_info "Next Steps:"
if [ "$workflow_type" = "simple" ]; then
    echo "1. Make changes to your code"
    echo "2. Commit and push to main branch"
    echo "3. Automatic release will be created with patch version bump"
elif [ "$workflow_type" = "advanced" ]; then
    echo "1. Use 'git commit-helper' or follow conventional commit format"
    echo "2. Push to main branch"
    echo "3. Automatic release will be created based on commit types:"
    echo "   - feat: -> minor version bump"
    echo "   - fix: -> patch version bump"
    echo "   - feat!: or BREAKING CHANGE -> major version bump"
fi

echo ""
print_info "Documentation:"
echo "- View workflows: .github/workflows/"
echo "- Commit helper: scripts/commit-helper.sh"
echo "- Test locally: ./test-workflow.sh"

echo ""
print_success "Auto-release setup complete! 🎉" 