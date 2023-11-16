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

# Default values:
DEFAULT_IMAGE_REPOSITORY = "quay.io/openshift-kni/oran-o2ims"
DEFAULT_IMAGE_TAG = "latest"

@click.group(
    cls=click_default_group.DefaultGroup,
    default="image",
    default_if_no_args=True,
)
def push() -> None:
    """
    Pushes build artifacts.
    """

@push.command()
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
    Pushes the container image to the image registry.
    """
    command.run(args=[
        "podman",
        "push",
        f"{repository}:{tag}",
    ])