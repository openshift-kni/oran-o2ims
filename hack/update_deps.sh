#!/bin/bash

set -e

# Handle the exported api/hardwaremanagement submodule first
pushd api/hardwaremanagement >/dev/null
go mod tidy
popd >/dev/null

go mod vendor
go mod tidy

