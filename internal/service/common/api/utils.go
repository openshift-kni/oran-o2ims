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
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	commonapi "github.com/openshift-kni/oran-o2ims/api/common"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
)

// Resolver abstracts DNS resolution for testability.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// DefaultResolver is the DNS resolver used by ValidateCallbackURL. Tests may replace it.
var DefaultResolver Resolver = net.DefaultResolver

// TestResolver is a Resolver stub for use in tests. Known hosts return their
// configured addresses; unknown hosts return the hostname itself as the address
// so that test-server URLs (e.g., 127.0.0.1) pass through without setup.
type TestResolver struct {
	Addrs map[string][]string
}

func (r *TestResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if addrs, ok := r.Addrs[host]; ok {
		return addrs, nil
	}
	return []string{host}, nil
}

// DeniedCIDRStrings lists the CIDR ranges that must never be used as callback targets.
// Exported so tests can verify every entry parses correctly.
var DeniedCIDRStrings = []string{
	"127.0.0.0/8",    // loopback
	"::1/128",        // IPv6 loopback
	"169.254.0.0/16", // link-local (cloud metadata)
	"fe80::/10",      // IPv6 link-local
	"0.0.0.0/8",      // "this network"
}

var deniedCIDRs []*net.IPNet

func init() {
	for _, cidr := range DeniedCIDRStrings {
		_, network, _ := net.ParseCIDR(cidr)
		deniedCIDRs = append(deniedCIDRs, network)
	}
}

// ValidateCallbackURL ensures that the URL used in subscription callback meets our requirements.
func ValidateCallbackURL(ctx context.Context, c notifier.ClientProvider, callback, smoURL string) error {
	if err := ValidateCallbackTarget(ctx, callback, smoURL, DefaultResolver); err != nil {
		return err
	}

	client, err := c.NewClient(ctx, commonapi.OAuth)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	if err := CheckCallbackReachabilityGET(ctx, client, callback); err != nil {
		return fmt.Errorf("reachability check failed: %w", err)
	}

	return nil
}

// ValidateCallbackTarget checks that a callback URL targets a safe and permitted host.
// Callbacks must target the configured SMO host.
// All callbacks are checked against a blocklist of dangerous IP ranges.
func ValidateCallbackTarget(ctx context.Context, callbackURL, smoURL string, resolver Resolver) error {
	u, err := url.Parse(callbackURL)
	if err != nil {
		return fmt.Errorf("invalid callback URL: %w", err)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("invalid callback scheme %q, only https is supported", u.Scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("callback URL has no hostname")
	}

	if smoURL == "" {
		return fmt.Errorf("callback URLs are not allowed when SMO URL is not configured")
	}
	smoU, err := url.Parse(smoURL)
	if err != nil {
		return fmt.Errorf("invalid SMO URL: %w", err)
	}
	if !strings.EqualFold(hostname, smoU.Hostname()) {
		return fmt.Errorf("callback host %q does not match SMO host %q", hostname, smoU.Hostname())
	}

	addrs, err := resolver.LookupHost(ctx, hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve callback hostname %q: %w", hostname, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		for _, denied := range deniedCIDRs {
			if denied.Contains(ip) {
				return fmt.Errorf("callback hostname %q resolves to blocked address %s (%s)", hostname, addr, denied)
			}
		}
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
			slog.WarnContext(ctx, "failed to close response body", slog.Any("error", err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: got %d, expected %d", resp.StatusCode, http.StatusNoContent)
	}

	slog.InfoContext(ctx, "Reachability check passed", slog.Int("status", resp.StatusCode))
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

	slog.InfoContext(ctx, "Server gracefully stopped")
	return nil
}
