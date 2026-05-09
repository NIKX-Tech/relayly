# Contributing to Relayly

First off, thank you for considering contributing to Relayly! It's people like you that make Relayly such a great tool for local-first, privacy-focused communication.

## Where do I go from here?

If you've noticed a bug or have a feature request, make sure to check our [Issues](https://github.com/nikx-one/relayly/issues) first to see if someone else has already created a ticket. If not, go ahead and [make one](https://github.com/nikx-one/relayly/issues/new)!

## Setting up for local development

To develop Relayly locally, you'll need:
- Go 1.22+
- Git
- Docker (optional, but recommended for running isolated instances)

### Building the project

1. Fork the repo and clone it to your machine.
2. Run `make deps` to install Go dependencies.
3. Run `make build` to build the `relayly` binary.

### Running tests

Before submitting a pull request, please ensure all tests pass. We require tests for any new features or bug fixes.

```bash
make test
```

This will run all unit tests with the race detector enabled.

### Code Style & Linting

We use standard `gofmt` and `golangci-lint`. Please ensure your code is formatted and passes the linter before submitting.

```bash
make lint
```

## Making a Pull Request

1. Fork the repository and create your branch from `main`.
2. Write clear, concise commit messages.
3. Add tests for any new functionality or bug fixes.
4. Update the `README.md` if you are changing user-facing features.
5. Ensure the CI suite passes.
6. Open a Pull Request! We'll review it as soon as we can.

## Architecture Guidelines

- **Monorepo Structure:** The project is a monorepo. The core server code lives in `cmd/` and `internal/`. The official client SDKs (Go, TypeScript) are located in `sdk/`. Examples using the SDKs are in `examples/`.
- **Keep it simple:** Relayly is designed to be extremely simple to run. Avoid adding complex dependencies (especially CGo).
- **Security first:** All data payloads must be end-to-end encrypted using the Noise protocol. The relay server must never have access to plaintext data.
- **Admin UI:** Changes to the Admin UI should use HTMX and Tailwind CSS, and must remain embedded in the Go binary without requiring external assets to be served or a separate build step (Tailwind is loaded via CDN for simplicity in the admin interface).

Thank you for contributing!
