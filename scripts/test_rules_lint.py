#!/usr/bin/env python3

import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("rules-lint.py")
SPEC = importlib.util.spec_from_file_location("rules_lint", SCRIPT)
rules_lint = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(rules_lint)


def rule(rule_id, match, action="ignore"):
    return {
        "id": rule_id,
        "enabled": True,
        "match": match,
        "action": action,
        "source": "reviewed",
    }


class RedundantRulesTest(unittest.TestCase):
    def test_specific_subject_rule_after_broader_subject_rule_is_shadowed(self):
        warnings = rules_lint.check_redundant_rules(
            [
                rule(
                    "synology_backup_notices",
                    {
                        "from_email": "venkytv@gmail.com",
                        "subject_contains_all": ["Synology C2"],
                    },
                    "classify",
                ),
                rule(
                    "synology_backup_success",
                    {
                        "from_email": "venkytv@gmail.com",
                        "subject_contains_all": ["Synology C2", "successful"],
                    },
                ),
            ]
        )

        self.assertEqual(len(warnings), 1)
        self.assertIn("synology_backup_success", warnings[0])
        self.assertIn("synology_backup_notices", warnings[0])

    def test_specific_subject_rule_before_broader_subject_rule_is_ok(self):
        warnings = rules_lint.check_redundant_rules(
            [
                rule(
                    "synology_backup_success",
                    {
                        "from_email": "venkytv@gmail.com",
                        "subject_contains_all": ["Synology C2", "successful"],
                    },
                ),
                rule(
                    "synology_backup_notices",
                    {
                        "from_email": "venkytv@gmail.com",
                        "subject_contains_all": ["Synology C2"],
                    },
                    "classify",
                ),
            ]
        )

        self.assertEqual(warnings, [])

    def test_domain_rule_shadows_later_email_rule_for_same_domain(self):
        warnings = rules_lint.check_redundant_rules(
            [
                rule("marketing_example", {"from_domain": "example.com"}),
                rule("person_example", {"from_email": "person@example.com"}),
            ]
        )

        self.assertEqual(len(warnings), 1)

    def test_ambiguous_any_subject_overlap_is_not_reported(self):
        warnings = rules_lint.check_redundant_rules(
            [
                rule(
                    "parcel_updates",
                    {
                        "from_email": "sender@example.com",
                        "subject_contains_any": ["parcel", "delivery"],
                    },
                ),
                rule(
                    "shipped_updates",
                    {
                        "from_email": "sender@example.com",
                        "subject_contains_any": ["parcel", "shipped"],
                    },
                ),
            ]
        )

        self.assertEqual(warnings, [])


if __name__ == "__main__":
    unittest.main()
