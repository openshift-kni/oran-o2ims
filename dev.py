#!/usr/bin/env python3
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
This script is a development tool for the project.
"""

import logging

import click

import dev

@click.group()
def cli():
    """
    Development tools.
    """
    pass

# Add the commands:
cli.add_command(dev.build)
cli.add_command(dev.ci_job)
cli.add_command(dev.clean)
cli.add_command(dev.deploy)
cli.add_command(dev.fmt)
cli.add_command(dev.generate)
cli.add_command(dev.install)
cli.add_command(dev.lint)
cli.add_command(dev.push)
cli.add_command(dev.run)
cli.add_command(dev.setup)
cli.add_command(dev.test)
cli.add_command(dev.undeploy)
cli.add_command(dev.uninstall)
cli.add_command(dev.update)
cli.add_command(dev.vet)

if __name__ == '__main__':
    # Configure logging:
    formatter = dev.Formatter()
    handler = logging.StreamHandler()
    handler.setFormatter(formatter)
    logging.root.handlers = [handler]
    logging.root.level = logging.DEBUG

    # Run the command:
    cli()
    #try:
    #    cli()
    #except Exception as err:
    #    logging.error(err)
    #    sys.exit(1)
