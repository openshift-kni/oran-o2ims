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

import logging
import os
import pathlib
import shutil
import tempfile

import click

from . import command

@click.command()
def setup() -> None:
    """
    Prepares the development environment.
    """
    # Install tools:
    install_go()
    install_ginkgo()
    install_mockgen()
    install_golangci_lint()

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
    go_install(tool="go.uber.org/mock/mockgen", version="v0.3.0")

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
        logging.info(f"Tool 'golangci-lint' tool is already installed at '{binary}'")
        return

    # Find an installation directory:
    directory = find_install_directory()

    # Now download, verify and install it from the GitHub releases page:
    version = "1.55.2"
    digest = "ca21c961a33be3bc15e4292dc40c98c8dcc5463a7b6768a3afc123761630c09c"
    platform = go_env("GOOS")
    architecture = go_env("GOARCH")
    url = (
        "https://github.com"
        f"/golangci/golangci-lint/releases/download/v{version}"
        f"/golangci-lint-{version}-{platform}-{architecture}.tar.gz"
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
            "--directory", directory,
            "--extract",
            "--file", tarball_file,
            "--strip-components", "1",
            f"golangci-lint-{version}-{platform}-{architecture}/golangci-lint",
        ])
    finally:
        shutil.rmtree(tmp)

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
    # Check if the binary already exists:
    segments = tool.split("/")
    binary = shutil.which(segments[-1])
    if binary is not None:
        logging.info(f"Tool '{tool}' is already installed at '{binary}'")
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


def find_install_directory() -> str | None:
    """
    Tries to find a writable directory that is in the path.
    """
    path = os.getenv("PATH")
    if path is None:
        return None
    directories = path.split(os.path.pathsep)
    directories = {pathlib.Path(d) for d in directories}

    # Discard the directories that aren't writable:
    directories = {d for d in directories if os.access(str(d), os.W_OK)}

    # We want to install in the most speficic directory, so we discard directories that are
    # parents of other candidate directories:
    tmp = directories.copy()
    for directory in tmp:
        for parent in directory.parents:
            directories.discard(parent)

    # Discard Python virtual environment 'bin' directories, those whose parent contains
    # a 'pyvenv.cfg' file:
    tmp = directories.copy()
    for directory in tmp:
        if directory.parent.joinpath("pyvenv.cfg"):
            directories.discard(directory)

    # Select the candidate that is closer to the current working directory. This is intended to
    # select a directory that is specific to the project rather than one that is shared by
    # multiple projects. For example, if the project is in the '/files/projects/myproject'
    # directory and '/files/projects/myproject/bin' is in the path then this will always be
    # prefered because the distance will be one, while the distance to '/usr/bin' will be six.
    work_directory = pathlib.Path(os.getcwd())
    min_directory = None
    min_distance = None
    for directory in directories:
        distance = path_distance(work_directory, directory)
        if min_distance is None or distance < min_distance:
            min_directory = directory
    if min_directory is None:
        return None
    return str(min_directory)

def path_distance(a: pathlib.Path, b: pathlib.Path) -> int:
    """
    Caculates a distance between two directory paths. This distance is the number of 'cd' commands
    that would be needed to change from one of them to the other. For example, the distance between
    '/home/myuser/bin' and '/files/projects/myproject/bin' would be seven because it would need
    the following directory changes:

    /home/myuser/bin $ cd ..
    /home/myuser $ cd ..
    /home $ cd ..
    / $ cd files
    /files $ cd projects
    /files/projects $ cd myproject
    /files/projects/myproject $ cd bin
    /files/projects/myproject/bin $
    """
    a = a.absolute().parts
    b = b.absolute().parts
    while True:
        if len(a) == 0:
            return len(b)
        if len(b) == 0:
            return len(a)
        if a[0] != b[0]:
            return len(a) + len(b)
        a = a[1:]
        b = b[1:]