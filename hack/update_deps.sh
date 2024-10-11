#!/bin/bash

set -e

# Handle the exported api/hardwaremanagement submodule first
pushd api/hardwaremanagement >/dev/null
go mod tidy
popd >/dev/null

go work vendor
go mod tidy

