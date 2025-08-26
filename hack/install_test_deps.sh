#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -ex

go install github.com/onsi/ginkgo/v2/ginkgo@$(go list -f '{{.Version}}' -m github.com/onsi/ginkgo/v2)

if ! [ -x "$(command -v spectral)" ]; then
    echo "Downloading spectral"

    curl -Lo spectral https://github.com/stoplightio/spectral/releases/download/v6.11.0/spectral-linux-x64
    echo 0e151d3dc5729750805428f79a152fa01dd4c203f1d9685ef19f4fd4696fcd5f spectral | sha256sum -c
    chmod +x spectral
    mv spectral $(go env GOPATH)/bin
fi
