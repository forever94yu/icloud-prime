# Changelog

All notable changes to this project will be documented in this file.

## v0.1.3 - 2026-08-11 12:10 +08:00

- Improved the inbox reading workflow by opening clicked messages in a centered detail modal.
- Removed the old bottom-of-page mail detail reading pattern from the primary inbox flow.
- Added modal metadata for subject, sender, recipient, time, detected verification code, and body preview.
- Added click-outside and close-button handling for the mail detail modal.
- Updated responsive styling for the inbox list and modal layout.
- Updated README usage notes and rebuilt embedded web assets for the new frontend behavior.

## v0.1.2 - 2026-08-10 00:46 +08:00

- Refreshed the web console visual design across account, creation, alias list, inbox, verification code, and settings views.
- Added public demo screenshots to the README.
- Sanitized README screenshots so real account data, aliases, verification codes, and message previews are not exposed.
- Updated the embedded web assets for the redesigned frontend.
- Updated the release workflow to publish only the Windows 10 portable package.

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
