# Security Policy

## Supported Versions

Only the latest release is actively supported.

## Reporting a Vulnerability

Please do not open a public issue for vulnerabilities that expose account data,
Cookie values, App-specific passwords, or local file contents.

Use GitHub private vulnerability reporting if it is available on the repository.
If it is not available, contact the maintainer through GitHub and share only the
minimum information needed to reproduce the issue.

## Sensitive Data Rules

Never include real values for:

- `data/accounts.json`
- `data/create_jobs.json`
- iCloud Cookie headers
- `X-APPLE-*` Cookie values
- App-specific passwords
- proxy credentials
- local logs containing account identifiers

Use placeholders in all reports and pull requests.

## Expected Response

Maintainers will acknowledge valid reports, investigate reproduction steps, and
publish fixes or mitigations in a tagged release when appropriate.
