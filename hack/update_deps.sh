#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -e

PINNED_GO="1.24.0"

# Use of GOTOOLCHAIN=local will cause failure if the local go version doesn't
# support the pinned go version, rather than automatically downloading a newer
# go version
GOFLAGS='' GOTOOLCHAIN=local go mod tidy -go="${PINNED_GO}"
GOFLAGS='' GOTOOLCHAIN=local go mod vendor

if grep -q "^toolchain " go.mod; then
    # Remove the toolchain directive from go.mod, if one was added by the go mod tidy command
    echo "Removing toolchain directive..."
    go get toolchain@none 2>&1 | sed 's/^/  /'
fi
