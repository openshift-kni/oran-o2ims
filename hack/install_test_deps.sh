#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -ex

go install github.com/onsi/ginkgo/v2/ginkgo@$(go list -f '{{.Version}}' -m github.com/onsi/ginkgo/v2)
go install go.uber.org/mock/mockgen@v0.3.0

if ! [ -x "$(command -v golangci-lint)" ]; then
    echo "Downloading golangci-lint"

    curl -Lo tarball https://github.com/golangci/golangci-lint/releases/download/v2.3.0/golangci-lint-2.3.0-linux-amd64.tar.gz
    echo b15f994fe7b9885e3862d394e159025fc2ca808d0dccf2ce3d6a242896c2be0c tarball | sha256sum -c
    tar -C $(go env GOPATH)/bin --strip-components=1 -xf tarball golangci-lint-2.3.0-linux-amd64/golangci-lint
    rm tarball
fi


if ! [ -x "$(command -v spectral)" ]; then
    echo "Downloading spectral"

    curl -Lo spectral https://github.com/stoplightio/spectral/releases/download/v6.11.0/spectral-linux-x64
    echo 0e151d3dc5729750805428f79a152fa01dd4c203f1d9685ef19f4fd4696fcd5f spectral | sha256sum -c
    chmod +x spectral
    mv spectral $(go env GOPATH)/bin
fi
