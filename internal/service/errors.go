/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package service

import (
	"errors"
	"fmt"
	"net/http"

	jsoniter "github.com/json-iterator/go"
)

var ErrNotFound = errors.New("not found")

func SendError(w http.ResponseWriter, status int, msg string, args ...any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	cfg := jsoniter.Config{
		IndentionStep: 2,
	}
	api := cfg.Froze()
	writer := jsoniter.NewStream(api, w, 512)
	writer.WriteObjectStart()
	writer.WriteObjectField("status")
	writer.WriteInt(status)
	writer.WriteMore()
	detail := fmt.Sprintf(msg, args...)
	writer.WriteObjectField("detail")
	writer.WriteString(detail)
	writer.WriteObjectEnd()
	writer.Flush()
}
