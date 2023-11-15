# -*- coding: utf-8 -*-

#
# Copyright (c) 2024 Red Hat Inc.
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
import tempfile

import click

from . import command

@click.command()
def install() -> None:
    """
    Install custom resource definitions into the K8S cluster specified in ~/.kube/config.
    """
    with tempfile.TemporaryFile() as tmp:
        command.run(
            args=[
               "kustomize", "build",
               os.path.join("config", "crd"),
            ],
            stdout=tmp,
        )
        tmp.seek(0)
        command.run(
             args=[
                  "kubectl", "apply",
                  "--filename", "-",
             ],
             stdin=tmp,
        )

@click.command()
@click.option(
    "--ignore-not-found",
    help="Ignore resource not found errors during deletion.",
    is_flag=True,
    default=False,
)
def uninstall(
    ignore_not_found: bool,
) -> None:
    """
    Uninstall custom resource definitions from the K8S cluster specified in ~/.kube/config.
    """
    with tempfile.TemporaryFile() as tmp:
        command.run(
            args=[
               "kustomize", "build",
               os.path.join("config", "crd"),
            ],
            stdout=tmp,
        )
        tmp.seek(0)
        command.run(
             args=[
                  "kubectl", "delete",
                  f"--ignore-not-found={str(ignore_not_found).lower()}",
                  "--filename", "-",
             ],
             stdin=tmp,
        )