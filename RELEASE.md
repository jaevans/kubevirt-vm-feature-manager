# Release Process

This project uses [GoReleaser](https://goreleaser.com/) for automated releases.

## For Maintainers

### Creating a Release

1. **Ensure all tests pass:**
   ```bash
   make test
   ```

2. **Create and push a version tag:**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

3. **GitHub Actions will automatically:**
   - Build binaries for multiple platforms (amd64, arm64, armv7)
   - Create multi-arch Docker images
   - Push images to GitHub Container Registry
   - Generate SBOMs and checksums
   - Sign artifacts with Cosign
   - Create a GitHub release with changelog

### Testing Releases Locally

Test the release process without publishing:

```bash
# Install GoReleaser
make install-tools

# Build a snapshot (local test)
make release-snapshot

# Test release without publishing
make release-test
```

The artifacts will be in the `dist/` directory.

### Manual Release

If you need to release manually:

```bash
export GITHUB_TOKEN="your-token-here"
export GITHUB_REPOSITORY_OWNER="jaevans"
make release
```

## Version Numbering

Follow [Semantic Versioning](https://semver.org/):
- `vX.Y.Z` - Production release
- `vX.Y.Z-rc.N` - Release candidate
- `vX.Y.Z-beta.N` - Beta release
- `vX.Y.Z-alpha.N` - Alpha release

## Commit Message Convention

For better changelogs, use conventional commit messages:

- `feat:` - New features
- `fix:` - Bug fixes
- `enhance:` - Enhancements to existing features
- `docs:` - Documentation changes
- `test:` - Test changes
- `ci:` - CI/CD changes
- `chore:` - Maintenance tasks

Example:
```
feat: add support for multiple GPU devices
fix: correct annotation stripping in error handler
enhance: improve patch validation logic
```
