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
from . import defaults

@click.command()
@click.option(
    "--image",
    help="Image reference.",
    default=f"{defaults.IMAGE_REPOSITORY}:{defaults.IMAGE_TAG}",
)
def deploy(
    image: str,
) -> None:
    """
    Deploy controller to the K8S cluster specified in ~/.kube/config.
    """
    command.run(
         cwd=os.path.join("config", "manager"),
         args=[
            "kustomize", "edit", "set", "image",
            f"controller={image}",
         ],
    )
    with tempfile.TemporaryFile() as tmp:
        command.run(
            args=[
               "kustomize", "build",
               os.path.join("config", "default"),
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
def undeploy(
    ignore_not_found: bool,
) -> None:
    """
    Undeploy controller from the K8S cluster specified in ~/.kube/config.
    """
    with tempfile.TemporaryFile() as tmp:
        command.run(
            args=[
               "kustomize", "build",
               os.path.join("config", "default"),
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