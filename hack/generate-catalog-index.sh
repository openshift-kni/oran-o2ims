#!/bin/bash
#
# Generate catalog index
#

declare WORKDIR=

function usage {
    cat <<EOF
Usage: $0 --opm <opm-executable> --name <package-name> --channel <channel>
EOF
    exit 1
}

function cleanup {
    if [ -n "${WORKDIR}" ] && [ -d "${WORKDIR}" ]; then
        rm -rf "${WORKDIR}"
    fi
}

trap cleanup EXIT

#
# Process cmdline arguments
#
declare OPM=
declare NAME=
declare CHANNEL=
declare VERSION=

longopts=(
    "help"
    "opm:"
    "name:"
    "channel:"
    "version:"
)

longopts_str=$(IFS=,; echo "${longopts[*]}")

if ! OPTS=$(getopt -o "h" --long "${longopts_str}" --name "$0" -- "$@"); then
    usage
    exit 1
fi

eval set -- "${OPTS}"

while :; do
    case "$1" in
        --opm)
            OPM="$2"
            shift 2
            ;;
        --name)
            NAME="$2"
            shift 2
            ;;
        --channel)
            CHANNEL="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
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

if [ -z "${OPM}" ] || [ -z "${NAME}" ] || [ -z "${CHANNEL}" ] || [ -z "${VERSION}" ]; then
    usage
fi

WORKDIR=$(mktemp -d --tmpdir genindex.XXXXXX)

${OPM} init ${NAME} --default-channel=${CHANNEL} --output=yaml > ${WORKDIR}/index.yaml
cat <<EOF >> ${WORKDIR}/index.yaml
---
schema: olm.channel
package: ${NAME}
name: ${CHANNEL}
entries:
  - name: ${NAME}.v${VERSION}
EOF

if [ ! -f catalog/index.yaml ] || ! cmp ${WORKDIR}/index.yaml catalog/index.yaml; then
    mv ${WORKDIR}/index.yaml catalog/index.yaml
fi

