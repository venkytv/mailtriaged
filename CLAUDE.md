# mailtriaged

Personal mail triage daemon for macOS. Watches IMAP mailboxes, applies local YAML rules first, and calls an external classifier CLI only for unmatched emails.

## Build & Test

```bash
go build ./...          # build all packages
go test ./...           # run all tests
go run . classify --config testdata/config.yaml --file testdata/samples/github_dependabot.eml
go run . rules validate --config testdata/config.yaml
go run . rules list --config testdata/config.yaml
```

## Project Structure

```
cmd/                    CLI commands (cobra)
cmd/classifier-openai/  Standalone OpenAI classifier binary
internal/
  config/               Config loading + secret resolution
  rules/                Rule loading, validation, evaluation
  email/                .eml parsing, header/body extraction
  classify/             Orchestrates rule eval + classifier
testdata/
  config.yaml           Test config
  rules/                Test rule files
  samples/              Sample .eml files
```

## Design

See `mailtriaged_design.md` for the full design document. Implementation follows the phased approach defined there.

## Key Conventions

- Rules live in YAML files, loaded in lexical filename order. First match wins.
- All matching is case-insensitive for emails, domains, subjects, and header names.
- The classifier is a generic CLI: stdin JSON in, stdout JSON out. No Hermes-specific code in the daemon.
- Secrets come from macOS Keychain via command execution, never stored in config files.

## Rules Maintenance

Long-running deployments may accumulate classifier-generated candidate or promoted rules with auto-generated IDs, redundant matches, or overly narrow subject filters. Use the `/rules-cleanup` skill for a generic fetch, analyse, lint, and deploy workflow. Keep hostnames, private paths, and deployment commands in ignored local memory rather than tracked repo files.
