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

import functools
import logging
import os
import pathlib
import re
import shutil
import stat
import tempfile

import click

from . import command
from . import versions

@click.command()
def setup() -> None:
    """
    Prepares the development environment.
    """
    # Install Go:
    install_go()

    # Install other tools:
    install_controller_gen()
    install_ginkgo()
    install_golangci_lint()
    install_kustomize()
    install_mockgen()
    install_operator_sdk()
    install_opm()
    install_setup_envtest()
    install_spectral()

def install_go() -> None:
    """
    Installs the Go compiler.
    """
    # First check if it is already installed:
    directory = shutil.which("go")
    if directory is not None:
        logging.info(f"The 'go' tool is already installed at '{directory}'")
        return

    # We could install Go with an approach similar to what we do for 'golangci-lint', downloadi
    # it from the Go downloads page. But then we would need to find a directory where to install
    # it. We don't want to do that for now, so instead we just complain.
    raise Exception("The 'go' tool isn't available")

def install_ginkgo() -> None:
    """
    Installs the 'ginkgo' tool.
    """
    go_install(tool="github.com/onsi/ginkgo/v2/ginkgo")

def install_mockgen() -> None:
    """
    Installs the 'mockgen' tool.
    """
    go_install(
        tool="go.uber.org/mock/mockgen",
        version=f"v{versions.MOCKGEN}",
    )

def install_golangci_lint() -> None:
    """
    Installs the 'golangci-lint' tool.
    """
    # The team that develops this tool doesn't recommend installing it with 'go install', see
    # here for details:
    #
    # https://golangci-lint.run/usage/install/#install-from-source
    #
    # So instead of that we will donwload the artifact from the GitHub releases page and install
    # it manually.

    # First check if it is already installed:
    binary = shutil.which("golangci-lint")
    if binary is not None:
        logging.info(f"Tool 'golangci-lint' is already installed at '{binary}'")
        return

    # Find an installation directory:
    bin = select_bin_directory()

    # Now download, verify and install it from the GitHub releases page:
    digest = "ca21c961a33be3bc15e4292dc40c98c8dcc5463a7b6768a3afc123761630c09c"
    platform = go_env("GOOS")
    architecture = go_env("GOARCH")
    url = (
        "https://github.com"
        f"/golangci/golangci-lint/releases/download/v{versions.GOLANGCI_LINT}"
        f"/golangci-lint-{versions.GOLANGCI_LINT}-{platform}-{architecture}.tar.gz"
    )
    tmp = tempfile.mkdtemp()
    try:
        tarball_file = os.path.join(tmp, "tarball")
        command.run(args=[
            "curl",
            "--location",
            "--silent",
            "--fail",
            "--output", tarball_file,
            url,
        ])
        digest_file = os.path.join(tmp, "digest")
        with open(file=digest_file, encoding="utf-8", mode="w") as file:
            file.write(f"{digest} {tarball_file}\n")
        command.run(args=[
            "sha256sum",
            "--check",
            digest_file,
        ])
        command.run(args=[
            "tar",
            "--directory", bin,
            "--extract",
            "--file", tarball_file,
            "--strip-components", "1",
            f"golangci-lint-{versions.GOLANGCI_LINT}-{platform}-{architecture}/golangci-lint",
        ])
    finally:
        shutil.rmtree(tmp)

def install_opm() -> None:
    """
    Installs the 'opm' tool.
    """
    # First check if the binary already exists:
    binary = shutil.which("opm")
    if binary is not None:
        logging.info(f"Tool 'opm' is already installed at '{binary}'")
        return

    # Find an installation directory:
    bin = select_bin_directory()

    # Download and install the binary:
    platform = go_env("GOOS")
    architecture = go_env("GOARCH")
    url = (
        "https://github.com"
        f"/operator-framework/operator-registry/releases/download/v{versions.OPM}"
        f"/{platform}-{architecture}-opm"
    )
    file = os.path.join(bin, "opm")
    command.run(args=[
        "curl",
        "--location",
        "--silent",
        "--fail",
        "--output", file,
        url,
    ])

    # Add the execution permission to the binary:
    mode = os.stat(file).st_mode
    mode |= stat.S_IXUSR
    os.chmod(file, mode)

def install_operator_sdk() -> None:
    """
    Install the operator SDK.
    """
    # First check if the binary already exists:
    binary = shutil.which("operator-sdk")
    if binary is not None:
        logging.info(f"Tool 'operator-sdk' is already installed at '{binary}'")
        return

    # Find an installation directory:
    bin = select_bin_directory()

    # Download and install the binary:
    platform = go_env("GOOS")
    architecture = go_env("GOARCH")
    url = (
        "https://github.com"
		f"/operator-framework/operator-sdk/releases/download/v{versions.OPERATOR_SDK}"
        f"/operator-sdk_{platform}_{architecture}"
    )
    file = os.path.join(bin, "operator-sdk")
    command.run(args=[
        "curl",
        "--location",
        "--silent",
        "--fail",
        "--output", file,
        url,
    ])

    # Add the execution permission to the binary:
    mode = os.stat(file).st_mode
    mode |= stat.S_IXUSR
    os.chmod(file, mode)

def install_kustomize() -> None:
    """
    Install the 'kustomize' tool.
    """
    go_install(
        tool="sigs.k8s.io/kustomize/kustomize/v5",
        version=f"v{versions.KUSTOMIZE}",
    )

def install_controller_gen() -> None:
    """
    Install the 'controller-gen' tool.
    """
    go_install(
        tool="sigs.k8s.io/controller-tools/cmd/controller-gen",
        version=f"v{versions.CONTROLLER_GEN}",
    )

def install_setup_envtest() -> None:
    """Install the 'envtest' tool."""
    go_install(
        tool="sigs.k8s.io/controller-runtime/tools/setup-envtest",
        version="latest",
    )

def install_spectral() -> None:
    """
    Install spectral.
    """
    # First check if the binary already exists:
    binary = shutil.which("spectral")
    if binary is not None:
        logging.info(f"Tool 'spectral' is already installed at '{binary}'")
        return

    # Find an installation directory:
    bin = select_bin_directory()

    # Download and install the binary:
    platform = go_env("GOOS")
    architecture = go_env("GOARCH")
    if architecture == "amd64":
        architecture = "x64"
    url = (
        "https://github.com"
		f"/stoplightio/spectral/releases/download/v{versions.SPECTRAL}"
        f"/spectral-{platform}-{architecture}"
    )
    file = os.path.join(bin, "spectral")
    command.run(args=[
        "curl",
        "--location",
        "--silent",
        "--fail",
        "--output", file,
        url,
    ])

    # Add the execution permission to the binary:
    mode = os.stat(file).st_mode
    mode |= stat.S_IXUSR
    os.chmod(file, mode)

@functools.cache
def go_env(var: str) -> str:
    """
    Returns the value of an environment variable as reported by the 'go env' command. For example,
    in a Linux platform the value for 'GOOS' will be 'linux'.
    """
    code, output = command.eval(args=[
        "go", "env", var,
    ])
    if code != 0:
        raise Exception(f"Failed to get Go environment variable '{var}'")
    return output

def go_install(
    tool: str,
    version: str | None = None,
) -> None:
    """
    Uses the 'go install' command to install the given tool.

    The tool parameter is the complete Go path of the binary. For example, for the 'ginkgo' tool
    the value should be 'github.com/onsi/ginkgo/v2/ginkgo'.

    The version is required when the tool isn't a dependency of the project. For example, the
    'mockgen' command isn't usually a dependency of the project because the mock code that it
    generates doesn't depend on the mock generation code itself. In those cases the version
    can't be extracted from the 'go.mod' file, so it needs to be provided explicitly.
    """
    # Check if the binary already exists. Note that usually the name of the binary will be the
    # last segment of the package path, but that last segment can also be a version number like
    # `v5`. In that case we need to ignore it and use the previous segment.
    segments = tool.split("/")
    name = segments[-1]
    if re.match(r"^v\d+$", name):
        name = segments[-2]
    binary = shutil.which(name)
    if binary is not None:
        logging.info(f"Tool '{name}' is already installed at '{binary}'")
        return

    # If the version hasn't been specified we need to extract it from the dependencies. Note that
    # we need to try with the complete package path, and then with the parent, so on. That is
    # because we don't know what part of the path corresponds the Go module.
    if version is None:
        for i in range(len(segments)-1, 2, -1):
            package = "/".join(segments[0:i])
            code, version = command.eval(args=[
                "go", "list", "-f", "{{.Version}}", "-m", package
            ])
            if code == 0:
                break
        if version is None:
            raise Exception(f"Failed to find version for tool '{tool}'")
        logging.info(f"Version of tool '{tool}' is '{version}'")

    # Try to install:
    command.run(args=[
        "go", "install", f"{tool}@{version}",
    ])


def select_bin_directory() -> str:
    """
    Tries to find the binaries directory.
    """
    # Preapre a set containing the directories from the PATH environment variable:
    path = os.getenv("PATH")
    path = path.split(os.pathsep)
    path = {pathlib.Path(d) for d in path}

    # Use the project specific binaries directory it is in the path:
    project = pathlib.Path(__file__).parent.parent
    bin = project.parent / ".local" / "bin"
    if bin in path:
        return str(bin)

    # Use the binaries directory inside the repository if it is in the path:
    bin = project / "bin"
    if bin in path:
        return str(bin)

    # Use the Go binaries directory if it is in the path:
    bin = go_env("GOBIN")
    if bin in path:
        return bin
    root = go_env("GOROOT")
    root = pathlib.Path(root)
    bin = root / "bin"
    if bin in path:
        return str(bin)

    # If we are here then we failed to find a suitable binaries directory:
    raise Exception("Failed to select a suitable binaries directory")