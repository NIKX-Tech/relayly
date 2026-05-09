# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.0.0 (2026-05-09)


### Features

* implement Noise XX handshake, device pairing UI, and key locking security ([60c0b6a](https://github.com/NIKX-Tech/relayly/commit/60c0b6acce6fa8dd753c5dca909f0753f4df831e))

## [Unreleased]

### Added
- Unified monorepo structure.
- Professional GitHub workflows for CI/CD.
- Release automation via GoReleaser.
- Dependabot configuration for automated dependency updates.
- Security policies and contribution guidelines.
- Support for Noise Protocol XX handshakes.
- Embedded HTMX Admin UI.
- Go and TypeScript SDKs.

### Changed
- Shifted default branch from `dev` to `main`.
- Improved CI performance with caching.

### Fixed
- Fixed various `errcheck` linting errors in tests and CLI.
- Resolved Go toolchain version mismatches in CI.
