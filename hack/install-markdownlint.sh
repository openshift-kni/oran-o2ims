#!/bin/bash -xe
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#
# Following example of: https://github.com/openshift/enhancements/blob/master/hack/install-markdownlint.sh

cat /etc/redhat-release || echo "No /etc/redhat-release"

if grep -q 'Red Hat Enterprise Linux' /etc/redhat-release; then
    # https://github.com/nodesource/distributions/blob/master/DEV_README.md#enterprise-linux-based-distributions
    yum module disable -y nodejs
    curl -fsSL https://rpm.nodesource.com/setup_23.x -o nodesource_setup.sh
    bash nodesource_setup.sh
    yum -y install nodejs
else
    # Fedora has a module we can use
    dnf -y module enable nodejs:16
    dnf -y install nodejs
fi

npm install -g markdownlint@v0.38.0 markdownlint-cli2@v0.18.1 --save-dev
