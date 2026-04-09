# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in go-jsonfast, please report it responsibly:

1. **Do not** open a public GitHub issue.
2. Use GitHub's private vulnerability reporting feature, or email the maintainers directly.
3. Include: description, steps to reproduce, and impact assessment.
4. We will acknowledge receipt within 48 hours and provide a fix timeline.

## Scope

go-jsonfast is a JSON builder library. Security concerns include:

- Denial of service via crafted input (e.g., excessive memory or CPU usage).
- Panics or crashes on adversarial input to `EscapeString`.
- Invalid JSON output that could lead to injection in downstream systems.

The library is fuzz-tested continuously with Go's built-in fuzzer.

## Known Limits

The builder enforces the following bounds to prevent resource exhaustion:

| Parameter | Limit | Constant |
|-----------|-------|----------|
| Maximum pooled buffer size | 256 KiB | `1<<18` |
| Maximum flatten depth | 64 levels | `maxFlattenDepth` |

Buffers exceeding the pool limit are discarded on `Release` to avoid holding large memory.
Inputs nested beyond the flatten depth limit cause `FlattenObject` to return `false`.
