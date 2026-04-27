#!/bin/bash
#
# check-dep-branches.sh — Report pseudo-version go.mod dependencies that
# are behind their target branch.
#
# Reads .dep-branches.yaml for the mapping of modules to target branches,
# extracts the pinned commit from go.mod, and compares it against the
# branch HEAD using the GitHub API.
#
# Subdependencies (alignWith): modules listed under a parent's alignWith
# are checked against the parent's go.mod on its target branch. This
# ensures subdependencies stay aligned with the parent module.
#
# Requirements: yq, gh (GitHub CLI), authenticated to GitHub
#
# Usage:
#   hack/check-dep-branches.sh              # Report only
#   hack/check-dep-branches.sh --update     # Update stale deps (interactive)
#   hack/check-dep-branches.sh --update-all # Update all stale deps (non-interactive)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_FILE="${REPO_ROOT}/.dep-branches.yaml"
GOMOD_FILE="${REPO_ROOT}/go.mod"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
DIM='\033[0;2m'
RESET='\033[0m'

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Check pseudo-version go.mod dependencies against their target branches.

Options:
  --help        Show this help message
  --update      Interactively update stale dependencies (prompts per module)
  --update-all  Update all stale dependencies without prompting

With no options, reports which dependencies are stale (exit 1 if any).

Configuration: .dep-branches.yaml in the repo root
Requirements:  yq, gh (GitHub CLI, authenticated to GitHub)
EOF
    exit 0
}

UPDATE_MODE=""
case "${1:-}" in
    --help|-h)     usage ;;
    --update)      UPDATE_MODE="interactive" ;;
    --update-all)  UPDATE_MODE="all" ;;
    "")            ;; # no args, report only
    *)             echo "Unknown option: $1" >&2; usage ;;
esac

if [[ ! -f "${CONFIG_FILE}" ]]; then
    echo "ERROR: Config file not found: ${CONFIG_FILE}" >&2
    exit 1
fi

if ! command -v yq &>/dev/null; then
    echo "ERROR: yq is required but not installed." >&2
    echo "Install: go install github.com/mikefarah/yq/v4@latest" >&2
    exit 1
fi

if ! command -v gh &>/dev/null; then
    echo "ERROR: gh (GitHub CLI) is required but not installed." >&2
    exit 1
fi

# Extract the short commit hash from a pseudo-version string
# v0.0.0-20260323052035-0ba6f160cfd2 -> 0ba6f160cfd2
extract_commit() {
    local version="$1"
    echo "${version##*-}"
}

# Extract the date from a pseudo-version string
# v0.0.0-20260323052035-0ba6f160cfd2 -> 2026-03-23
extract_date() {
    local version="$1"
    local timestamp
    timestamp=$(echo "${version}" | sed 's/v0\.0\.0-\([0-9]*\).*/\1/' | head -c 8)
    echo "${timestamp:0:4}-${timestamp:4:2}-${timestamp:6:2}"
}

# Get our Go version from go.mod as major.minor (e.g., "1.24")
OUR_GO_VERSION=$(awk '$1=="go"{print $2; exit}' "${GOMOD_FILE}" | sed -E 's/^([0-9]+\.[0-9]+).*/\1/')

# Get the Go version required by a remote module on its target branch.
# Returns the go directive value (e.g., "1.25") or empty if not found.
get_remote_go_version() {
    local repo="$1"
    local branch="$2"
    local remote_gomod
    remote_gomod=$(gh api "repos/${repo}/contents/go.mod?ref=${branch}" --jq '.content' 2>/dev/null | base64 -d)
    if [[ -z "${remote_gomod}" ]]; then
        echo ""
        return
    fi
    echo "${remote_gomod}" | awk '$1=="go"{print $2; exit}' | sed -E 's/^([0-9]+\.[0-9]+).*/\1/'
}

# Compare two version strings (major.minor). Returns 0 if v1 > v2.
version_gt() {
    local v1_major v1_minor v2_major v2_minor
    v1_major="${1%%.*}"
    v1_minor="${1#*.}"
    v2_major="${2%%.*}"
    v2_minor="${2#*.}"
    if [[ "${v1_major}" -gt "${v2_major}" ]]; then
        return 0
    elif [[ "${v1_major}" -eq "${v2_major}" && "${v1_minor}" -gt "${v2_minor}" ]]; then
        return 0
    fi
    return 1
}

# Find the last commit on a branch before the Go version was bumped.
# Returns "pseudo-version|short-sha" or empty if not applicable.
find_last_safe_commit() {
    local repo="$1"
    local branch="$2"
    local commits parent_sha parent_date pseudo_ts

    # Get commits that touched go.mod on this branch
    commits=$(gh api "repos/${repo}/commits?sha=${branch}&path=go.mod&per_page=20" --jq '.[].sha' 2>/dev/null)
    if [[ -z "${commits}" ]]; then
        return
    fi

    for sha in ${commits}; do
        # Check if this commit changed the "go" directive
        diff=$(gh api "repos/${repo}/commits/${sha}" --jq '.files[] | select(.filename == "go.mod") | .patch' 2>/dev/null)
        if echo "${diff}" | grep -q "^+go "; then
            # Found the commit that bumped go version — get its parent
            parent_sha=$(gh api "repos/${repo}/commits/${sha}" --jq '.parents[0].sha' 2>/dev/null)
            if [[ -z "${parent_sha}" ]]; then
                return
            fi
            parent_date=$(gh api "repos/${repo}/commits/${parent_sha}" --jq '.commit.committer.date' 2>/dev/null)
            if [[ -z "${parent_date}" ]]; then
                return
            fi
            # Format timestamp for pseudo-version: 20260114125353
            pseudo_ts=$(echo "${parent_date}" | sed 's/[-T:]//g' | head -c 14)
            short_sha="${parent_sha:0:12}"
            echo "v0.0.0-${pseudo_ts}-${short_sha}|${short_sha}"
            return
        fi
    done
}

# Match lines in a go.mod file where the module appears as the first token
# (avoids substring matches like github.com/foo/bar matching github.com/foo/bar-extra)
gomod_lines() {
    local module="$1"
    local gomod_file="$2"
    awk -v m="${module}" '$1 == m || ($1 == "replace" && $2 == m)' "${gomod_file}"
}

# Check if a module uses a replace directive in go.mod
uses_replace_directive() {
    local module="$1"
    gomod_lines "${module}" "${GOMOD_FILE}" | grep -q "=>"
}

# Get the pinned version for a module from a go.mod file
# Handles both direct deps and replace directives
get_pinned_version() {
    local module="$1"
    local gomod_file="${2:-${GOMOD_FILE}}"
    local version

    local lines
    lines=$(gomod_lines "${module}" "${gomod_file}")

    # Check replace directives first (they take precedence)
    version=$(echo "${lines}" | grep "=>" \
        | grep -oE 'v0\.0\.0-[0-9]+-[0-9a-f]+' | head -1)

    if [[ -z "${version}" ]]; then
        # Check direct require
        version=$(echo "${lines}" \
            | grep -oE 'v0\.0\.0-[0-9]+-[0-9a-f]+' | head -1)
    fi

    # If still not found, try non-pseudo-version (tagged releases)
    if [[ -z "${version}" ]]; then
        version=$(echo "${lines}" \
            | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+[^ ]*' | head -1)
    fi

    echo "${version}"
}

# Get the HEAD commit of a branch from GitHub
get_branch_head() {
    local repo="$1"
    local branch="$2"

    gh api "repos/${repo}/commits/${branch}" --jq '.sha' 2>/dev/null | head -c 12
}

# Check if two short commit hashes match
is_current() {
    local pinned_commit="$1"
    local head_commit="$2"

    if [[ "${pinned_commit}" == "${head_commit:0:${#pinned_commit}}" ]] || \
       [[ "${head_commit}" == "${pinned_commit:0:${#head_commit}}" ]]; then
        return 0
    fi

    return 1
}

# Fetch a remote go.mod from GitHub and extract a module's pinned version
get_remote_gomod_version() {
    local repo="$1"
    local branch="$2"
    local module="$3"
    local remote_gomod

    remote_gomod=$(gh api "repos/${repo}/contents/go.mod?ref=${branch}" --jq '.content' 2>/dev/null | base64 -d)
    if [[ -z "${remote_gomod}" ]]; then
        return 1
    fi

    # Write to a temp file so get_pinned_version can grep it
    local tmpfile
    tmpfile=$(mktemp)
    echo "${remote_gomod}" > "${tmpfile}"
    local version
    version=$(get_pinned_version "${module}" "${tmpfile}")
    rm -f "${tmpfile}"
    echo "${version}"
}

# Validate no duplicate modules in config (standalone + subdependencies)
validate_no_duplicates() {
    local all_modules=()
    local dep_count
    dep_count=$(yq '.dependencies | length' "${CONFIG_FILE}")

    for i in $(seq 0 $((dep_count - 1))); do
        local module
        module=$(yq ".dependencies[${i}].module" "${CONFIG_FILE}")
        all_modules+=("${module}")

        # Collect subdependencies
        local sub_count
        sub_count=$(yq ".dependencies[${i}].alignWith | length // 0" "${CONFIG_FILE}")
        for j in $(seq 0 $((sub_count - 1))); do
            local sub_module
            sub_module=$(yq ".dependencies[${i}].alignWith[${j}]" "${CONFIG_FILE}")
            all_modules+=("${sub_module}")
        done
    done

    # Check for duplicates
    local sorted
    sorted=$(printf '%s\n' "${all_modules[@]}" | sort)
    local dupes
    dupes=$(echo "${sorted}" | uniq -d)
    if [[ -n "${dupes}" ]]; then
        echo "ERROR: Duplicate modules found in ${CONFIG_FILE}:" >&2
        while IFS= read -r dup; do
            echo "  ${dup}" >&2
        done <<< "${dupes}"
        echo "Each module must appear only once (either standalone or as a subdependency)." >&2
        exit 1
    fi
}

# --- Main ---

validate_no_duplicates

echo "Checking pseudo-version dependencies against target branches..."
echo "Config: ${CONFIG_FILE}"
echo

stale_count=0
current_count=0
error_count=0
stale_modules=()

dep_count=$(yq '.dependencies | length' "${CONFIG_FILE}")

for i in $(seq 0 $((dep_count - 1))); do
    module=$(yq ".dependencies[${i}].module" "${CONFIG_FILE}")
    branch=$(yq ".dependencies[${i}].branch" "${CONFIG_FILE}")
    repo=$(yq ".dependencies[${i}].repo" "${CONFIG_FILE}")

    # Get pinned version from go.mod
    pinned_version=$(get_pinned_version "${module}")
    if [[ -z "${pinned_version}" ]]; then
        printf "  ${YELLOW}SKIP${RESET}  %-60s (not found in go.mod)\n" "${module}"
        continue
    fi

    pinned_commit=$(extract_commit "${pinned_version}")
    pinned_date=$(extract_date "${pinned_version}")

    # Get branch HEAD from GitHub
    head_commit=$(get_branch_head "${repo}" "${branch}")
    # align_ref is the ref used for subdependency checks (may be overridden to safe commit)
    align_ref="${branch}"
    if [[ -z "${head_commit}" ]]; then
        printf "  ${RED}ERROR${RESET} %-60s (failed to get HEAD of %s/%s)\n" "${module}" "${repo}" "${branch}"
        error_count=$((error_count + 1))
        continue
    fi

    if is_current "${pinned_commit}" "${head_commit}"; then
        printf "  ${GREEN}OK${RESET}    %-60s %s @ %s\n" "${module}" "${branch}" "${pinned_date}"
        current_count=$((current_count + 1))
    else
        flags=""
        if uses_replace_directive "${module}"; then
            flags="${flags} [replace]"
        fi
        # Check if the target branch requires a newer Go version
        update_target="${branch}"
        remote_go=$(get_remote_go_version "${repo}" "${branch}")
        if [[ -n "${remote_go}" ]] && version_gt "${remote_go}" "${OUR_GO_VERSION}"; then
            flags="${flags} [needs go ${remote_go}]"
            # Find the last commit before the Go version bump
            safe_info=$(find_last_safe_commit "${repo}" "${branch}")
            if [[ -n "${safe_info}" ]]; then
                safe_version="${safe_info%%|*}"
                safe_sha="${safe_info##*|}"
                if ! is_current "${pinned_commit}" "${safe_sha}"; then
                    flags="${flags} [safe: ${safe_version}]"
                    update_target="${safe_version}"
                    align_ref="${safe_sha}"
                else
                    # Already at the safe version — nothing more we can do
                    printf "  ${YELLOW}LIMIT${RESET} %-60s %s @ %s (at go %s safe limit, HEAD needs go %s)\n" \
                        "${module}" "${branch}" "${pinned_date}" "${OUR_GO_VERSION}" "${remote_go}"
                    current_count=$((current_count + 1))
                    continue
                fi
            fi
        fi
        printf "  ${RED}STALE${RESET} %-60s %s @ %s (pinned: %s, HEAD: %s)%s\n" \
            "${module}" "${branch}" "${pinned_date}" "${pinned_commit}" "${head_commit}" "${flags}"
        stale_count=$((stale_count + 1))
        stale_modules+=("${module}|${update_target}|${flags}")
    fi

    # Check subdependencies (alignWith)
    sub_count=$(yq ".dependencies[${i}].alignWith | length // 0" "${CONFIG_FILE}")
    for j in $(seq 0 $((sub_count - 1))); do
        sub_module=$(yq ".dependencies[${i}].alignWith[${j}]" "${CONFIG_FILE}")

        # Get our pinned version
        our_version=$(get_pinned_version "${sub_module}")
        if [[ -z "${our_version}" ]]; then
            printf "    ${YELLOW}SKIP${RESET}  ${DIM}└─${RESET} %-56s (not found in our go.mod)\n" "${sub_module}"
            continue
        fi

        # Get the parent's pinned version from its remote go.mod
        parent_version=$(get_remote_gomod_version "${repo}" "${align_ref}" "${sub_module}")
        if [[ -z "${parent_version}" ]]; then
            printf "    ${YELLOW}SKIP${RESET}  ${DIM}└─${RESET} %-56s (not found in %s go.mod)\n" "${sub_module}" "${module}"
            continue
        fi

        if [[ "${our_version}" == "${parent_version}" ]]; then
            printf "    ${GREEN}OK${RESET}    ${DIM}└─${RESET} %-56s aligned with %s\n" "${sub_module}" "${module}"
            current_count=$((current_count + 1))
        else
            our_commit=$(extract_commit "${our_version}")
            parent_commit=$(extract_commit "${parent_version}")
            printf "    ${RED}DRIFT${RESET} ${DIM}└─${RESET} %-56s ours: %s, %s has: %s\n" \
                "${sub_module}" "${our_commit}" "${module}" "${parent_commit}"
            stale_count=$((stale_count + 1))
            # For subdependencies, store the parent version and mark as subdep
            stale_modules+=("${sub_module}|${parent_version}| [subdep]")
        fi
    done
done

echo
echo "Summary: ${current_count} current, ${stale_count} stale, ${error_count} errors"

# Handle updates if requested
if [[ "${UPDATE_MODE}" != "" ]] && [[ ${stale_count} -gt 0 ]]; then
    echo
    for entry in "${stale_modules[@]}"; do
        # Parse module|target|replace_flag
        module="${entry%%|*}"
        rest="${entry#*|}"
        target="${rest%%|*}"
        replace_flag="${rest#*|}"

        # Skip modules that need manual intervention
        if [[ "${replace_flag}" == *"replace"* ]] && [[ "${replace_flag}" != *"subdep"* ]]; then
            printf "  ${YELLOW}SKIP${RESET}  %-60s (uses replace directive, update manually)\n" "${module}"
            continue
        fi
        if [[ "${UPDATE_MODE}" == "interactive" ]]; then
            printf "Update ${CYAN}%s${RESET} to ${CYAN}%s${RESET}? [y/N] " "${module}" "${target}"
            read -r answer
            if [[ "${answer}" != "y" && "${answer}" != "Y" ]]; then
                echo "  Skipped."
                continue
            fi
        fi

        if [[ "${replace_flag}" == *"subdep"* ]]; then
            # Subdependency: update the replace directive in go.mod
            # target is the full pseudo-version (e.g., v0.0.0-20251026193953-3266b6d73526)
            echo "  Updating replace directive for ${module} to ${target}..."
            # Use go mod edit to update the replace directive
            (cd "${REPO_ROOT}" && go mod edit -replace "${module}=${module}@${target}" 2>&1 | sed 's/^/    /')
        else
            # Standalone: use go get to update
            echo "  Updating ${module} to ${target}..."
            (cd "${REPO_ROOT}" && GOFLAGS='' go get "${module}@${target}" 2>&1 | sed 's/^/    /')
        fi
    done

    echo
    echo "Running go mod tidy..."
    (cd "${REPO_ROOT}" && GOFLAGS='' GOTOOLCHAIN=local go mod tidy 2>&1 | sed 's/^/  /')
    # Safety net: remove toolchain directive if one was added despite GOTOOLCHAIN=local
    if grep -q "^toolchain " "${GOMOD_FILE}"; then
        echo "Removing toolchain directive..."
        (cd "${REPO_ROOT}" && go get toolchain@none 2>&1 | sed 's/^/  /')
    fi
    echo "Running go mod vendor..."
    (cd "${REPO_ROOT}" && GOFLAGS='' GOTOOLCHAIN=local go mod vendor 2>&1 | sed 's/^/  /')
    echo
    echo "Done. Review changes with: git diff go.mod go.sum"
fi

if [[ ${stale_count} -gt 0 ]]; then
    exit 1
fi
exit 0
