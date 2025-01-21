package files

import "embed"

var (
	//go:embed controllers
	Controllers embed.FS
)

const (
	AlarmDictionaryVersion = "v1"
)
