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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Configuration
ENGINE="${ENGINE:-docker}"
SWAGGER_UI_PORT="${SWAGGER_UI_PORT:-9090}"
SWAGGER_UI_CONTAINER_NAME="oran-o2ims-swagger-ui"
OPENAPI_BUNDLED_DIR=""

# OpenAPI specs to serve (excluding common which contains shared types)
OPENAPI_SPECS=(resources alarms provisioning artifacts cluster)

# API display names
declare -A API_NAMES=(
    [resources]="Infrastructure Inventory"
    [alarms]="Infrastructure Monitoring (Alarms)"
    [provisioning]="Infrastructure Provisioning"
    [artifacts]="Infrastructure Artifacts"
    [cluster]="Infrastructure Cluster"
)

# Cleanup function for error cases
cleanup_on_error() {
    local exit_code=$?
    if [ $exit_code -ne 0 ] && [ -n "${OPENAPI_BUNDLED_DIR}" ] && [ -d "${OPENAPI_BUNDLED_DIR}" ]; then
        echo "Error occurred, cleaning up temporary directory..."
        rm -rf "${OPENAPI_BUNDLED_DIR}"
    fi
    exit $exit_code
}

stop_swagger_ui() {
    if ${ENGINE} stop "${SWAGGER_UI_CONTAINER_NAME}" 2>/dev/null; then
        echo "Swagger UI stopped."
    else
        echo "Swagger UI is not running."
    fi

    # Clean up the temporary directory if it exists
    local path_file="/tmp/oran-o2ims-swagger-ui.path"
    if [ -f "${path_file}" ]; then
        local tmp_dir
        tmp_dir="$(cat "${path_file}")"
        if [ -d "${tmp_dir}" ]; then
            rm -rf "${tmp_dir}"
            echo "Cleaned up temporary directory."
        fi
        rm -f "${path_file}"
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

    # Create temporary directory and set up cleanup trap
    OPENAPI_BUNDLED_DIR="$(mktemp -d)"
    chmod 755 "${OPENAPI_BUNDLED_DIR}"
    trap cleanup_on_error EXIT

    echo "Bundling OpenAPI specs to ${OPENAPI_BUNDLED_DIR}..."
    for spec in "${OPENAPI_SPECS[@]}"; do
        echo "  Bundling ${spec} API..."
        if ! npx --yes @redocly/cli bundle \
            "${PROJECT_ROOT}/internal/service/${spec}/api/openapi.yaml" \
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
        -v "${OPENAPI_BUNDLED_DIR}:/usr/share/nginx/html/api:ro" \
        swaggerapi/swagger-ui

    # Save the temp directory path for cleanup on stop
    echo "${OPENAPI_BUNDLED_DIR}" > /tmp/oran-o2ims-swagger-ui.path

    # Disable the error trap since we succeeded
    trap - EXIT

    echo ""
    echo "=============================================="
    echo "  Swagger UI is running!"
    echo "  Open your browser at: http://localhost:${SWAGGER_UI_PORT}"
    echo ""
    echo "  Available APIs (use dropdown in top-right):"
    for spec in "${OPENAPI_SPECS[@]}"; do
        echo "    - ${API_NAMES[$spec]}"
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
