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

import click

from . import command

# Default values:
DEFAULT_IMAGE_REPOSITORY = "quay.io/openshift-kni/oran-o2ims"
DEFAULT_IMAGE_TAG = "latest"

@click.group()
def build() -> None:
    """
    Builds binaries, images, etc.
    """

@build.command()
def binary() -> None:
    """
    Builds the binary.
    """
    command.run(args=["go", "build"])

@build.command()
@click.option(
    "--repository",
    help="Image repository.",
    default=DEFAULT_IMAGE_REPOSITORY,
)
@click.option(
    "--tag",
    help="Image tag.",
    default=DEFAULT_IMAGE_TAG,
)
def image(
    repository: str,
    tag: str,
) -> None:
    """
    Builds the container image.
    """
    command.run(args=[
        "podman",
        "build",
        "--tag", f"{repository}:{tag}",
        ".",
    ])