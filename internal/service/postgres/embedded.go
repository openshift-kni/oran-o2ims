package postgres

import "embed"

//go:embed k8s/base/postgresql*
var Artifacts embed.FS

const (
	ConfigFilePath  = "k8s/base/postgresql-cfg/postgresql.conf"
	StartupFilePath = "k8s/base/postgresql-start/init.sh"

	ConfigFileName  = "postgresql.conf"
	StartupFileName = "init.sh"
)
