#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -e

PINNED_GO="1.24.0"

# Ensure submodules exist and are updated
if [ "${SKIP_SUBMODULE_SYNC}" != "yes" ]; then
    echo "Syncing submodules"
    git submodule update --init --recursive
else
    echo "Skipping submodule sync"
fi

# Handle the exported api/hardwaremanagement submodule first
pushd api/hardwaremanagement >/dev/null
go mod tidy -go="${PINNED_GO}"
popd >/dev/null

pushd api/provisioning >/dev/null
go mod tidy -go="${PINNED_GO}"
popd >/dev/null

pushd api/inventory >/dev/null
go mod tidy -go="${PINNED_GO}"
popd >/dev/null

go mod vendor
go mod tidy -go="${PINNED_GO}"

