# Contributing to OpenBot

Thank you for your interest in contributing to OpenBot.

## How to contribute

- **Bug reports**: Open an issue describing the problem, steps to reproduce, and your environment (OS, Go version).
- **Feature requests**: Open an issue with a clear description and use case.
- **Code**: Fork the repo, create a branch, make your changes, run tests (`make test`, with `-race`), and open a pull request. When changing Web UI or API, run `make test` before merge; run E2E (`make e2e`) if you changed chat/API flows

## Development setup

- Go 1.25+
- Run `make build` and `make test` before submitting.

## Documentation

- Project docs (Vietnamese) are in `docs/projects/` (presales, architecture, development, governance).
- When adding a channel, provider, or tool, document it in docs/projects and update
