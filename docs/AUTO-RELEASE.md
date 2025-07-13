# Auto-Release System Documentation

This document explains how to set up and use the automated release system for Go1090.

## Overview

The auto-release system automatically creates new versions and releases when code is merged to the main branch. It supports two modes:

1. **Simple Auto-Release**: Increments patch version on every merge
2. **Advanced Auto-Release**: Uses semantic versioning based on commit messages

## Quick Start

1. **Setup**: Run the setup script to configure auto-releases
   ```bash
   chmod +x scripts/setup-auto-release.sh
   ./scripts/setup-auto-release.sh
   ```

2. **Choose your workflow**:
   - **Simple**: Patch version bump on every merge (1.0.0 → 1.0.1)
   - **Advanced**: Semantic versioning based on commit messages

3. **Start using**: Make changes and push to main branch

## Simple Auto-Release

### How it works
- Every merge to main triggers a patch version bump
- Builds releases for all supported platforms
- Creates ZIP files with binaries and documentation
- Uploads to GitHub Releases

### Usage
```bash
# Make changes
git add .
git commit -m "Fix bug in parser"
git push origin main

# → Automatically creates v1.0.1 (if previous was v1.0.0)
```

### Configuration
File: `.github/workflows/simple-auto-release.yml`

```yaml
on:
  push:
    branches: [ main ]
    paths-ignore:
      - '**.md'
      - 'docs/**'
      - '.github/**'
```

## Advanced Auto-Release

### How it works
- Analyzes commit messages to determine version bump type
- Follows [Conventional Commits](https://www.conventionalcommits.org/) specification
- Generates changelogs from commit history
- Supports manual version bumping

### Commit Message Format
```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Version Bump Rules
| Commit Type | Version Bump | Example |
|-------------|-------------|---------|
| `feat:` | Minor | 1.0.0 → 1.1.0 |
| `fix:` | Patch | 1.0.0 → 1.0.1 |
| `feat!:` or `BREAKING CHANGE:` | Major | 1.0.0 → 2.0.0 |
| `docs:`, `style:`, `refactor:`, `test:`, `chore:` | Patch | 1.0.0 → 1.0.1 |

### Examples
```bash
# Feature (minor version bump)
git commit -m "feat: add new RTL-SDR device support"

# Bug fix (patch version bump)
git commit -m "fix: resolve Beast mode parsing issue"

# Breaking change (major version bump)
git commit -m "feat!: change CLI interface"
# or
git commit -m "feat: new API

BREAKING CHANGE: API has changed significantly"

# Documentation (patch version bump)
git commit -m "docs: update installation guide"
```

### Using the Commit Helper
The commit helper script guides you through creating properly formatted commits:

```bash
# Use the helper
git commit-helper

# Or use the git alias
git commit-helper
```

### Manual Version Bumping
You can manually trigger releases with specific version bumps:

1. Go to Actions tab in GitHub
2. Select "Auto Release" workflow
3. Click "Run workflow"
4. Choose version bump type (patch/minor/major)

## Build Process

### Supported Platforms
- Linux (x64, ARM64)
- macOS (Intel, Apple Silicon)
- Windows (x64, limited RTL-SDR support)

### Build Output
Each release includes:
- Binary for each platform
- README.md
- LICENSE
- CHANGELOG.md
- Platform-specific INSTALL.md

### Package Structure
```
go1090-1.2.3-linux-amd64.zip
├── go1090-linux-amd64
├── README.md
├── LICENSE
├── CHANGELOG.md
└── INSTALL.md
```

## Testing

### Local Testing
Test the workflows locally using `act`:

```bash
# Test the workflow
act --job test --container-architecture linux/amd64

# Test specific matrix
act --matrix os:linux --matrix arch:amd64 --job build

# Dry run
act --dryrun
```

### Test Script
Use the comprehensive test script:

```bash
./test-workflow.sh
```

## Configuration

### Workflow Files
- `auto-release.yml`: Advanced semantic versioning
- `simple-auto-release.yml`: Simple patch bumping
- `release.yml`: Active workflow (symlink to chosen type)

### Environment Variables
```yaml
env:
  GO_VERSION: '1.21'
```

### Customization
Edit the workflow files to customize:
- Supported platforms
- Build flags
- Package contents
- Release notes format

## Security

### Required Permissions
The workflows require these GitHub permissions:
- `contents: write` (create releases)
- `actions: read` (workflow access)

### Branch Protection
Recommended settings for main branch:
- Require pull request reviews
- Require status checks
- Require branches to be up to date
- Include administrators

## Troubleshooting

### Common Issues

1. **No releases created**
   - Check if workflow is triggered
   - Verify branch protection settings
   - Check GitHub Actions logs

2. **Build failures**
   - Ensure dependencies are available
   - Check CGO settings for cross-compilation
   - Verify static linking configuration

3. **Permission errors**
   - Check GitHub token permissions
   - Verify repository settings

### Debug Commands
```bash
# Check current tags
git tag -l

# Check workflow status
gh workflow list
gh workflow view

# Check latest release
gh release list
```

## Best Practices

### Commit Messages
- Use conventional commit format
- Be descriptive but concise
- Include breaking change notes
- Reference issues when applicable

### Release Management
- Use semantic versioning
- Document breaking changes
- Test before merging to main
- Review auto-generated changelogs

### Branch Management
- Use feature branches
- Require pull request reviews
- Enable status checks
- Keep main branch stable

## Advanced Features

### Custom Release Notes
Modify the changelog generation in the workflow:

```yaml
- name: Generate changelog
  run: |
    # Custom changelog generation
    echo "## Custom Changes" > RELEASE_NOTES.md
    git log --oneline --since="1 day ago" >> RELEASE_NOTES.md
```

### Conditional Releases
Skip releases for certain commits:

```yaml
- name: Check if release needed
  run: |
    if [[ ${{ github.event.head_commit.message }} == *"[skip-release]"* ]]; then
      echo "Skipping release"
      exit 0
    fi
```

### Notification Integration
Add notifications to your workflow:

```yaml
- name: Notify Slack
  uses: 8398a7/action-slack@v3
  with:
    status: ${{ job.status }}
    webhook_url: ${{ secrets.SLACK_WEBHOOK }}
```

## Migration

### From Manual to Auto-Release
1. Run setup script
2. Choose workflow type
3. Create initial tag if needed
4. Configure branch protection
5. Test with a small change

### Between Workflow Types
1. Backup current workflow
2. Run setup script
3. Choose new workflow type
4. Update documentation
5. Notify team of changes

## Reference

### Links
- [Conventional Commits](https://www.conventionalcommits.org/)
- [Semantic Versioning](https://semver.org/)
- [GitHub Actions](https://docs.github.com/en/actions)
- [Act - Run GitHub Actions locally](https://github.com/nektos/act)

### Scripts
- `scripts/setup-auto-release.sh`: Initial setup
- `scripts/commit-helper.sh`: Commit message helper
- `test-workflow.sh`: Local testing

### Files
- `.github/workflows/auto-release.yml`: Advanced workflow
- `.github/workflows/simple-auto-release.yml`: Simple workflow
- `docs/AUTO-RELEASE.md`: This documentation 