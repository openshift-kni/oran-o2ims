#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

VERSION="1.62.2"

rootdir=$(git rev-parse --show-toplevel)
if [ -z "${rootdir}" ]; then
    echo "Failed to determine top level directory"
    exit 1
fi

bindir="${rootdir}/bin"
golangci_lint="${bindir}/golangci-lint"

function get_tool {
    mkdir -p "${bindir}"
    echo "Downloading golangci-lint tool"
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -d -b "${bindir}" "v${VERSION}"

    if [ $? -ne 0 ]; then
        echo "Install from script failed. Trying go install"
        go install "github.com/golangci/golangci-lint/cmd/golangci-lint@v${VERSION}"
        if [ $? -ne 0 ]; then
            echo "Install of golangci-lint failed"
            exit 1
        else
            bindir="$(go env GOPATH)/bin"
            golangci_lint="${bindir}/golangci-lint"
        fi
    fi
}

if [ ! -f "${golangci_lint}" ]; then
    get_tool
else
    current_ver=$("${golangci_lint}" version)
    if ! [[ "${current_ver}" =~ ${VERSION} ]]; then
        get_tool
    fi
fi

export GOCACHE=/tmp/
export GOLANGCI_LINT_CACHE=/tmp/.cache
"${golangci_lint}" run --verbose --print-resources-usage --modules-download-mode=vendor --timeout=5m0s
