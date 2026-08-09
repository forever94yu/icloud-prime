# Changelog

All notable changes to this project will be documented in this file.

## v0.1.1 - 2026-08-09 16:28 +08:00

- Added batch Hide My Email alias creation with per-account hourly quota reporting.
- Added automatic alias creation jobs with duration and daily-window modes.
- Added local job persistence in `data/create_jobs.json`.
- Added API endpoints for listing, creating, pausing, resuming, and deleting automatic creation jobs.
- Updated the web console with batch creation and scheduled job controls.
- Added tests for create-job APIs, scheduler behavior, limiter behavior, and persistent job storage.

## v0.1.0

- Initial public release.
- Added local web console for iCloud Hide My Email alias management.
- Added Windows 10 portable package.
- Added placeholder-only example account configuration.
