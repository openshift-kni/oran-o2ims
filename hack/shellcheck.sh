#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

VERSION="0.7.2"

rootdir=$(git rev-parse --show-toplevel)
if [ -z "${rootdir}" ]; then
    echo "Failed to determine top level directory"
    exit 1
fi

bindir="${rootdir}/bin"
shellcheck="${bindir}/shellcheck"

function get_tool {
    mkdir -p "${bindir}"
    echo "Downloading shellcheck tool"
    scversion="v${VERSION}"
    wget -qO- "https://github.com/koalaman/shellcheck/releases/download/${scversion}/shellcheck-${scversion}.linux.x86_64.tar.xz" \
        | tar -xJ -C "${bindir}" --strip=1 shellcheck-${scversion}/shellcheck
}

if [ ! -f ${shellcheck} ]; then
    get_tool
else
    current_ver=$("${shellcheck}" --version | grep '^version:' | awk '{print $2}')
    if [ "${current_ver}" != "${VERSION}" ]; then
        get_tool
    fi
fi

find . -name '*.sh' -not -path './vendor/*' -not -path './*/vendor/*' -not -path './git/*' \
    -not -path './bin/*' -not -path './testbin/*' -print0 \
    | xargs -0 --no-run-if-empty ${shellcheck} -x
