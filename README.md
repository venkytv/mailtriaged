# mailtriaged

Personal mail triage daemon for macOS. Watches IMAP mailboxes via IDLE, applies local YAML rules, and calls an external classifier CLI for unmatched emails.

## Setup

**1. Install the binary:**
```bash
go install github.com/venky/mailtriaged@latest
# or from the repo:
go build -o ~/bin/mailtriaged .
```

**2. Create config directory:**
```bash
mkdir -p ~/.config/mailtriaged/rules
```

**3. Store secrets in macOS Keychain:**
```bash
# IMAP password
security add-generic-password -a you@example.com -s imap-mailtriaged -w

# Telegram bot token (if using notifications)
security add-generic-password -s telegram-mailtriaged -w
```

**4. Write config:**
```yaml
# ~/.config/mailtriaged/config.yaml
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

notifications:
  telegram:
    enabled: true
    bot_token_command: "security find-generic-password -s telegram-mailtriaged -w"
    chat_id: "YOUR_CHAT_ID"

summary:
  enabled: true
  send_time: "08:00"
  timezone: "Europe/London"

runtime:
  reconnect_backoff_seconds: [5, 15, 60, 300]
```

**5. Write initial rules:**
```yaml
# ~/.config/mailtriaged/rules/000-manual.yaml
rules:
  - id: bank_alerts
    enabled: true
    description: "Immediate alert for bank emails"
    match:
      from_domain: mybank.com
      subject_contains_any:
        - declined
        - fraud
    action: alert_now
    source: manual

  - id: github_dependabot_ignore
    enabled: true
    description: "Ignore Dependabot alerts"
    match:
      from_email: notifications@github.com
      subject_contains_all:
        - dependabot
        - alert
    action: ignore
    source: manual
```

## CLI Commands

```bash
# Validate rules
mailtriaged rules validate

# List all loaded rules
mailtriaged rules list

# Test classification against a .eml file
mailtriaged classify --file sample.eml
mailtriaged classify --file sample.eml --dry-run   # skip classifier

# Run the daemon (foreground)
mailtriaged run

# Install as launchd agent (starts on login, auto-restarts)
mailtriaged launchd install
mailtriaged launchd status
mailtriaged launchd uninstall

# Send daily summary immediately
mailtriaged summary send

# Review classifier-suggested candidate rules
mailtriaged rules review

# Promote a safe candidate to active rules
mailtriaged rules promote <candidate-id>

# Reject a broad/unwanted candidate
mailtriaged rules reject <candidate-id>
```

## How it works

1. Daemon watches IMAP folders via IDLE
2. New email arrives → rules evaluated in file order, first match wins
3. No rule matches → external classifier CLI is called (JSON in/out on stdin/stdout)
4. Classifier suggests a rule → appended to `rules/800-llm-candidates.yaml`
5. Actions dispatched: `alert_now` → immediate Telegram, `daily_summary` → batched, `ignore` → silent, `needs_review` → included in daily summary
6. You periodically run `rules review` to promote or reject candidates

## Bundled Classifier (OpenAI)

A default classifier using the OpenAI API is included at `cmd/classifier-openai/`. It uses function calling (tool use) to enforce the response schema.

**Build:**
```bash
go build -o ~/bin/classifier-openai ./cmd/classifier-openai/
```

**Configure mailtriaged to use it:**
```yaml
classifier:
  command:
    - classifier-openai
    - --model
    - gpt-4o-mini
  timeout_seconds: 30
```

**API key** — set `OPENAI_API_KEY` env var, or use Keychain:
```bash
security add-generic-password -s openai-mailtriaged -w

# then in classifier command:
classifier:
  command:
    - classifier-openai
    - --api-key-command
    - "security find-generic-password -s openai-mailtriaged -w"
```

**Flags:**
| Flag | Default | Description |
|---|---|---|
| `--model` | `gpt-4o-mini` | OpenAI model name |
| `--api-key-command` | | Shell command to retrieve API key |
| `--base-url` | `https://api.openai.com/v1` | OpenAI-compatible API base URL |
| `--verbose` | `false` | Print debug info to stderr |

The `--base-url` flag lets you point at any OpenAI-compatible API (Azure OpenAI, Ollama, etc.).

## Writing a Custom Classifier

Any program that reads JSON from stdin and writes JSON to stdout works. The schema is defined in `mailtriaged_design.md`. Use `classifier-openai` as a reference implementation.

**stdin** (mailtriaged sends):
```json
{
  "schema_version": 1,
  "message": { "from": {...}, "subject": "...", "body_excerpt": "...", ... },
  "valid_actions": ["alert_now", "daily_summary", "ignore", "needs_review"],
  "rule_capabilities": { "supported_match_fields": [...], "regex_supported": false },
  "instruction": "Classify this email..."
}
```

**stdout** (your classifier returns):
```json
{
  "schema_version": 1,
  "action": "ignore",
  "reason": "Recurring dependency alert",
  "summary": null,
  "suggested_rule": {
    "id_hint": "github_dependabot_ignore",
    "description": "Ignore Dependabot alerts",
    "action": "ignore",
    "safety": "narrow",
    "match": { "from_email": "notifications@github.com", "subject_contains_all": ["dependabot"] }
  }
}
```

Exit 0 on success. Non-zero exit, invalid JSON, or timeout → mailtriaged treats it as `needs_review`.

## Watching logs

```bash
tail -f ~/.local/state/mailtriaged/logs/stderr.log
```

## Actions

| Action | Behavior |
|---|---|
| `alert_now` | Immediate Telegram notification |
| `daily_summary` | Queued for daily digest |
| `needs_review` | Shown in daily digest under "Needs review" |
| `ignore` | Logged silently, no notification |
