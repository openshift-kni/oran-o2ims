#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#
# This script uses go workspaces to sync the dependencies of the exported API submodules against the main go.mod
#

PINNED_GO="1.24.0"

function cleanup {
    if [ -n "${rootdir}" ]; then
        rm -f "${rootdir}/go.work" "${rootdir}/go.work.sum"
    fi
}

trap cleanup EXIT

rootdir=$(git rev-parse --show-toplevel)
if [ -z "${rootdir}" ]; then
    echo "Failed to determine top level directory" >&2
    exit 1
fi

if ! cd "${rootdir}"; then
    echo "Failed to cd to top level directory: ${rootdir}" >&2
    exit 1
fi

if [ -f go.work ]; then
    echo "A go.work file already exists. Aborting sync" >&2
    exit 1
fi

echo "Creating workspace"
if ! go work init .; then
    echo "Command failed: go work init" >&2
    exit 1
fi

for gomod in ./api/*/go.mod; do
    submodule=$(dirname "$gomod")
    echo "Adding ${submodule}"
    if ! go work use "${submodule}"; then
        echo "Command failed: go work use ${submodule}" >&2
        exit 1
    fi
done

echo "Syncing API module dependencies"
if ! go work sync; then
    echo "Command failed: go work sync" >&2
    exit 1
fi

for gomod in ./api/*/go.mod; do
    submodule=$(dirname "$gomod")
    echo "Tidying ${submodule}"
    if ! pushd ${submodule} >/dev/null; then
        echo "Command failed: pushd ${submodule}" >&2
        exit 1
    fi

    if ! go mod tidy -go="${PINNED_GO}"; then
        echo "Command failed: go mod tidy -go=${PINNED_GO}" >&2
        exit 1
    fi

    if ! popd >/dev/null; then
        echo "Command failed: popd" >&2
        exit 1
    fi
done

rm -f "${rootdir}/go.work" "${rootdir}/go.work.sum"

echo "Tidying main"
if ! go mod tidy -go="${PINNED_GO}"; then
    echo "Command failed: go mod tidy -go=${PINNED_GO}" >&2
    exit 1
fi

if ! go mod vendor; then
    echo "Command failed: go mod vendor" >&2
    exit 1
fi

echo "Done"
exit 0

