#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#
# Start Swagger UI to browse all OpenAPI documentation interactively.
# Usage: ./hack/swagger-ui.sh [start|stop]
#

set -e

# Require Bash 4+ for associative arrays (declare -A)
if [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
    echo "Error: ${0} requires Bash 4.0 or later (found ${BASH_VERSION})."
    echo "  macOS ships Bash 3.2; install a newer version with:"
    echo "    brew install bash"
    echo "  Then run: \$(brew --prefix)/bin/bash ${0}"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Configuration
ENGINE="${ENGINE:-docker}"
SWAGGER_UI_PORT="${SWAGGER_UI_PORT:-9090}"
SWAGGER_UI_CONTAINER_NAME="oran-o2ims-swagger-ui"
OPENAPI_BUNDLED_DIR=""
STATE_DIR="${XDG_RUNTIME_DIR:-${HOME}/.cache}/oran-o2ims"
STATE_FILE="${STATE_DIR}/swagger-ui.path"
TMPDIR_PREFIX="${TMPDIR:-/tmp}/oran-o2ims-swagger-ui."
# Pinned @redocly/cli version for reproducible bundling; bump after testing a new release.
REDOCLY_CLI_VERSION="2.25.2"

# OpenAPI specs to serve (excluding common which contains shared types)
OPENAPI_SPECS=(resources alarms provisioning artifacts cluster hwplugin-inventory hwplugin-provisioning hwplugin-nar-callback)

# API display names
declare -A API_NAMES=(
    [resources]="Infrastructure Inventory"
    [alarms]="Infrastructure Monitoring (Alarms)"
    [provisioning]="Infrastructure Provisioning"
    [artifacts]="Infrastructure Artifacts"
    [cluster]="Infrastructure Cluster"
    [hwplugin-inventory]="Hardware Plugin Inventory"
    [hwplugin-provisioning]="Hardware Plugin Provisioning"
    [hwplugin-nar-callback]="Hardware Plugin NAR Callback"
)

# Source file paths for each spec
declare -A API_PATHS=(
    [resources]="${PROJECT_ROOT}/internal/service/resources/api/openapi.yaml"
    [alarms]="${PROJECT_ROOT}/internal/service/alarms/api/openapi.yaml"
    [provisioning]="${PROJECT_ROOT}/internal/service/provisioning/api/openapi.yaml"
    [artifacts]="${PROJECT_ROOT}/internal/service/artifacts/api/openapi.yaml"
    [cluster]="${PROJECT_ROOT}/internal/service/cluster/api/openapi.yaml"
    [hwplugin-inventory]="${PROJECT_ROOT}/hwmgr-plugins/api/openapi/specs/inventory.yaml"
    [hwplugin-provisioning]="${PROJECT_ROOT}/hwmgr-plugins/api/openapi/specs/provisioning.yaml"
    [hwplugin-nar-callback]="${PROJECT_ROOT}/hwmgr-plugins/api/openapi/specs/nar_callback.yaml"
)

# Cleanup function for error cases
cleanup_on_error() {
    local exit_code=$?
    if [ $exit_code -ne 0 ] && [ -n "${OPENAPI_BUNDLED_DIR}" ] && [ -d "${OPENAPI_BUNDLED_DIR}" ]; then
        echo "Error occurred, cleaning up temporary directory..."
        rm -rf -- "${OPENAPI_BUNDLED_DIR}"
    fi
    exit $exit_code
}

stop_swagger_ui() {
    if ${ENGINE} stop "${SWAGGER_UI_CONTAINER_NAME}" 2>/dev/null; then
        echo "Swagger UI stopped."
    else
        echo "Swagger UI is not running."
    fi

    # Clean up the temporary directory if the state file exists
    if [ -f "${STATE_FILE}" ]; then
        local tmp_dir
        tmp_dir="$(cat "${STATE_FILE}")"

        # Validate before deleting: must be owned by us, match the expected
        # mktemp prefix, and actually be a directory.
        if [ -d "${tmp_dir}" ] && [ -O "${tmp_dir}" ] && [[ "${tmp_dir}" == ${TMPDIR_PREFIX}* ]]; then
            rm -rf -- "${tmp_dir}"
            echo "Cleaned up temporary directory."
        elif [ -n "${tmp_dir}" ]; then
            echo "Warning: skipping removal of '${tmp_dir}' (ownership or prefix mismatch)."
        fi
        rm -f -- "${STATE_FILE}"
    fi
}

start_swagger_ui() {
    # Check for npx
    if ! command -v npx &> /dev/null; then
        echo "Error: npx is not installed. Please install Node.js:"
        echo "  Fedora: sudo dnf install nodejs npm"
        echo "  macOS:  brew install node"
        echo "  Other:  https://nodejs.org/en/download"
        exit 1
    fi

    # Check for container runtime (binary exists and daemon is reachable)
    if ! command -v "${ENGINE}" &> /dev/null; then
        echo "Error: ${ENGINE} is not installed."
        echo "  Set ENGINE=podman or ENGINE=docker, or install one of them."
        exit 1
    fi
    if ! ${ENGINE} info &> /dev/null; then
        echo "Error: ${ENGINE} is installed but not running or not accessible."
        echo "  Start the daemon, or set ENGINE to a working runtime (e.g. ENGINE=podman)."
        exit 1
    fi

    # Clean up stale temp dirs from previous runs that weren't stopped
    for stale in "${TMPDIR_PREFIX}"*; do
        [ -d "${stale}" ] && [ -O "${stale}" ] && rm -rf -- "${stale}"
    done

    # Create temporary directory with identifiable prefix and set up cleanup trap
    OPENAPI_BUNDLED_DIR="$(mktemp -d "${TMPDIR_PREFIX}XXXXXX")"
    chmod 755 "${OPENAPI_BUNDLED_DIR}"
    trap cleanup_on_error EXIT INT TERM

    echo "Bundling OpenAPI specs to ${OPENAPI_BUNDLED_DIR}..."
    for spec in "${OPENAPI_SPECS[@]}"; do
        echo "  Bundling ${spec} API..."
        if ! npx --yes "@redocly/cli@${REDOCLY_CLI_VERSION}" bundle \
            "${API_PATHS[$spec]}" \
            -o "${OPENAPI_BUNDLED_DIR}/${spec}.yaml"; then
            echo "Error: Failed to bundle ${spec} API"
            exit 1
        fi
    done

    # Verify all files were created
    echo "Verifying bundled files..."
    for spec in "${OPENAPI_SPECS[@]}"; do
        if [ ! -f "${OPENAPI_BUNDLED_DIR}/${spec}.yaml" ]; then
            echo "Error: ${spec}.yaml was not created"
            exit 1
        fi
        echo "  Ok: ${spec}.yaml ($(wc -c < "${OPENAPI_BUNDLED_DIR}/${spec}.yaml") bytes)"
    done

    # Build the URLS JSON array
    URLS='['
    first=true
    for spec in "${OPENAPI_SPECS[@]}"; do
        if [ "$first" = true ]; then
            first=false
        else
            URLS+=','
        fi
        URLS+="{\"url\":\"/api/${spec}.yaml\",\"name\":\"${API_NAMES[$spec]}\"}"
    done
    URLS+=']'

    echo "Starting Swagger UI (using ${ENGINE})..."
    ${ENGINE} stop "${SWAGGER_UI_CONTAINER_NAME}" 2>/dev/null || true

    ${ENGINE} run -d --rm \
        --name "${SWAGGER_UI_CONTAINER_NAME}" \
        -p "${SWAGGER_UI_PORT}:8080" \
        -e URLS="${URLS}" \
        -e "URLS_PRIMARY_NAME=Infrastructure Inventory" \
        -v "${OPENAPI_BUNDLED_DIR}:/usr/share/nginx/html/api:ro,Z" \
        docker.io/swaggerapi/swagger-ui

    # Save the temp directory path for cleanup on stop
    mkdir -p "${STATE_DIR}" && chmod 700 "${STATE_DIR}"
    echo "${OPENAPI_BUNDLED_DIR}" > "${STATE_FILE}"
    chmod 600 "${STATE_FILE}"

    # Disable the error trap since we succeeded
    trap - EXIT

    echo ""
    echo "=============================================="
    echo "  Swagger UI is running!"
    echo "  Open your browser at: http://127.0.0.1:${SWAGGER_UI_PORT}"
    echo ""
    echo "  Available APIs (use dropdown in top-right):"
    for spec in "${OPENAPI_SPECS[@]}"; do
        echo "    ${API_NAMES[$spec]}"
    done
    echo ""
    echo "  To stop:  make swagger-ui-stop"
    echo "  To start: make swagger-ui-start"
    echo "=============================================="
}

# Main
case "${1:-start}" in
    start)
        start_swagger_ui
        ;;
    stop)
        stop_swagger_ui
        ;;
    *)
        echo "Usage: $0 [start|stop]"
        exit 1
        ;;
esac
