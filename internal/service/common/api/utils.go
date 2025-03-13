/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// ValidateCallbackURL ensures that the URL used in subscription callback meets our requirements
func ValidateCallbackURL(callback string) error {
	u, err := url.Parse(callback)
	if err != nil {
		return fmt.Errorf("invalid callback URL: %w", err)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("invalid callback scheme %q, only https is supported", u.Scheme)
	}

	return nil
}

// GracefulShutdown allow graceful shutdown with timeout
func GracefulShutdown(srv *http.Server) error {
	// Create shutdown context with 10 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed graceful shutdown: %w", err)
	}

	slog.Info("Server gracefully stopped")
	return nil
}
