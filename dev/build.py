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
import click_default_group

from . import command
from . import defaults

@click.group(
    cls=click_default_group.DefaultGroup,
    default="binary",
    default_if_no_args=True,
)
def build() -> None:
    """
    Builds binaries, images, catalogs, etc.
    """
    pass

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
    default=defaults.IMAGE_REPOSITORY,
)
@click.option(
    "--tag",
    help="Image tag.",
    default=defaults.IMAGE_TAG,
)
def image(
    repository: str,
    tag: str,
) -> None:
    """
    Builds the container image.
    """
    command.run(args=[
        "podman", "build",
        "--tag", f"{repository}:{tag}",
        "--file", "Containerfile",
    ])

@build.command()
@click.option(
    "--repository",
    help="Image repository.",
    default=defaults.IMAGE_REPOSITORY,
)
@click.option(
    "--tag",
    help="Image tag.",
    default=defaults.IMAGE_TAG,
)
def bundle_image(
    repository: str,
    tag: str,
) -> None:
    """
    Builds the bundle image.
    """
    command.run(args=[
        "podman", "build",
        "--file", "bundle.Dockerfile",
        "--tag", f"{repository}:{tag}"
        ".",
    ])