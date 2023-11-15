# -*- coding: utf-8 -*-

#
# Copyright (c) 2023 Red Hat Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except
# in compliance with the License. You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software distributed under the License
# is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
# or implied. See the License for the specific language governing permissions and limitations under
# the License.
#

import os
import shlex

import click

from . import command

@click.command()
def test() -> None:
    """
    Runs tests.
    """
    ginkgo_args = ["ginkgo", "run", "-r"]
    ginkgo_flags = os.getenv("GINKGO_FLAGS")
    if ginkgo_flags is not None:
        ginkgo_args.extend(shlex.split(ginkgo_flags))
    command.run(args=ginkgo_args)