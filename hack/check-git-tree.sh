#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

RC=0
if [ -n "$(git status --porcelain)" ]; then
    echo "Unstaged or untracked changes exist:"
    git status --porcelain
    git diff
    RC=1
else
    echo "git tree is clean"
fi

exit ${RC}
