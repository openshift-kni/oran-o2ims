/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// ValidateCallbackURL ensures that the URL used in subscription callback meets our requirements
func ValidateCallbackURL(ctx context.Context, c notifier.ClientProvider, callback string) error {
	// Validate URL
	u, err := url.Parse(callback)
	if err != nil {
		return fmt.Errorf("invalid callback URL: %w", err)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("invalid callback scheme %q, only https is supported", u.Scheme)
	}

	// A new client is created every time since the user may request a baseclient or oauthclient which we figure out at runtime
	client, err := c.NewClient(ctx, callback)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Call the url without retry for quick feedback
	if err := CheckCallbackReachabilityGET(ctx, client, callback); err != nil {
		return fmt.Errorf("reachability check failed: %w", err)
	}

	return nil
}

// CheckCallbackReachabilityGET sends a GET request to the callback URL and expects a 204 No Content response.
func CheckCallbackReachabilityGET(ctx context.Context, client *http.Client, callbackURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create GET request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform GET request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			slog.Warn("failed to close response body", "error", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: got %d, expected %d", resp.StatusCode, http.StatusNoContent)
	}

	slog.Info("Reachability check passed", "status", resp.StatusCode)
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
