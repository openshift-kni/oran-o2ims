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
