#!/bin/bash -xe
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#
# Following example of: https://github.com/openshift/enhancements/blob/master/hack/install-markdownlint.sh

dnf -y module enable nodejs:24
dnf -y install nodejs

npm install -g markdownlint@v0.41.0 markdownlint-cli2@v0.22.1 --save-dev
