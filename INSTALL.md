# Installing mailtriaged

This guide sets up `mailtriaged` on macOS with IMAP, local rules, an external classifier, optional Telegram notifications, and launchd supervision.

## 1. Build or Install

Install from the module:

```bash
go install github.com/venky/mailtriaged@latest
```

Or build from a checkout:

```bash
go build -o ~/bin/mailtriaged .
```

The bundled OpenAI classifier is optional but useful as a starting point:

```bash
go build -o ~/bin/classifier-openai ./cmd/classifier-openai/
```

Make sure the install directory is on your `PATH`:

```bash
export PATH="$HOME/bin:$PATH"
```

## 2. Create Directories

```bash
mkdir -p ~/.config/mailtriaged/rules
mkdir -p ~/.local/state/mailtriaged/logs
```

Config and rules live under `~/.config/mailtriaged/`. Runtime state and logs live under `~/.local/state/mailtriaged/`.

## 3. Store Secrets

Secrets should come from commands, not plain text config. On macOS, Keychain works well:

```bash
security add-generic-password -a you@example.com -s imap-mailtriaged -w
security add-generic-password -s openai-mailtriaged -w
security add-generic-password -s telegram-mailtriaged -w
```

The OpenAI and Telegram secrets are only needed if you use those integrations.

## 4. Write Config

Create `~/.config/mailtriaged/config.yaml`:

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
    - classifier-openai
    - --model
    - gpt-4o-mini
    - --api-key-command
    - "security find-generic-password -s openai-mailtriaged -w"
  timeout_seconds: 30
  max_body_excerpt_chars: 6000
  # Optional guidance appended to the default classifier prompt.
  # instruction: "Mailing lists should generally be classified as ignore."

notifications:
  telegram:
    enabled: false
    bot_token_command: "security find-generic-password -s telegram-mailtriaged -w"
    chat_id: "YOUR_CHAT_ID"

summary:
  enabled: false
  send_time: "08:00"
  timezone: "UTC"

runtime:
  rules_reload_seconds: 30
  reconnect_backoff_seconds: [5, 15, 60, 300]
  disable_rules: false
```

Any classifier can be used if it reads the documented JSON request from stdin and writes the documented JSON response to stdout. See `mailtriaged_design.md` for the full contract.

## 5. Add Starter Rules

Create `~/.config/mailtriaged/rules/000-manual.yaml`:

```yaml
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

Rules are loaded in lexical filename order. First match wins, so put narrow high-priority rules before broader ones.

## 6. Validate and Test

Validate rules:

```bash
mailtriaged rules validate
mailtriaged rules list
```

Test an email file:

```bash
mailtriaged classify --file sample.eml
mailtriaged classify --file sample.eml --dry-run
```

`--dry-run` applies local rules but skips the classifier. This is useful for checking whether a rule prevents an LLM-backed classifier call.

## 7. Run

Run in the foreground first:

```bash
mailtriaged run
```

When the foreground run is healthy, install a launchd user agent:

```bash
mailtriaged launchd install
mailtriaged launchd status
```

Useful launchd commands:

```bash
launchctl list com.venky.mailtriaged
tail -f ~/.local/state/mailtriaged/logs/stderr.log
mailtriaged launchd uninstall
```

## 8. Review Classifier Suggestions

When no local rule matches, `mailtriaged` calls the classifier and can append a candidate rule to `rules/800-llm-candidates.yaml`. Review these periodically:

```bash
mailtriaged rules review
mailtriaged rules tui
mailtriaged rules promote <candidate-id>
mailtriaged rules reject <candidate-id>
```

Promoting good candidates is how the system reduces future classifier and LLM usage: similar future emails match local YAML rules instead of going back through the classifier.

## Troubleshooting

Check that the config can be loaded and rules validate:

```bash
mailtriaged rules validate --config ~/.config/mailtriaged/config.yaml
```

Check logs:

```bash
tail -f ~/.local/state/mailtriaged/logs/stderr.log
```

Check Keychain commands directly:

```bash
security find-generic-password -a you@example.com -s imap-mailtriaged -w
security find-generic-password -s openai-mailtriaged -w
```

If classification fails, `mailtriaged` treats the message as `needs_review`. Check the classifier command, API key, timeout, and stderr logs.
