package files

import "embed"

var (
	//go:embed alarms
	Alarms embed.FS
	//go:embed controllers
	Controllers embed.FS
)

const (
	AlarmDictionaryVersion = "v1"
)
