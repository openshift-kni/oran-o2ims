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

import click

from . import command
from . import defaults

@click.group(invoke_without_command=True)
@click.pass_context
def generate(ctx: click.Context):
    """
    Generate code.
    """
    if ctx.invoked_subcommand is not None:
        return
    ctx.invoke(mocks)
    ctx.invoke(manifests)
    ctx.invoke(deep_copy)
    ctx.invoke(bundle)

@generate.command()
def mocks() -> None:
    """
    Generate mocks.
    """
    command.run(args=["go", "generate", "./..."])

@generate.command()
def manifests() -> None:
    """
    Generate webhook configuration, cluster role and custom resource definitions.
    """
    command.run(args=[
        "controller-gen",
        "rbac:roleName=manager-role",
        "crd",
        "webhook",
        "paths=./...",
        f"output:crd:artifacts:config={os.path.join('config', 'crd', 'bases')}",
    ])

@generate.command()
def deep_copy() -> None:
    """
    Generate deep copy code.
    """
    command.run(args=[
        "controller-gen",
	    f"object:headerFile={os.path.join('hack', 'boilerplate.go.txt')}",
        "paths=./...",
    ])

@generate.command()
@click.option(
    "--image",
    help="Image reference.",
    default=f"{defaults.IMAGE_REPOSITORY}:{defaults.IMAGE_TAG}",
)
def bundle(image: str) -> None:
    """
    Generate operator bundle.
    """
    # Generate the manifests:
    command.run(args=[
        "operator-sdk", "generate", "kustomize", "manifests",
        "--quiet",
        "--apis-dir", "api",
    ])

    # Kustomize the manifests:
    command.run(
        cwd=os.path.join("config", "manager"),
        args=[
            "kustomize", "edit", "set", "image",
            f"controller={image}",
        ],
    )

    # Generate the bundle:
    command.run(args=[
        "kustomize", "build",
        os.path.join("config", "manifests"),
    ])

    # Validate the bundle:
    command.run(args=[
        "operator-sdk", "bundle", "validate",
        os.path.join(".", "bundle"),
    ])
