# Release Guide for Go1090

This document outlines the process for creating releases of Go1090.

## 📋 Release Checklist

### Pre-Release Steps

1. **Update Documentation**
   - [ ] Update `CHANGELOG.md` with new features, fixes, and changes
   - [ ] Update version references in `README.md` if needed
   - [ ] Review and update installation instructions
   - [ ] Test all examples in documentation

2. **Code Quality Checks**
   - [ ] Run `make check` to ensure code quality
   - [ ] Run `make test` to verify all tests pass
   - [ ] Test with real RTL-SDR device (if available)
   - [ ] Verify cross-platform builds: `make build-all`

3. **Version Preparation**
   - [ ] Decide on version number following [Semantic Versioning](https://semver.org/)
   - [ ] Update `CHANGELOG.md` with release date
   - [ ] Commit all changes: `git commit -am "Prepare for vX.Y.Z release"`

## 🚀 Release Process

### 1. Create and Push Tag

```bash
# Create annotated tag
git tag -a v1.0.0 -m "Release v1.0.0"

# Push tag to trigger GitHub Actions
git push origin v1.0.0
```

### 2. GitHub Actions Automatic Build

The GitHub Actions workflow will automatically:
- Build binaries for all platforms (Linux, macOS, Windows)
- Create platform-specific packages
- Generate release notes
- Upload release assets

### 3. Monitor Release Build

1. Go to **GitHub Actions** tab
2. Watch the "Build and Release" workflow
3. Verify all builds complete successfully
4. Check that release is created with all assets

### 4. Post-Release Steps

1. **Verify Release**
   - [ ] Download and test binaries from each platform
   - [ ] Verify installation instructions work
   - [ ] Test RTL-SDR functionality (if device available)

2. **Update Documentation**
   - [ ] Update any version-specific links
   - [ ] Consider updating examples with new features

3. **Announce Release**
   - [ ] Post to relevant aviation/SDR communities
   - [ ] Update project description if needed

## 🔧 Manual Release Process (Fallback)

If GitHub Actions fails, you can create releases manually:

### 1. Build All Platforms

```bash
# Clean and build all platforms
make clean
make release-prep
```

### 2. Create Release on GitHub

1. Go to **GitHub Releases**
2. Click **"Create a new release"**
3. Choose your tag
4. Fill in release notes (use template below)
5. Upload files from `dist/` directory

### 3. Release Notes Template

```markdown
# Go1090 ADS-B Beast Mode Decoder vX.Y.Z

## Downloads

Choose the appropriate binary for your system:

### Linux
- **Linux x64**: `go1090-vX.Y.Z-linux-amd64.tar.gz`
- **Linux ARM64**: `go1090-vX.Y.Z-linux-arm64.tar.gz`

### macOS  
- **macOS Intel**: `go1090-vX.Y.Z-darwin-amd64.tar.gz`
- **macOS Apple Silicon**: `go1090-vX.Y.Z-darwin-arm64.tar.gz`

### Windows
- **Windows x64**: `go1090-vX.Y.Z-windows-amd64.zip`
- ⚠️ **Note**: Windows build has limited RTL-SDR support

## What's New

[Copy from CHANGELOG.md]

## Installation

1. Download the appropriate archive for your system
2. Extract the archive: `tar -xzf go1090-vX.Y.Z-your-platform.tar.gz`
3. Follow the INSTALL.md instructions included in the package
4. Install RTL-SDR dependencies as described
5. Run: `./go1090-your-platform --help`

## Requirements

- RTL2832U USB SDR device
- librtlsdr library installed
- 1090MHz antenna (for optimal reception)
```

## 🐛 Troubleshooting Releases

### GitHub Actions Build Fails

**Common Issues:**

1. **CGO/librtlsdr not found**
   - Check if dependencies are properly installed in CI
   - Verify package names for different platforms

2. **Cross-compilation fails**
   - Ensure cross-compilation tools are available
   - Check CC environment variables for ARM builds

3. **Release upload fails**
   - Verify GITHUB_TOKEN permissions
   - Check if release already exists

**Solutions:**

```bash
# Test builds locally before tagging
make build-all

# Check specific platform build
make build-linux
make build-darwin
make build-windows
```

### Manual Build Issues

```bash
# Check dependencies
make check-deps

# Clean and rebuild
make clean-all
make deps
make build

# Test version info
make run-version
```

## 📚 Version Guidelines

### Major Version (X.0.0)
- Breaking API changes
- Major architectural changes
- Removal of deprecated features

### Minor Version (0.X.0)
- New features
- New platform support
- Significant improvements

### Patch Version (0.0.X)
- Bug fixes
- Documentation updates
- Security fixes

## 🔄 Post-Release Workflow

After each release:

1. **Create next development milestone**
   - Update `CHANGELOG.md` with `[Unreleased]` section
   - Plan next features

2. **Monitor feedback**
   - Watch GitHub Issues for bug reports
   - Monitor download statistics
   - Collect user feedback

3. **Plan hotfixes if needed**
   - Critical bugs: patch release (0.0.X)
   - Security issues: immediate patch release

## 📞 Release Support

- **Documentation**: Update README.md and GitHub Wiki
- **Issues**: Respond to GitHub Issues promptly
- **Discussions**: Engage in GitHub Discussions
- **Community**: Monitor aviation/SDR forums for feedback 