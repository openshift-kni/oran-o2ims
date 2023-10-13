/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
*/

package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	// Create the logger:
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(os.Stdout, options)
	logger := slog.New(handler)

	// Create the server:
	server, err := NewServer().
		SetLogger(logger).
		SetHandler(&echoHandler{}).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create server",
			"error", err,
		)
		os.Exit(1)
	}

	// Start the server:
	err = http.ListenAndServe(":8080", server)
	if err != nil {
		logger.Error(
			"server finished with error",
			"error", err,
		)
	}
}

type echoHandler struct {
}

func (h *echoHandler) Serve(w http.ResponseWriter, r *Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "filter=%s", r.Filter)
}
