# iCloud Prime API

Base URL:

```text
http://127.0.0.1:8081
```

All API responses use this shape:

```json
{
  "success": true,
  "data": {}
}
```

Errors use:

```json
{
  "success": false,
  "message": "error message"
}
```

Do not send real Cookie values or App-specific passwords in public bug reports.

## Accounts

### List Accounts

```http
GET /api/accounts
```

Sensitive fields are redacted from the response.

### Add Account

```http
POST /api/accounts
Content-Type: application/json
```

Body:

```json
{
  "name": "Main account",
  "host": "icloud.com",
  "cookies": "X-APPLE-WEBAUTH-TOKEN=value; X-APPLE-WEBAUTH-USER=value",
  "proxy": ""
}
```

Fields:

- `name`: required display name.
- `host`: optional, usually `icloud.com` or `icloud.com.cn`.
- `cookies`: optional Cookie input as header string or JSON object string.
- `proxy`: optional HTTP/SOCKS5 proxy URL.

### Delete Account

```http
DELETE /api/accounts/:id
```

### Set App-Specific Password

```http
POST /api/accounts/:id/password
Content-Type: application/json
```

Body:

```json
{
  "icloud_email": "your_email@icloud.com",
  "app_password": "xxxx-xxxx-xxxx-xxxx"
}
```

The App-specific password is used for IMAP mail reading.

### Update Cookies

```http
PUT /api/accounts/:id/cookies
Content-Type: application/json
```

Body:

```json
{
  "cookies": {
    "X-APPLE-WEBAUTH-TOKEN": "value",
    "X-APPLE-WEBAUTH-USER": "value",
    "X-APPLE-DS-WEB-SESSION-TOKEN": "value"
  }
}
```

### Login Account

```http
POST /api/accounts/:id/login
Content-Type: application/json
```

Body:

```json
{
  "password": "apple-id-password",
  "otp_code": "123456"
}
```

`otp_code` is optional when 2FA is not required.

## Hide My Email Aliases

### Create Alias

```http
POST /api/create
Content-Type: application/json
```

Body:

```json
{
  "account_id": "acc_1",
  "label": "Example site"
}
```

Response:

```json
{
  "success": true,
  "data": {
    "email": "alias@icloud.com",
    "label": "Example site",
    "created_at": "2026-01-01T00:00:00Z",
    "account_id": "acc_1"
  }
}
```

Single creation, batch creation, and automatic jobs share the same quota:
up to 5 successful alias creations per account per hour.
When the quota is exhausted, single creation returns `429 Too Many Requests`.

### Batch Create Aliases

```http
POST /api/create/batch
Content-Type: application/json
```

Body:

```json
{
  "account_id": "acc_1",
  "count": 5,
  "label_prefix": "Signup"
}
```

Fields:

- `account_id`: required account ID.
- `count`: required number from `1` to `5`.
- `label_prefix`: optional prefix used to generate labels such as `Signup 1`.

Response:

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "requested": 5,
    "created": [
      {
        "email": "alias@icloud.com",
        "label": "Signup 1",
        "created_at": "2026-08-09T10:12:00+08:00",
        "account_id": "acc_1"
      }
    ],
    "created_count": 1,
    "skipped_count": 4,
    "remaining_this_hour": 0,
    "message": "quota was not enough for the full request"
  }
}
```

If the remaining hourly quota is lower than `count`, the API creates what it can
and reports the skipped count.

### List Automatic Create Jobs

```http
GET /api/create/jobs?account_id=acc_1
```

`account_id` is optional. When it is present, the response includes
`remaining_this_hour`.

Response:

```json
{
  "success": true,
  "data": {
    "remaining_this_hour": 5,
    "jobs": [
      {
        "id": "job_abcd1234",
        "account_id": "acc_1",
        "label_prefix": "Auto",
        "mode": "duration",
        "status": "running",
        "duration_hours": 12,
        "created_count": 3,
        "next_run_at": "2026-08-09T10:20:00+08:00",
        "created_at": "2026-08-09T09:00:00+08:00",
        "updated_at": "2026-08-09T10:00:00+08:00"
      }
    ]
  }
}
```

### Create or Update an Automatic Create Job

```http
POST /api/create/jobs
Content-Type: application/json
```

Duration mode:

```json
{
  "account_id": "acc_1",
  "label_prefix": "Auto",
  "mode": "duration",
  "duration_hours": 12
}
```

Daily window mode:

```json
{
  "account_id": "acc_1",
  "label_prefix": "Workday",
  "mode": "daily_window",
  "start_time": "09:00",
  "end_time": "18:00"
}
```

Fields:

- `id`: optional job ID. Omit it to create a new job; provide it to update a job.
- `account_id`: required account ID.
- `label_prefix`: optional label prefix.
- `mode`: required, either `duration` or `daily_window`.
- `duration_hours`: required when `mode` is `duration`.
- `start_time` and `end_time`: required when `mode` is `daily_window`, in `HH:mm` format.

Daily windows can cross midnight, for example `22:00` to `02:00`.
Jobs are stored locally in `data/create_jobs.json` and resume after restart when
their status is `running`.

### Manage Automatic Create Jobs

```http
POST /api/create/jobs/:id/pause
POST /api/create/jobs/:id/resume
DELETE /api/create/jobs/:id
```

Pause and resume return the updated job. Delete returns the removed job ID.

### List Aliases

```http
GET /api/aliases?account_id=acc_1
```

### Deactivate Alias

```http
POST /api/aliases/:id/deactivate
Content-Type: application/json
```

Body:

```json
{
  "account_id": "acc_1"
}
```

### Reactivate Alias

```http
POST /api/aliases/:id/reactivate
Content-Type: application/json
```

Body:

```json
{
  "account_id": "acc_1"
}
```

### Delete Alias

```http
DELETE /api/aliases/:id
Content-Type: application/json
```

Body:

```json
{
  "account_id": "acc_1"
}
```

## Mail

### Read Inbox

```http
GET /api/inbox?account_id=acc_1&alias=alias@icloud.com&limit=20&days=7
```

Query parameters:

- `account_id`: required.
- `alias`: optional; when present, filters messages sent to that alias.
- `folder`: optional; defaults to `inbox`.
- `limit`: optional; defaults to `20`.
- `days`: optional; defaults to `7` for IMAP mode.
- `body`: optional; set to `1` or `true` to include parsed body text in the
  returned message previews when using IMAP.

Read order:

1. IMAP through App-specific password.
2. Web API through Cookie fallback.

Response includes `method` with `imap` or `web_api`.

### Read One Full Message

```http
GET /api/messages/:id?account_id=acc_1&folder=INBOX
```

Path and query parameters:

- `:id`: required IMAP UID. Values such as `INBOX:42` are also accepted and
  automatically split into folder and UID.
- `account_id`: required account ID.
- `folder`: optional IMAP folder name; defaults to `INBOX`.

Response:

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "message": {
      "id": "INBOX:42",
      "uid": "42",
      "folder": "INBOX",
      "subject": "Your verification code",
      "from": "Example <noreply@example.com>",
      "to": "alias@icloud.com",
      "date": "2026-08-13T15:30:00Z",
      "preview": "Your code is 123456",
      "body": "Your code is 123456",
      "content_type": "text/plain"
    },
    "method": "imap",
    "cached": false
  }
}
```

Full-message responses are cached briefly so repeated modal opens do not need to
fetch the same IMAP body again.

### Batch Read Full Messages

```http
POST /api/messages
Content-Type: application/json
```

Body:

```json
{
  "account_id": "acc_1",
  "messages": [
    { "uid": "42", "folder": "INBOX" },
    { "uid": "43", "folder": "Junk" }
  ]
}
```

Notes:

- `messages` is required and may contain up to `50` message references.
- `folder` defaults to `INBOX` for each message.
- Cached messages are returned immediately; uncached messages are grouped by
  folder and fetched in batches through IMAP.

### List Mailboxes

```http
GET /api/mailboxes?account_id=acc_1
```

Requires an App-specific password.

## System

### Reload Account Configuration

```http
POST /api/reload
```

Reloads `data/accounts.json` without restarting the process.
