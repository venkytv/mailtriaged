#!/usr/bin/env python3
"""Lint a mailtriaged rules file for common issues.

Checks:
  - YAML parsing and daemon validation (via `go run . rules validate`)
  - Duplicate rule IDs
  - Redundant rules (same from_email, later rule shadowed by earlier broad match)

Usage: scripts/rules-lint.py <rules-file>
"""

import argparse
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

VALIDATE_CONFIG = """\
imap:
  host: imap.example.com
  port: 993
  username: test@example.com
  password_command: "echo test"
  folders: [INBOX]
classifier:
  command: ["echo", "{}"]
  timeout_seconds: 30
  max_body_excerpt_chars: 6000
runtime:
  rules_reload_seconds: 30
"""


def validate_with_daemon(rules_path: Path) -> tuple[bool, str]:
    with tempfile.TemporaryDirectory() as tmpdir:
        tmp = Path(tmpdir)
        (tmp / "config.yaml").write_text(VALIDATE_CONFIG)
        rules_dir = tmp / "rules"
        rules_dir.mkdir()
        shutil.copy(rules_path, rules_dir / rules_path.name)

        result = subprocess.run(
            ["go", "run", ".", "rules", "validate", "--config", str(tmp / "config.yaml")],
            capture_output=True,
            text=True,
        )
        output = (result.stdout + result.stderr).strip()
        return result.returncode == 0, output


def load_rules(rules_path: Path) -> list[dict]:
    with open(rules_path) as f:
        data = yaml.safe_load(f)
    return data.get("rules", [])


def check_duplicate_ids(rules: list[dict]) -> list[str]:
    seen = {}
    dupes = []
    for r in rules:
        rid = r.get("id", "")
        if rid in seen:
            dupes.append(rid)
        seen[rid] = True
    return dupes


def check_redundant_rules(rules: list[dict]) -> list[str]:
    """Find rules shadowed by an earlier broad rule on the same from_email."""
    warnings = []
    for i, earlier in enumerate(rules):
        match_e = earlier.get("match", {})
        email_e = match_e.get("from_email", "")
        if not email_e:
            continue
        has_subject = match_e.get("subject_contains_all") or match_e.get("subject_contains_any")
        if has_subject:
            continue

        for later in rules[i + 1 :]:
            match_l = later.get("match", {})
            if match_l.get("from_email") == email_e:
                warnings.append(
                    f"'{later['id']}' is unreachable — "
                    f"'{earlier['id']}' already matches all mail from {email_e}"
                )
    return warnings


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("rules_file", type=Path, help="Path to a rules YAML file")
    args = parser.parse_args()

    if not args.rules_file.exists():
        print(f"ERROR: File not found: {args.rules_file}", file=sys.stderr)
        sys.exit(1)

    errors = 0
    warnings = 0

    # Daemon validation
    ok, output = validate_with_daemon(args.rules_file)
    if ok:
        print(f"OK: {output}")
    else:
        print(f"FAIL: daemon validation failed:\n{output}")
        errors += 1

    rules = load_rules(args.rules_file)

    # Duplicate IDs
    print("\n--- Checking for duplicate IDs ---")
    dupes = check_duplicate_ids(rules)
    if dupes:
        print("ERROR: Duplicate rule IDs:")
        for d in dupes:
            print(f"  {d}")
        errors += 1
    else:
        print("OK: No duplicate IDs")

    # Redundant rules
    print("\n--- Checking for redundant rules (same sender, broader earlier rule) ---")
    redundant = check_redundant_rules(rules)
    if redundant:
        for w in redundant:
            print(f"WARNING: {w}")
        warnings += len(redundant)
    else:
        print("OK: No redundant rules found")

    print()
    if errors:
        print(f"RESULT: {errors} error(s), {warnings} warning(s)")
        sys.exit(1)
    elif warnings:
        print(f"RESULT: {warnings} warning(s)")
    else:
        print("RESULT: All checks passed")


if __name__ == "__main__":
    main()
