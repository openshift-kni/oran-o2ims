#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -e

# Read the Go version from go.mod rather than hardcoding it
GO_VERSION=$(grep '^go ' go.mod | awk '{print $2}')
if [ -z "${GO_VERSION}" ]; then
    echo "Error: could not determine Go version from go.mod"
    exit 1
fi

# Use of GOTOOLCHAIN=local will cause failure if the local go version doesn't
# support the go.mod go version, rather than automatically downloading a newer
# go version
GOFLAGS='' GOTOOLCHAIN=local go mod tidy -go="${GO_VERSION}"
GOFLAGS='' GOTOOLCHAIN=local go mod vendor

if grep -q "^toolchain " go.mod; then
    # Remove the toolchain directive from go.mod, if one was added by the go mod tidy command
    echo "Removing toolchain directive..."
    go get toolchain@none 2>&1 | sed 's/^/  /'
fi
