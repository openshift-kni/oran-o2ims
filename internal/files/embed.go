package files

import "embed"

var (
	//go:embed alarms
	Alarms embed.FS
)

const (
	AlarmDictionaryVersion = "v1"
)
