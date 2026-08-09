# Contributing

Thanks for helping improve iCloud Prime.

This project accepts issues and pull requests from the community. The goal is
to keep contributions small, reviewable, and safe for users who store sensitive
iCloud account data locally.

## Ways to Contribute

- Report bugs with clear reproduction steps.
- Suggest features and explain the user workflow.
- Improve documentation and setup instructions.
- Add tests around account parsing, API handlers, and mail behavior.
- Improve Windows portable packaging.
- Improve error messages without logging secrets.

## Before You Start

1. Search existing issues and pull requests.
2. Open an issue for larger changes before writing code.
3. Keep the scope small.
4. Never include real Cookie values, App-specific passwords, account data, or local job data.

## Local Development

```bash
go mod download
```

If you change the web UI:

```bash
cd web
npm ci
npm run build
cd ..
```

Run Go checks when Go is installed:

```bash
go test ./...
```

Build a local Windows binary:

```bash
go build -ldflags="-s -w" -o icloud-prime.exe .
```

## Pull Request Checklist

- [ ] The change has a clear user or maintainer benefit.
- [ ] Sensitive local data is not included.
- [ ] Documentation is updated when behavior changes.
- [ ] Tests or manual verification steps are included.
- [ ] The PR description explains what changed and why.

## Security Rules

Do not paste real secrets into issues, pull requests, screenshots, logs, or test
fixtures. Use placeholders such as `PASTE_YOUR_COOKIE_VALUE_HERE`.

Do not add logging that prints Cookie values, App-specific passwords, proxy
credentials, full account files, or `data/create_jobs.json`.

## Review Process

Maintainers will triage issues, review pull requests, request changes when
needed, and release tagged versions when changes are ready.
