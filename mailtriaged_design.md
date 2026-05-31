# mailtriaged Design

## Purpose

`mailtriaged` is a small personal mail triage daemon for macOS.

Its purpose is not to replace a mail client or build a general-purpose priority inbox. Its purpose is to reduce LLM token burn by applying local rules first and calling an LLM-backed classifier only when local rules do not already cover the incoming email.

The daemon is intentionally simple:

```text
IMAP IDLE
  ↓
fetch new message
  ↓
apply local rules from files
  ↓
matched?
  ├─ yes → apply action
  └─ no  → call external classifier CLI
            ↓
          store decision
          append candidate rule
          apply action
```

The external classifier will likely be Hermes, but `mailtriaged` must not know or care about Hermes specifically. It should execute a configured command, send JSON to stdin, and expect JSON on stdout.

---

## Goals

- Watch IMAP mailboxes in near real time using IMAP IDLE.
- Apply deterministic local rules before invoking any LLM.
- Invoke an external classifier only for unmatched emails.
- Support immediate alerting for time-critical mail.
- Support daily summaries for important-but-not-urgent mail.
- Ignore unimportant mail silently.
- Store rule candidates suggested by the external classifier.
- Keep rules as editable files, not hidden in SQLite.
- Keep enough state to avoid duplicate processing and audit decisions.
- Run cleanly as a macOS `launchd` user agent.

---

## Non-goals

- No full mail client.
- No local Maildir requirement.
- No notmuch indexing.
- No Sieve server requirement.
- No Dovecot/Pigeonhole.
- No generic ML classifier.
- No autonomous broad-rule promotion in v1.
- No attachment analysis in v1.
- No attempt at perfect classification.

---

## Required components

```text
mailtriaged      Go daemon
SQLite           local state/event log
YAML files       active rules and candidate rules
CLI classifier   generic external command, likely Hermes
Telegram         notification sink, at least initially
launchd          macOS process supervision
```

---

## Filesystem layout

Use an XDG-style split between config and state.

```text
~/.config/mailtriaged/
  config.yaml
  rules/
    000-manual.yaml
    100-active.yaml
    800-llm-candidates.yaml
    900-rejected.yaml

~/.local/state/mailtriaged/
  mailtriaged.db
  logs/
    stdout.log
    stderr.log
    mailtriaged.log
```

Rules should be suitable for Git version control. SQLite should not be version-controlled.

---

## Configuration

Example `~/.config/mailtriaged/config.yaml`:

```yaml
imap:
  host: imap.example.com
  port: 993
  username: you@example.com
  password_command: "security find-generic-password -a you@example.com -s imap-mailtriaged -w"
  folders:
    - INBOX

classifier:
  command:
    - hermes
    - run
    - mail-triage
  timeout_seconds: 30
  max_body_excerpt_chars: 6000
  # Optional extra instruction appended to the default classifier prompt.
  # The default instruction is:
  #   "Classify this email for a single user's personal mail triage.
  #    Return strict JSON only. If you suggest a rule, keep it narrow
  #    and only use supported match fields."
  # Use this to add domain-specific guidance without replacing the base prompt.
  instruction: "Mailing lists should generally be classified as ignore."

notifications:
  telegram:
    enabled: true
    bot_token_command: "security find-generic-password -s telegram-mailtriaged -w"
    chat_id: "123456789"

summary:
  enabled: true
  send_time: "08:00"
  timezone: "Europe/London"

runtime:
  rules_reload_seconds: 30
  reconnect_backoff_seconds:
    - 5
    - 15
    - 60
    - 300
  disable_rules: false
```

Secrets should come from macOS Keychain via command execution. They should not be stored directly in config files.

---

## Rule files

Rules live in YAML files under `rules/`. Files are loaded in lexical order.

Manual rules should generally live in:

```text
rules/000-manual.yaml
```

Active reviewed rules should live in:

```text
rules/100-active.yaml
```

LLM-suggested rule candidates should be appended to:

```text
rules/800-llm-candidates.yaml
```

Rejected candidates can be moved to:

```text
rules/900-rejected.yaml
```

---

## Active rule format

Example:

```yaml
rules:
  - id: github_dependabot_repo_x_ignore
    enabled: true
    description: "Ignore GitHub Dependabot alerts for repo-x"
    match:
      from_email: notifications@github.com
      list_id: "owner/repo-x"
      subject_contains_all:
        - dependabot
        - alert
    action: ignore
    source: manual
```

Supported actions:

```text
alert_now
 daily_summary
ignore
needs_review
```

Supported match fields in v1:

```yaml
from_email: "notifications@github.com"
from_domain: "github.com"
to_contains: "me@example.com"
cc_contains: "me@example.com"
list_id: "owner/repo-x"
subject_contains_all:
  - dependabot
  - alert
subject_contains_any:
  - outage
  - incident
header_equals:
  x-github-reason: security_alert
header_contains:
  x-some-header: useful-fragment
```

Initial implementation should avoid arbitrary regex unless there is a specific need.

---

## Candidate rule format

Candidate rules are generated by the external classifier and appended to `800-llm-candidates.yaml`.

Example:

```yaml
candidates:
  - id: candidate_20260531_091500_001
    created_at: "2026-05-31T09:15:00+01:00"
    source_message_id: "<abc@example.com>"
    proposed_by: classifier
    action: ignore
    reason: "Recurring GitHub Dependabot alert; not time-critical."
    safety: narrow
    match:
      from_email: notifications@github.com
      list_id: "owner/repo-x"
      subject_contains_all:
        - dependabot
        - alert
```

The daemon may append candidates, but should not silently promote broad candidates into active rules in v1.

---

## SQLite responsibilities

SQLite stores state and audit data. It should not be the primary policy store.

SQLite stores:

- IMAP UID processing state.
- Message metadata.
- Classification decisions.
- Rule hit counts.
- LLM/classifier call records.
- Daily summary queue.
- Notification delivery status.

SQLite does not store:

- Active rules.
- Manual policy.
- Candidate rule files as the source of truth.

---

## SQLite schema

Initial schema:

```sql
CREATE TABLE messages (
  id INTEGER PRIMARY KEY,
  account TEXT NOT NULL,
  folder TEXT NOT NULL,
  imap_uid INTEGER NOT NULL,
  message_id TEXT,
  from_email TEXT,
  from_domain TEXT,
  subject TEXT,
  received_at TEXT,
  seen_at TEXT NOT NULL,
  UNIQUE(account, folder, imap_uid)
);

CREATE TABLE decisions (
  id INTEGER PRIMARY KEY,
  message_id INTEGER NOT NULL,
  action TEXT NOT NULL,
  source TEXT NOT NULL, -- rule | classifier | manual
  rule_id TEXT,
  reason TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(message_id) REFERENCES messages(id)
);

CREATE TABLE summary_items (
  id INTEGER PRIMARY KEY,
  message_id INTEGER NOT NULL,
  summary TEXT NOT NULL,
  sent INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  sent_at TEXT,
  FOREIGN KEY(message_id) REFERENCES messages(id)
);

CREATE TABLE rule_hits (
  id INTEGER PRIMARY KEY,
  rule_id TEXT NOT NULL,
  message_id INTEGER NOT NULL,
  action TEXT NOT NULL,
  hit_at TEXT NOT NULL,
  FOREIGN KEY(message_id) REFERENCES messages(id)
);

CREATE TABLE classifier_calls (
  id INTEGER PRIMARY KEY,
  message_id INTEGER NOT NULL,
  command TEXT NOT NULL,
  request_json TEXT NOT NULL,
  response_json TEXT,
  exit_code INTEGER,
  stderr TEXT,
  duration_ms INTEGER,
  created_at TEXT NOT NULL,
  FOREIGN KEY(message_id) REFERENCES messages(id)
);

CREATE TABLE notifications (
  id INTEGER PRIMARY KEY,
  message_id INTEGER NOT NULL,
  channel TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT,
  created_at TEXT NOT NULL,
  sent_at TEXT,
  FOREIGN KEY(message_id) REFERENCES messages(id)
);
```

---

## Daemon startup flow

On startup:

1. Load config.
2. Resolve secrets through configured commands.
3. Open SQLite database.
4. Run schema migrations.
5. Load rule files from `rules/*.yaml`.
6. Validate rule syntax.
7. Connect to IMAP.
8. Select configured folders.
9. Start IMAP IDLE loop.
10. Start rule reload ticker.
11. Start daily summary scheduler.

---

## IMAP flow

For each configured folder:

1. Connect to IMAP over TLS.
2. Authenticate.
3. Select folder.
4. Enter IDLE.
5. On new message notification, exit IDLE.
6. Fetch new message UIDs.
7. For each unseen UID:
   - fetch headers
   - store message metadata
   - classify
   - re-enter IDLE

The daemon should deduplicate using:

```text
account + folder + imap_uid
```

Reconnect handling should use configured backoff values.

---

## Message extraction

For each message, extract:

```text
From
To
Cc
Subject
Date
Message-ID
List-ID
Auto-Submitted
Precedence
X-* headers, preserved as lower-case keys
text/plain body excerpt
```

Body handling in v1:

1. Prefer `text/plain`.
2. If absent, strip basic text from `text/html`.
3. Ignore attachments.
4. Limit body excerpt to `classifier.max_body_excerpt_chars`.

---

## Rule evaluation

Rule evaluation should be deterministic and explainable.

Algorithm:

1. If `runtime.disable_rules` is true, skip rules.
2. Load enabled rules in lexical file order.
3. Evaluate each rule against message metadata.
4. First matching rule wins.
5. Store `decisions.source = rule`.
6. Store matching `rule_id`.
7. Store `rule_hits` row.
8. Execute action.

Matching should be case-insensitive for email addresses, domains, subjects, and header names.

---

## Generic classifier CLI contract

The classifier integration must be generic.

`mailtriaged` executes the configured command:

```yaml
classifier:
  command:
    - hermes
    - run
    - mail-triage
```

The daemon sends a single JSON object to the command over stdin.

The daemon expects a single JSON object on stdout.

The daemon should capture stderr for diagnostics.

The daemon should enforce a timeout.

The daemon should treat non-zero exit status, invalid JSON, or timeout as classifier failure.

---

## Classifier stdin JSON

Example request sent to stdin:

```json
{
  "schema_version": 1,
  "message": {
    "account": "you@example.com",
    "folder": "INBOX",
    "imap_uid": 12345,
    "message_id": "<abc@example.com>",
    "from": {
      "name": "GitHub",
      "email": "notifications@github.com",
      "domain": "github.com"
    },
    "to": ["you@example.com"],
    "cc": [],
    "subject": "[repo-x] Dependabot alert for openssl",
    "received_at": "2026-05-31T09:15:00+01:00",
    "headers": {
      "list-id": "owner/repo-x",
      "auto-submitted": "auto-generated"
    },
    "body_excerpt": "Body text here..."
  },
  "valid_actions": [
    "alert_now",
    "daily_summary",
    "ignore",
    "needs_review"
  ],
  "rule_capabilities": {
    "supported_match_fields": [
      "from_email",
      "from_domain",
      "to_contains",
      "cc_contains",
      "list_id",
      "subject_contains_all",
      "subject_contains_any",
      "header_equals",
      "header_contains"
    ],
    "regex_supported": false
  },
  "instruction": "Classify this email for a single user's personal mail triage. Return strict JSON only. If you suggest a rule, keep it narrow and only use supported match fields.\n\nMailing lists should generally be classified as ignore."
}
```

The `instruction` field always starts with the built-in default prompt. If `classifier.instruction` is set in `config.yaml`, it is appended after the default (separated by a blank line).

---

## Classifier stdout JSON

Expected response on stdout:

```json
{
  "schema_version": 1,
  "action": "ignore",
  "reason": "Recurring dependency alert; not time-critical.",
  "summary": null,
  "suggested_rule": {
    "id_hint": "github_dependabot_repo_x_ignore",
    "description": "Ignore GitHub Dependabot alerts for repo-x",
    "action": "ignore",
    "safety": "narrow",
    "match": {
      "from_email": "notifications@github.com",
      "list_id": "owner/repo-x",
      "subject_contains_all": ["dependabot", "alert"]
    }
  }
}
```

For `daily_summary`:

```json
{
  "schema_version": 1,
  "action": "daily_summary",
  "reason": "Useful but not time-critical.",
  "summary": "GitHub reported a non-urgent CI status update for repo-x.",
  "suggested_rule": null
}
```

For `alert_now`:

```json
{
  "schema_version": 1,
  "action": "alert_now",
  "reason": "This appears to require immediate user attention.",
  "summary": "Bank reports a declined transaction requiring review.",
  "suggested_rule": null
}
```

---

## Classifier failure handling

If classifier invocation fails:

- Store the failed call in `classifier_calls`.
- Do not ignore the message.
- Treat as `needs_review`.
- Add to daily summary or send low-priority notification.

Failure cases:

```text
timeout
non-zero exit code
invalid JSON
missing action
unsupported action
suggested_rule uses unsupported match fields
```

---

## Action handling

### `alert_now`

Send an immediate Telegram notification.

Include:

```text
From
Subject
Reason
Summary if present
```

Store notification result.

### `daily_summary`

Insert a row into `summary_items`.

Do not notify immediately.

### `ignore`

Store decision only.

No notification.

### `needs_review`

For v1, treat as daily summary item under a separate “Needs review” section.

---

## Daily summary

The daemon should send a daily summary at the configured local time.

Flow:

1. Query unsent `summary_items`.
2. Group by action/reason/source if useful.
3. Send one Telegram message.
4. Mark items as sent.

Example summary format:

```text
Daily mail summary

Needs review
- From: ...
  Subject: ...
  Reason: ...

Summary
- From: ...
  Subject: ...
  Summary: ...
```

---

## Candidate rule lifecycle

1. Classifier returns `suggested_rule`.
2. Daemon validates the rule shape.
3. Daemon appends it to `rules/800-llm-candidates.yaml`.
4. Separate consolidation task reviews candidates.
5. Approved/sanitised rules move to `rules/100-active.yaml`.
6. Rejected rules move to `rules/900-rejected.yaml`.
7. Daemon reloads rules automatically on interval.

The daemon should not require restart for rule changes.

---

## Rule consolidation

Rule consolidation should be a separate command or external task.

Possible command:

```text
mailtriaged rules consolidate
```

Inputs:

```text
rules/100-active.yaml
rules/800-llm-candidates.yaml
recent decisions from SQLite
```

Outputs:

```text
updated rules/100-active.yaml
pruned rules/800-llm-candidates.yaml
updated rules/900-rejected.yaml if needed
```

This task may itself use Hermes or another LLM. It is intentionally out of the hot path.

---

## Safety rules for candidate promotion

The consolidation task should reject or require manual review for broad candidates.

Risky examples:

```yaml
match:
  from_domain: github.com
action: ignore
```

```yaml
match:
  subject_contains_any:
    - alert
action: ignore
```

Better examples:

```yaml
match:
  from_email: notifications@github.com
  list_id: "owner/repo-x"
  subject_contains_all:
    - dependabot
    - alert
action: ignore
```

Promotion policy:

- Manual rules always win by file order.
- Ignore rules should be narrow.
- Alert rules should usually be manually written or reviewed.
- Domain-only ignore rules should be rejected by default.
- Subject-only ignore rules should be rejected by default.

---

## CLI commands

Initial binary commands:

```text
mailtriaged run
mailtriaged classify --file sample.eml
mailtriaged rules validate
mailtriaged rules list
mailtriaged summary send
```

Later commands:

```text
mailtriaged rules consolidate
mailtriaged db inspect
mailtriaged test-classifier --file sample.eml
```

---

## `classify --file` behaviour

This command should make local testing easy.

Flow:

1. Read `.eml` file.
2. Extract metadata/body excerpt.
3. Apply rules.
4. If no rule matches, optionally call classifier unless disabled.
5. Print decision JSON.

Example output:

```json
{
  "action": "ignore",
  "source": "rule",
  "rule_id": "github_dependabot_repo_x_ignore",
  "reason": "Matched active rule"
}
```

---

## launchd integration

Install a user LaunchAgent at:

```text
~/Library/LaunchAgents/com.venky.mailtriaged.plist
```

Example:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.venky.mailtriaged</string>

  <key>ProgramArguments</key>
  <array>
    <string>/Users/venky/bin/mailtriaged</string>
    <string>run</string>
  </array>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>/Users/venky/.local/state/mailtriaged/logs/stdout.log</string>

  <key>StandardErrorPath</key>
  <string>/Users/venky/.local/state/mailtriaged/logs/stderr.log</string>
</dict>
</plist>
```

---

## Implementation phases

### Phase 1: local rule engine

Build:

- config loader
- rule file loader
- rule validator
- `.eml` parser
- local classifier
- `classify --file`

Success criterion:

```text
mailtriaged classify --file samples/github.eml
```

returns a rule-based decision.

---

### Phase 2: generic classifier CLI

Build:

- command execution
- JSON stdin request
- JSON stdout response parsing
- timeout handling
- stderr capture
- classifier failure handling
- candidate rule append

Success criterion:

```text
unknown sample mail → external classifier → decision + candidate rule
```

---

### Phase 3: SQLite event log

Build:

- schema migrations
- message insert/dedupe
- decision logging
- rule hit logging
- classifier call logging
- summary queue

Success criterion:

```text
all decisions are auditable after restart
```

---

### Phase 4: IMAP IDLE

Build:

- IMAP TLS connection
- authentication
- folder selection
- IDLE loop
- reconnect/backoff
- UID dedupe
- header/body fetch

Success criterion:

```text
new incoming mail triggers classification within seconds
```

---

### Phase 5: Telegram notifications and daily summary

Build:

- Telegram send function
- `alert_now` handling
- `daily_summary` queue handling
- scheduled summary send

Success criterion:

```text
urgent mail sends immediate Telegram alert
summary mail appears in daily digest
ignored mail stays silent
```

---

### Phase 6: launchd

Build:

- install instructions
- sample plist
- logging paths
- restart behaviour

Success criterion:

```text
mailtriaged starts on login and survives network interruptions
```

---

### Phase 7: consolidation workflow

Build or document:

- candidate review workflow
- optional LLM-assisted consolidation
- Git diff review
- rule validation before activation

Success criterion:

```text
candidate rules can be safely promoted into active rules
```

---

## MVP acceptance test

1. Start the daemon.
2. Send a known ignored test email.
3. Confirm no Telegram alert.
4. Send an unknown urgent-looking email.
5. Confirm classifier CLI is called.
6. Confirm Telegram alert is sent.
7. Confirm classifier response is logged.
8. Confirm candidate rule appears in `800-llm-candidates.yaml`.
9. Restart daemon.
10. Confirm old UID is not reprocessed.
11. Move candidate rule into `100-active.yaml`.
12. Send similar mail.
13. Confirm classifier is not called.

---

## Main design principle

`mailtriaged` should remain dumb.

It should:

- receive mail
- apply file-based rules
- call a configured classifier CLI on misses
- store decisions
- notify
- append candidate rules

It should not:

- understand email semantics
- do complex rule synthesis
- hide policy in SQLite
- become a mail client
- require a local mail stack

The intelligence belongs in the external classifier and in the separate consolidation workflow, not in the daemon hot path.

