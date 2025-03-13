/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package files

import "embed"

var (
	//go:embed controllers
	Controllers embed.FS
)

const (
	AlarmDictionaryVersion = "v1"
)
