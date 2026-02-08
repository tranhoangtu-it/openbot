# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.2.x   | :white_check_mark: |
| < 0.2   | :x:                |

We release security updates for the latest minor version. Please upgrade to the latest release.

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

- **Do not** open a public issue.
- Email or contact the maintainers privately (see repository contacts).
- Include a description, steps to reproduce, and impact.
- We will acknowledge receipt and aim to respond within a reasonable time. If the report is accepted, we will work on a fix and coordinate disclosure.

## Security Headers (Web UI)

The Web UI sets the following HTTP response headers for defense in depth:

- **X-Frame-Options: DENY** — Prevents the page from being embedded in iframes (clickjacking mitigation).
- **X-Content-Type-Options: nosniff** — Prevents MIME-type sniffing.

- **Content-Security-Policy-Report-Only** — CSP in report-only mode is set (see `internal/channel/web.go`). It does not block content; use browser devtools or report endpoint to collect violations before switching to enforcing CSP.

Configuration is in `internal/channel/web.go` (middleware applied to all Web UI responses).

## Dependency Audit

To check for known vulnerabilities in Go dependencies:

1. **govulncheck** (recommended):
   ```bash
   go install golang.org/x/vuln/cmd/govulncheck@latest
   govulncheck ./...
   ```

2. **go list** (list outdated modules):
   ```bash
   go list -m -u all
   ```

Update dependencies with `go get -u ./...` or targeted updates, then run `make test` and fix any breakage. We recommend running an audit before each release and when adding or updating modules.

