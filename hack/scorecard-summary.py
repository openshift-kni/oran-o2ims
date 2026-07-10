#!/usr/bin/env python3
"""Parse scorecard JSON output and print a concise summary.

Exits with code 1 if any test did not pass or no results are found.
In --strict mode, suggestions are also treated as failures.
Filters the verbose log output from the olm-crds-have-validation test,
which dumps the entire CRD schema as raw Go structs.

On failure, the full unfiltered results are printed first, followed by
the summary, so that CI logs contain the details needed for debugging.
"""

import json
import sys


def format_summary(results, strict):
    """Format the concise summary of test results."""
    lines = []
    lines.append("Scorecard Test Summary")
    lines.append("(run with SCORECARD_VERBOSE=true for full unfiltered output)")
    lines.append("")

    suggestion_count = 0

    for name, state, suggestions, errors, log in results:
        lines.append(f"--- {name} --- [{state}]")

        for error in errors:
            lines.append(f"  Error: {error}")

        for suggestion in suggestions:
            suggestion_count += 1
            prefix = "FAIL" if strict else "WARNING"
            lines.append(f"  {prefix} - Suggestion: {suggestion}")

        if log and name != "olm-crds-have-validation":
            truncated = log[:500]
            if len(log) > 500:
                truncated += "..."
            lines.append(f"  Log: {truncated}")

        lines.append("")

    if suggestion_count > 0:
        if strict:
            lines.append(f"** {suggestion_count} suggestion(s) found — "
                         f"treated as failures (SCORECARD_STRICT=true) **")
        else:
            lines.append(f"** {suggestion_count} suggestion(s) found — "
                         f"run with SCORECARD_STRICT=true to treat as failures **")
        lines.append("")

    return "\n".join(lines)


def format_full(results):
    """Format the full unfiltered results for debugging."""
    lines = []
    lines.append("Full Scorecard Test Results")
    lines.append("=" * 60)
    lines.append("")

    for name, state, suggestions, errors, log in results:
        lines.append(f"--- {name} --- [{state}]")

        for error in errors:
            lines.append(f"  Error: {error}")

        for suggestion in suggestions:
            lines.append(f"  Suggestion: {suggestion}")

        if log:
            lines.append(f"  Log: {log}")

        lines.append("")

    return "\n".join(lines)


def main():
    strict = "--strict" in sys.argv

    data = json.load(sys.stdin)
    results = []

    for item in data.get("items", []):
        for result in item.get("status", {}).get("results", []):
            results.append((
                result.get("name", ""),
                result.get("state", ""),
                result.get("suggestions", []),
                result.get("errors", []),
                result.get("log", "").strip(),
            ))

    if not results:
        print("Error: no scorecard test results found", file=sys.stderr)
        sys.exit(1)

    failed = any(state != "pass" for _, state, _, _, _ in results)
    has_suggestions = any(len(s) > 0 for _, _, s, _, _ in results)

    if strict and has_suggestions:
        failed = True

    if failed:
        print(format_full(results))
        print()

    print(format_summary(results, strict))

    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
