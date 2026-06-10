#!/usr/bin/env python3
"""Lint a mailtriaged rules file for common issues.

Checks:
  - YAML parsing and daemon validation (via `go run . rules validate`)
  - Duplicate rule IDs
  - Redundant rules (later rule shadowed by earlier broader match)

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
    """Find rules shadowed by an earlier broader rule."""
    warnings = []
    for i, earlier in enumerate(rules):
        if not is_enabled(earlier):
            continue
        for later in rules[i + 1 :]:
            if not is_enabled(later):
                continue
            if match_implies(later.get("match", {}), earlier.get("match", {})):
                warnings.append(
                    f"'{later['id']}' is unreachable — "
                    f"'{earlier['id']}' matches a broader or equal set of messages"
                )
    return warnings


def is_enabled(rule: dict) -> bool:
    return rule.get("enabled", True) is not False


def match_implies(later: dict, earlier: dict) -> bool:
    """Return true if every message matching later also matches earlier."""
    return (
        sender_implies(later, earlier)
        and scalar_fields_imply(later, earlier, ["to_contains", "cc_contains", "list_id"])
        and subject_implies(later, earlier)
        and headers_imply(later, earlier)
    )


def sender_implies(later: dict, earlier: dict) -> bool:
    earlier_email = norm(earlier.get("from_email"))
    later_email = norm(later.get("from_email"))
    earlier_domain = norm(earlier.get("from_domain"))
    later_domain = norm(later.get("from_domain"))

    if earlier_email and earlier_email != later_email:
        return False

    if earlier_domain:
        if later_domain == earlier_domain:
            return True
        if later_email and email_domain(later_email) == earlier_domain:
            return True
        return False

    return True


def scalar_fields_imply(later: dict, earlier: dict, fields: list[str]) -> bool:
    for field in fields:
        earlier_value = norm(earlier.get(field))
        if earlier_value and norm(later.get(field)) != earlier_value:
            return False
    return True


def subject_implies(later: dict, earlier: dict) -> bool:
    earlier_all = norm_list(earlier.get("subject_contains_all"))
    earlier_any = norm_list(earlier.get("subject_contains_any"))
    later_all = norm_list(later.get("subject_contains_all"))
    later_any = norm_list(later.get("subject_contains_any"))

    # Earlier requires all of these terms, so later must guarantee all of them.
    if not earlier_all.issubset(later_all):
        return False

    # Earlier requires none of these terms.
    if not earlier_any:
        return True

    # Later's all-terms already guarantee at least one earlier any-term.
    if earlier_any & later_all:
        return True

    # If later only guarantees one of several any-terms, each possible later term
    # must also satisfy the earlier any-term set.
    if later_any and later_any.issubset(earlier_any):
        return True

    return False


def headers_imply(later: dict, earlier: dict) -> bool:
    later_equals = norm_map(later.get("header_equals"))
    later_contains = norm_map(later.get("header_contains"))
    earlier_equals = norm_map(earlier.get("header_equals"))
    earlier_contains = norm_map(earlier.get("header_contains"))

    for key, earlier_value in earlier_equals.items():
        if later_equals.get(key) != earlier_value:
            return False

    for key, earlier_value in earlier_contains.items():
        later_equal = later_equals.get(key)
        later_contain = later_contains.get(key)
        if later_equal and earlier_value in later_equal:
            continue
        if later_contain and earlier_value in later_contain:
            continue
        return False

    return True


def norm(value: object) -> str:
    return str(value or "").lower()


def norm_list(values: object) -> set[str]:
    return {norm(v) for v in values or []}


def norm_map(values: object) -> dict[str, str]:
    return {norm(k): norm(v) for k, v in (values or {}).items()}


def email_domain(email: str) -> str:
    parts = email.rsplit("@", 1)
    if len(parts) != 2:
        return ""
    return parts[1]


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
    print("\n--- Checking for redundant rules (broader earlier match) ---")
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
