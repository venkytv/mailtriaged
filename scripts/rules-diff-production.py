#!/usr/bin/env python3
"""Diff a local rules file against a deployed rules file over SSH.

Compares rule IDs to show added, removed, and changed rules.
Requires SSH access to the target host.

Usage:
  scripts/rules-diff-production.py <local-rules-file> --remote-host <host> --remote-file <path>
"""

import argparse
import subprocess
import sys
from pathlib import Path

import yaml
def fetch_remote_rules(remote_host: str, remote_path: str, ssh_user: str = "") -> list[dict]:
    host = f"{ssh_user}@{remote_host}" if ssh_user else remote_host
    result = subprocess.run(
        ["ssh", host, "cat", remote_path],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        print(f"ERROR: Failed to fetch remote rules: {result.stderr.strip()}", file=sys.stderr)
        sys.exit(1)
    data = yaml.safe_load(result.stdout)
    return data.get("rules", [])


def load_local_rules(path: Path) -> list[dict]:
    with open(path) as f:
        data = yaml.safe_load(f)
    return data.get("rules", [])


def rules_by_id(rules: list[dict]) -> dict[str, dict]:
    return {r["id"]: r for r in rules}


def summarise_rule(r: dict) -> str:
    match = r.get("match", {})
    parts = []
    if match.get("from_email"):
        parts.append(match["from_email"])
    if match.get("subject_contains_all"):
        parts.append(f"subject_all={match['subject_contains_all']}")
    if match.get("subject_contains_any"):
        parts.append(f"subject_any={match['subject_contains_any']}")
    action = r.get("action", "?")
    return f"{action} | {' '.join(parts)}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("local_file", type=Path, help="Path to local rules YAML file")
    parser.add_argument(
        "--remote-host",
        required=True,
        help="SSH host that has the deployed rules file",
    )
    parser.add_argument(
        "--remote-file",
        required=True,
        help="Path to the deployed rules YAML file on the remote host",
    )
    parser.add_argument(
        "--ssh-user",
        default="",
        help="Optional SSH username; defaults to the current SSH configuration",
    )
    args = parser.parse_args()

    if not args.local_file.exists():
        print(f"ERROR: File not found: {args.local_file}", file=sys.stderr)
        sys.exit(1)

    local_rules = load_local_rules(args.local_file)
    remote_rules = fetch_remote_rules(args.remote_host, args.remote_file, args.ssh_user)

    local_by_id = rules_by_id(local_rules)
    remote_by_id = rules_by_id(remote_rules)

    local_ids = set(local_by_id.keys())
    remote_ids = set(remote_by_id.keys())

    added = sorted(local_ids - remote_ids)
    removed = sorted(remote_ids - local_ids)
    common = sorted(local_ids & remote_ids)

    changed = []
    for rid in common:
        if local_by_id[rid] != remote_by_id[rid]:
            changed.append(rid)

    if not added and not removed and not changed:
        print("No differences.")
        return

    if added:
        print(f"--- Added ({len(added)}) ---")
        for rid in added:
            print(f"  + {rid}: {summarise_rule(local_by_id[rid])}")
        print()

    if removed:
        print(f"--- Removed ({len(removed)}) ---")
        for rid in removed:
            print(f"  - {rid}: {summarise_rule(remote_by_id[rid])}")
        print()

    if changed:
        print(f"--- Changed ({len(changed)}) ---")
        for rid in changed:
            print(f"  {rid}:")
            print(f"    remote: {summarise_rule(remote_by_id[rid])}")
            print(f"    local:  {summarise_rule(local_by_id[rid])}")
        print()

    print(f"Summary: {len(added)} added, {len(removed)} removed, {len(changed)} changed")


if __name__ == "__main__":
    main()
