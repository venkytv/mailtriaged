---
name: rules-cleanup
description: Clean up production mailtriaged rules on mort. Fetch current rules, identify issues, write cleaned file, validate, and deploy.
---

# Rules Cleanup

Clean up the production rules file on mort. The daemon's classifier adds rules at runtime with auto-generated IDs that accumulate issues over time.

## What to look for

1. **Redundant rules**: a later rule can never fire because an earlier rule already matches the same sender (first-match-wins)
2. **Shadowed rules**: same sender + overlapping subject match with a different action — the later one is dead
3. **Overly narrow subject filters on marketing/newsletter senders**: classifier-generated rules include campaign-specific subject words (e.g. `subject_contains_any: [best-value beaches, summer]`). For senders where ALL mail should get the same action, remove the subject filter so future campaigns match. Be careful with senders that have multiple rules with different actions (e.g. Wise returns=ignore vs Wise promos=daily_summary) — those need the subject filter to distinguish
4. **Malformed match fields**: raw header values that weren't parsed correctly (e.g. a `list_id` containing the full header instead of the extracted ID)
5. **Auto-generated IDs**: rename `candidate_YYYYMMDD_HHMMSS` to descriptive kebab-case IDs matching the style of reviewed rules (e.g. `marketing_british_airways`, `royalmail_delivery_today`)
6. **Ordering**: group rules by action (alert_now first, then daily_summary, then ignore). Within a sender, more specific rules must come before broader ones

## Steps

1. **Fetch current production rules**:
   ```
   mcp__mort__run_command: cat /Users/mort/.config/mailtriaged/rules/100-active.yaml
   ```

2. **Analyse and present findings** to the user. Group by issue type. Ask user to confirm cleanup scope before proceeding.

3. **Write cleaned file to /tmp/100-active.yaml**. Never write production rules to testdata/.

4. **Validate and lint**:
   ```bash
   python3 scripts/rules-lint.py /tmp/100-active.yaml
   python3 scripts/rules-diff-production.py /tmp/100-active.yaml
   ```
   Show the diff output to the user.

5. **Deploy** via ansible (creates backup on mort):
   ```bash
   cd ~/src/ansible && ./deploy.sh mailtriaged-rules.yml -e "rules_src=/tmp/100-active.yaml" --diff
   ```

6. **Verify** on production:
   ```
   mcp__mort__run_command: /Users/mort/.local/bin/mailtriaged rules validate
   ```
