#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

PROG=$(basename $0)

function usage {
    cat <<EOF
${PROG} [ --branch <branch> ] [ --dev <github username> ]

Options:
    --branch <branch>           Specify a branch to pull from (default: main)
    --dev <github username>     Specify a github user for developer replace, for WIP dev

For WIP dev work, to resync against the wip-dev-work-x branch in the github.com/myuserid/oran-hwmgr-plugin fork, run:
hack/resync-oran-hwmgr-plugin-api.sh --dev myuserid --branch wip-dev-work-x

EOF
    exit 1
}

#
# Defaults
#
declare BRANCH="main"
declare DEVELOPER=

#
# Process command-line arguments
#
LONGOPTS="help,branch:,dev:"
OPTS=$(getopt -o "hb:d:" --long "${LONGOPTS}" --name "$0" -- "$@")

if [ $? -ne 0 ]; then
    usage
    exit 1
fi

eval set -- "${OPTS}"

while :; do
    case "$1" in
        -b|--branch)
            BRANCH=$2
            shift 2
            ;;
        -d|--dev)
            DEVELOPER=$2
            shift 2
            ;;
        --)
            shift
            break
            ;;
        *)
            usage
            ;;
    esac
done

MODULES=("api/hwmgr-plugin" "pkg/inventory-client")

for MODULE in "${MODULES[@]}"; do
    CMD="go get github.com/openshift-kni/oran-hwmgr-plugin/${MODULE}@${BRANCH}"

    if [ -n "${DEVELOPER}" ]; then
        CMD="go mod edit -replace github.com/openshift-kni/oran-hwmgr-plugin/${MODULE}=github.com/${DEVELOPER}/oran-hwmgr-plugin/${MODULE}@${BRANCH}"
    fi

    # Remove stale -replace, if any
    go mod edit -dropreplace github.com/openshift-kni/oran-hwmgr-plugin/${MODULE}

    echo "Re-syncing ${MODULE} with: ${CMD}"
    if ! bash -c "${CMD}"; then
        echo "Command failed" >&2
        exit 1
    fi

    go mod tidy && go mod vendor
done
