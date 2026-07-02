#!/bin/bash
# check-coverage.sh - Verify code coverage meets per-package thresholds
#
# Usage: ./hack/check-coverage.sh <coverage-profile>
#
# Reads thresholds from .coverage-thresholds.yaml and checks the coverage
# profile against them. Exits with 1 if any threshold is violated or if
# new packages are found that are not listed in the thresholds file.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
THRESHOLDS_FILE="${REPO_ROOT}/.coverage-thresholds.yaml"
COVERAGE_FILE="${1:?Usage: $0 <coverage-profile>}"

if [[ ! -f "${COVERAGE_FILE}" ]]; then
    echo "ERROR: Coverage profile not found: ${COVERAGE_FILE}"
    exit 1
fi

if [[ ! -f "${THRESHOLDS_FILE}" ]]; then
    echo "ERROR: Thresholds file not found: ${THRESHOLDS_FILE}"
    exit 1
fi

# Module path prefix to strip from coverage output
MODULE="github.com/openshift-kni/oran-o2ims"

# Parse the overall threshold
OVERALL_THRESHOLD=$(grep '^overall:' "${THRESHOLDS_FILE}" | awk '{print $2}')

# Get overall coverage from the profile
OVERALL_COVERAGE=$(go tool cover -func="${COVERAGE_FILE}" | grep '^total:' | awk '{gsub(/%/,""); print $NF}')

echo "=== Code Coverage Report ==="
echo ""

# Calculate per-package statement-weighted coverage from the raw profile.
# Each line in the profile (after the mode: header) has the format:
#   file.go:startline.col,endline.col numStatements count
# When merging unit and envtest profiles, the same block may appear multiple
# times. We deduplicate by taking the max count per block, so a block covered
# by either test suite counts as covered.
declare -A block_stmts
declare -A block_count
while IFS= read -r line; do
    # Skip the mode: header
    [[ "${line}" == mode:* ]] && continue

    # Parse: location numStatements count
    location="${line%% *}"
    rest="${line#* }"
    num_stmts="${rest%% *}"
    count="${rest##* }"

    # Deduplicate: keep the max count for each block
    block_stmts["${location}"]="${num_stmts}"
    if [[ -z "${block_count["${location}"]+x}" ]] || [[ "${count}" -gt "${block_count["${location}"]}" ]]; then
        block_count["${location}"]="${count}"
    fi
done < "${COVERAGE_FILE}"

# Aggregate per-package from deduplicated blocks
declare -A pkg_total_stmts
declare -A pkg_covered_stmts
for location in "${!block_stmts[@]}"; do
    num_stmts="${block_stmts[${location}]}"
    count="${block_count[${location}]}"

    # Extract file path from location (before the colon with line info)
    file="${location%%:*}"

    # Skip files outside the module
    if [[ "${file}" != "${MODULE}"/* ]]; then
        continue
    fi

    # Extract package path (strip module prefix and filename)
    rel="${file#"${MODULE}"/}"
    if [[ "${rel}" == */* ]]; then
        pkg="${rel%/*}"
    else
        # Root-level file (e.g. main.go) — skip
        continue
    fi

    if [[ -n "${pkg}" ]]; then
        pkg_total_stmts[${pkg}]=$(( ${pkg_total_stmts[${pkg}]:-0} + num_stmts ))
        if [[ "${count}" -gt 0 ]]; then
            pkg_covered_stmts[${pkg}]=$(( ${pkg_covered_stmts[${pkg}]:-0} + num_stmts ))
        fi
    fi
done

# Calculate per-package coverage percentages
declare -A pkg_avg
for pkg in "${!pkg_total_stmts[@]}"; do
    total="${pkg_total_stmts[${pkg}]}"
    covered="${pkg_covered_stmts[${pkg}]:-0}"
    if [[ "${total}" -gt 0 ]]; then
        pkg_avg[${pkg}]=$(awk "BEGIN {printf \"%.1f\", ${covered} * 100 / ${total}}")
    else
        pkg_avg[${pkg}]="0"
    fi
done

# Collect known packages from thresholds file
declare -A known_packages
in_pkgs=false
while IFS= read -r line; do
    [[ "${line}" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line}" ]] && continue
    if [[ "${line}" == "packages:" ]]; then
        in_pkgs=true
        continue
    fi
    if ${in_pkgs}; then
        kpkg=$(echo "${line}" | sed 's/^[[:space:]]*//' | cut -d: -f1)
        known_packages[${kpkg}]=1
    fi
done < "${THRESHOLDS_FILE}"

# Check thresholds
FAILURES=0

# Print header
printf "%-65s %8s %8s %s\n" "Package" "Coverage" "Min" "Status"
printf "%-65s %8s %8s %s\n" "-------" "--------" "---" "------"

# Read per-package thresholds and check
in_packages=false
while IFS= read -r line; do
    # Skip comments and empty lines
    [[ "${line}" =~ ^[[:space:]]*# ]] && continue
    [[ -z "${line}" ]] && continue
    [[ "${line}" =~ ^overall: ]] && continue

    if [[ "${line}" == "packages:" ]]; then
        in_packages=true
        continue
    fi

    if ${in_packages}; then
        # Parse "  package/path: threshold"
        pkg=$(echo "${line}" | sed 's/^[[:space:]]*//' | cut -d: -f1)
        threshold=$(echo "${line}" | cut -d: -f2 | tr -d ' ')

        actual="${pkg_avg[${pkg}]:-N/A}"

        if [[ "${actual}" == "N/A" ]]; then
            printf "%-65s %7s%% %7s%% %s\n" "${pkg}" "${actual}" "${threshold}" "✗ NOT FOUND"
            FAILURES=$((FAILURES + 1))
            continue
        fi

        # Compare with 0.5% tolerance to account for non-deterministic coverage variance
        if (( $(awk "BEGIN {print (${actual} < ${threshold} - 0.5)}") )); then
            printf "%-65s %7s%% %7s%% %s\n" "${pkg}" "${actual}" "${threshold}" "✗ FAIL"
            FAILURES=$((FAILURES + 1))
        else
            printf "%-65s %7s%% %7s%% %s\n" "${pkg}" "${actual}" "${threshold}" "✓ OK"
        fi
    fi
done < "${THRESHOLDS_FILE}"

# Check for packages in coverage profile that are not in thresholds file
for pkg in "${!pkg_avg[@]}"; do
    if [[ -z "${known_packages[${pkg}]+x}" ]]; then
        printf "%-65s %7s%%          %s\n" "${pkg}" "${pkg_avg[${pkg}]}" "✗ NOT IN THRESHOLDS"
        FAILURES=$((FAILURES + 1))
    fi
done

echo ""
printf "%-65s %7s%% %7s%% " "OVERALL" "${OVERALL_COVERAGE}" "${OVERALL_THRESHOLD}"
if (( $(awk "BEGIN {print (${OVERALL_COVERAGE} < ${OVERALL_THRESHOLD} - 0.5)}") )); then
    echo "✗ FAIL"
    FAILURES=$((FAILURES + 1))
else
    echo "✓ OK"
fi

echo ""
if [[ ${FAILURES} -gt 0 ]]; then
    echo "FAILED: ${FAILURES} coverage threshold(s) violated"
    exit 1
else
    echo "PASSED: All coverage thresholds met"
fi
