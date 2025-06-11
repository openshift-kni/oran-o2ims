/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package postgres

import "embed"

//go:embed files/*
var Artifacts embed.FS

const (
	ConfigFilePath  = "files/postgresql.conf"
	StartupFilePath = "files/init.sh"

	ConfigFileName  = "postgresql.conf"
	StartupFileName = "init.sh"
)
