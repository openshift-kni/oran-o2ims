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

"""
This script is intended to simplify development tasks.
"""

import click
import click_default_group

from . import command
from . import defaults

@click.group(
    cls=click_default_group.DefaultGroup,
    default="image",
    default_if_no_args=True,
)
def push() -> None:
    """
    Pushes build artifacts.
    """
    pass

@push.command()
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
    Pushes the container image to the image registry.
    """
    command.run(args=[
        "podman",
        "push",
        f"{repository}:{tag}",
    ])

@push.command()
@click.option(
    "--repository",
    help="Image repository.",
    default=defaults.BUNDLE_IMAGE_REPOSITORY
)
@click.option(
    "--tag",
    help="Image tag.",
    default=defaults.BUNDLE_IMAGE_TAG,
)
def bundle_image(
    repository: str,
    tag: str,
) -> None:
    """
    Pushes the catalog bundle image.
    """
    command.run(args=[
        "podman",
        "push",
        f"{repository}:{tag}",
    ])