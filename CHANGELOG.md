# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2026-05-20

### Added
- REST API on the relay port under `/api/v1/`:
  - `POST /api/v1/pair` — register a new device and receive `device_id` + `pair_token`
  - `GET /api/v1/devices` — list all registered devices
  - `GET /api/v1/health` — server status, version, uptime, connected device count
- CORS middleware on all API endpoints (supports browser and mobile clients)
- Per-IP token-bucket rate limiter on WebSocket upgrades (10 req/min, HTTP 429 on excess)
- Pairing code TTL — `expires_at` column on `devices` table (schema migration v2), pair codes expire after 5 minutes
- API handler test suite (`internal/api/handler_test.go`) covering pair, list devices, health, CORS preflight
- Go clipboard-sync example (`examples/go/clipboard-sync/`)
- TypeScript echo client example (`examples/ts/echo/`)
- Rewritten `docs/PROTOCOL.md` matching current Noise XX + WebSocket behaviour

### Changed
- WebSocket upgrade handler wired through rate limiter before reaching the relay hub

## [0.2.0](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.1.0...relayly-v0.2.0) (2026-05-09)


### Features

* add String method to version package ([e4fb56b](https://github.com/NIKX-Tech/relayly/commit/e4fb56bc68b94bda6bc81b7a46efad6bff221ab3))
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
