# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0](https://github.com/NIKX-Tech/relayly/compare/relayly-v0.2.0...relayly-v0.3.0) (2026-05-21)


### Features

* add live chat demo and fix relay transport encryption ([a1e0f20](https://github.com/NIKX-Tech/relayly/commit/a1e0f20c4a34284570c78bac8755742fc8ec7a5b))
* add live chat demo and fix relay transport encryption ([311ae69](https://github.com/NIKX-Tech/relayly/commit/311ae692dd0d09b838d02ef121af01ca42504116))
* add pairing expiry, rate limiting, REST API, and new examples ([8874531](https://github.com/NIKX-Tech/relayly/commit/88745315f82ed1aeda2b6e775fc34b28793a9802))
* fix admin favicon, add API tests, and cut v0.3.0 changelog ([a4feccb](https://github.com/NIKX-Tech/relayly/commit/a4feccbb6cf437869244d5ac1ffc566e73df708c))
* fix admin favicon, add API tests, and cut v0.3.0 changelog ([a46381b](https://github.com/NIKX-Tech/relayly/commit/a46381b43146e3568092cf623eaa8b22c99563d5))
* rebrand admin UI with logo, polished layout, and page-routing fix ([e964cb3](https://github.com/NIKX-Tech/relayly/commit/e964cb36129513bbabd7bdc66daaf7d02463dc5c))


### Bug Fixes

* update readme badges ([3b6b13c](https://github.com/NIKX-Tech/relayly/commit/3b6b13c2b5eb95f09ef7ddb736128eeaa00ee2e1))

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
