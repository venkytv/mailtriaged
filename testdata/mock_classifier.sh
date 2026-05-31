#!/bin/sh
# Mock classifier that reads stdin and returns a classification response.
# Echoes an "ignore" decision with a suggested rule.
cat <<'JSON'
{
  "schema_version": 1,
  "action": "ignore",
  "reason": "Recurring newsletter; not time-critical.",
  "summary": null,
  "suggested_rule": {
    "id_hint": "newsletter_example_org_ignore",
    "description": "Ignore weekly digest from example.org",
    "action": "ignore",
    "safety": "narrow",
    "match": {
      "from_email": "news@example.org",
      "subject_contains_all": ["weekly", "digest"]
    }
  }
}
JSON
