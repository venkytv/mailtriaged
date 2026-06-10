# mailtriaged

Personal mail triage daemon for macOS. Watches IMAP mailboxes via IDLE, applies local YAML rules first, and calls an external classifier CLI only for unmatched emails.

## Setup

See [INSTALL.md](INSTALL.md) for full setup instructions, including config, Keychain secrets, starter rules, launchd installation, and troubleshooting.

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

# Interactively review all candidates (TUI)
mailtriaged rules tui

# Promote a safe candidate to active rules
mailtriaged rules promote <candidate-id>

# Promote but override the suggested action
mailtriaged rules promote <candidate-id> --action ignore

# Reject a broad/unwanted candidate
mailtriaged rules reject <candidate-id>

# Classifier statistics (model usage, escalation rate, latency)
mailtriaged stats              # last 7 days
mailtriaged stats --days 30    # last 30 days
```

## How it works

1. Daemon watches IMAP folders via IDLE
2. New email arrives → rules evaluated in file order, first match wins
3. No rule matches → external classifier CLI is called (JSON in/out on stdin/stdout)
4. Classifier suggests a rule → appended to `rules/800-llm-candidates.yaml`
5. Actions dispatched: `alert_now` → immediate Telegram, `daily_summary` → batched, `ignore` → silent, `needs_review` → included in daily summary
6. You periodically run `rules review` to promote or reject candidates, so future matching emails are handled locally without another classifier call

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

**Tiered classification** — use a cheap model for obvious emails, escalate to a capable model when uncertain:
```yaml
classifier:
  command:
    - classifier-openai
    - --model
    - gpt-4o-mini
    - --fallback-model
    - gpt-4o
    - --confidence-threshold
    - "0.7"
  timeout_seconds: 60
```

The primary model self-reports a confidence score (0.0–1.0). If it falls below the threshold, the classifier automatically re-runs the same request with the fallback model. Use `--verbose` to see which model handled each email.

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
| `--fallback-model` | | More capable model for low-confidence classifications |
| `--confidence-threshold` | `0.7` | Confidence below this triggers the fallback model |
| `--api-key-command` | | Shell command to retrieve API key |
| `--base-url` | `https://api.openai.com/v1` | OpenAI-compatible API base URL |
| `--verbose` | `false` | Print debug info to stderr |

The `--base-url` flag lets you point at any OpenAI-compatible API (Azure OpenAI, Ollama, etc.).

## Classifier Instruction

The classifier receives a system prompt that tells it how to classify emails. The built-in default is:

> Classify this email for a single user's personal mail triage. Return strict JSON only. If you suggest a rule, keep it narrow and only use supported match fields.

You can add domain-specific guidance via `classifier.instruction` in config. Your text is **appended** to the default — you don't need to repeat the base prompt:

```yaml
classifier:
  instruction: |
    Mailing lists should generally be classified as ignore.
    Emails from @mycompany.com are always alert_now.
```

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
