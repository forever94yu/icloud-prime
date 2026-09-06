# Changelog

All notable changes to this project will be documented in this file.

## v0.1.9 - 2026-09-06

- Preserve account credentials when redacting API responses, isolate Cookie snapshots across requests, and roll back account changes when persistence fails.
- Listen on localhost by default. Remote listeners now require an API token; local API calls also validate the peer, Host, and browser Origin.
- Restore account creation, Cookie updates, App Password setup, and password login in the web console, with credential status and API token settings.
- Prevent stale account and inbox requests from mixing accounts, and invalidate cached account data after deletion.
- Correct SRP challenge forwarding and reject invalid authentication responses without panicking.
- Fix daily schedule boundaries, cross-hour quota accounting, pause races, and task persistence rollback.
- Return already-created aliases when a batch partially fails instead of hiding successful creations.
- Fix Web mail recipient searches and China-region gateway Cookies, preserve IMAP errors and partial results, and exclude MIME attachments from message bodies.
- Return actual unread state, match Hide My Email filters against known aliases, and load message bodies in batches beyond the first 50 messages.
- Reject upstream alias-list business errors instead of caching them as empty lists.
- Add regression tests for all reviewed failure scenarios and require tests before publishing release packages.

## v0.1.8 - 2026-08-27

- Import edited `data/accounts.example.json` on first startup when `data/accounts.json` does not exist.
- Ignore placeholder-only example accounts so portable releases do not show fake accounts in the web console.
- Added account-manager regression coverage for the edited-example import path.
- Updated README, API docs, and Windows portable usage notes for manual account configuration.

## v0.1.7 - 2026-08-21

- Pace automatic alias creation evenly across each account's remaining hourly quota instead of choosing a random next run time.
- Added regression coverage for automatic job pacing and independent per-account quota behavior.
- Documented Cookie session maintenance expectations for VPS and proxy deployments.

## v0.1.6 - 2026-08-20

- Persist current-hour per-account quota usage in `data/create_jobs.json`.
- Prevent restarting the program from resetting the local five-alias hourly quota.
- Added scheduler regression coverage for quota restoration after restart.
- Updated README and API documentation to describe persisted quota state.

## v0.1.5 - 2026-08-17

- Fixed automatic alias creation jobs stopping permanently after transient iCloud errors such as HTTP 421.
- Automatic jobs now record the transient error, keep running, release the reserved hourly quota, and retry in the next hour.
- Added scheduler regression coverage for the HTTP 421 backoff path while keeping permanent authentication errors terminal.

## v0.1.4 - 2026-08-13 23:35 +08:00

- Added full-message detail APIs for single-message and batch body loading.
- Added server-side message detail caching with defensive copies to avoid cache mutation.
- Updated IMAP mail reading to fetch full message bodies on demand and in batches.
- Updated the inbox frontend to prefetch visible message bodies in the background.
- Improved verification-code extraction by using full message bodies when available.
- Rebuilt embedded web assets and updated README/API documentation for the message detail flow.
- Added tests for message cache copy safety and updated mail parsing coverage.

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
