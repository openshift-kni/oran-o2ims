/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cincinnati

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-semver/semver"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	configv1 "github.com/openshift/api/config/v1"
	"golang.org/x/net/http/httpproxy"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultGraphURL is the public Red Hat Cincinnati update graph endpoint.
	DefaultGraphURL = "https://api.openshift.com/api/upgrades_info/v1/graph"
	defaultTimeout  = 30 * time.Second

	trustedCABundleNamespace = "openshift-config-managed"
	trustedCABundleName      = "trusted-ca-bundle"
	trustedCABundleKey       = "ca-bundle.crt"
)

// graph is a Cincinnati update graph response (unconditional upgrade paths only).
type graph struct {
	Nodes []node  `json:"nodes"`
	Edges [][]int `json:"edges"` // each edge is [origin, destination] referencing Nodes by index
}

// node holds the version string of a release in the Cincinnati graph.
type node struct {
	Version string `json:"version"`
}

// SelectIntermediateVersion fetches the Cincinnati graph and selects the best
// EUS intermediate version for an upgrade from currentVersion to targetVersion.
// It builds an HTTP client with the cluster's trusted CA bundle and proxy
// configuration so it works in connected, proxied, and disconnected environments.
func SelectIntermediateVersion(
	ctx context.Context, k8sClient client.Client, logger *slog.Logger,
	upstream *url.URL, channel, arch, currentVersion, targetVersion string,
) (string, error) {
	httpClient, err := getHTTPClient(ctx, k8sClient, logger)
	if err != nil {
		return "", fmt.Errorf("unable to build HTTP client to retrieve update graph: %w", err)
	}
	g, err := getGraph(ctx, logger, httpClient, upstream, channel, arch)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve update graph: %w", err)
	}
	selected, err := findBestIntermediateVersion(g, upstream, channel, currentVersion, targetVersion)
	if err != nil {
		return "", typederrors.NewInputError("%s", err.Error())
	}
	return selected, nil
}

// getHTTPClient creates an HTTP client configured with:
//   - Trusted CAs from openshift-config-managed/trusted-ca-bundle (contains both
//     system CAs and custom CAs merged by the Cluster Network Operator)
//   - Proxy configuration from the cluster Proxy CR
//
// Falls back gracefully if cluster resources are unavailable.
func getHTTPClient(ctx context.Context, k8sClient client.Client, logger *slog.Logger) (*http.Client, error) {
	transport := &http.Transport{}

	if err := configureProxy(ctx, k8sClient, transport); err != nil {
		return nil, err
	}

	tlsConfig, err := getTLSConfig(ctx, k8sClient, logger)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}, nil
}

// getTLSConfig reads the cluster's trusted-ca-bundle and returns a TLS
// config with those CAs for querying the update graph. If the ConfigMap
// is not found, returns nil so the transport uses the system CA bundle.
func getTLSConfig(ctx context.Context, k8sClient client.Client, logger *slog.Logger) (*tls.Config, error) {
	cm := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: trustedCABundleNamespace,
		Name:      trustedCABundleName,
	}, cm); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil //nolint:nilnil // Not found is not an error
		}
		return nil, fmt.Errorf("failed to read trusted-ca-bundle: %w", err)
	}

	bundle, ok := cm.Data[trustedCABundleKey]
	if !ok || bundle == "" {
		return nil, nil //nolint:nilnil // No data is not an error
	}

	// The Cluster Network Operator merges system CAs and any custom CAs
	// from openshift-config/user-ca-bundle into this ConfigMap, so the
	// bundle already contains the full set of trusted certificates.
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM([]byte(bundle)); !ok {
		return nil, fmt.Errorf(
			"configMap %s/%s key %q contains no valid PEM certificates",
			trustedCABundleNamespace, trustedCABundleName, trustedCABundleKey)
	}
	logger.InfoContext(ctx, "Loaded trusted CA bundle for Cincinnati TLS",
		slog.String("configMap", trustedCABundleNamespace+"/"+trustedCABundleName))

	return &tls.Config{
		RootCAs: certPool,
	}, nil
}

// configureProxy reads the cluster Proxy CR and configures the transport's
// proxy function if proxy settings are present.
func configureProxy(ctx context.Context, k8sClient client.Client, transport *http.Transport) error {
	proxy := &configv1.Proxy{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, proxy); err != nil {
		if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to read Proxy CR: %w", err)
	}

	// On OpenShift the Proxy CR is the authoritative source for proxy
	// configuration. Setting transport.Proxy here intentionally overrides
	// Go's default http.ProxyFromEnvironment. When all proxy fields are
	// empty, connections go direct.
	proxyConfig := &httpproxy.Config{
		HTTPProxy:  proxy.Status.HTTPProxy,
		HTTPSProxy: proxy.Status.HTTPSProxy,
		NoProxy:    proxy.Status.NoProxy,
	}
	proxyFunc := proxyConfig.ProxyFunc()
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if req.URL == nil {
			return nil, nil //nolint:nilnil // No URL means no proxy, direct connection
		}
		return proxyFunc(req.URL)
	}
	return nil
}

// getGraph fetches the update graph for the given upstream URL, channel, and arch.
func getGraph(ctx context.Context, logger *slog.Logger, httpClient *http.Client, upstream *url.URL, channel, arch string) (*graph, error) {
	uri := *upstream
	queryParams := uri.Query()
	queryParams.Set("channel", channel)
	queryParams.Set("arch", arch)
	uri.RawQuery = queryParams.Encode()

	// Download the update graph from the upstream URL.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		var dnsErr *net.DNSError
		if stderrors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, typederrors.NewInputError("%s", err.Error())
		}
		return nil, fmt.Errorf("unable to query update graph: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.ErrorContext(ctx, "Failed to close Cincinnati response body",
				slog.Any("error", err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected HTTP status: %d from %s",
			resp.StatusCode, upstream.String())
		if resp.StatusCode == http.StatusNotFound {
			return nil, typederrors.NewInputError("%s", err.Error())
		}
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("unable to read HTTP response: %w", err)
	}

	// Parse the response body into a graph.
	var graph graph
	if err := json.Unmarshal(body, &graph); err != nil {
		return nil, fmt.Errorf("unable to parse update graph: %w", err)
	}
	return &graph, nil
}

// findBestIntermediateVersion finds the best EUS intermediate version from the
// Cincinnati graph for an upgrade from currentVersion to targetVersion.
//
// For example, upgrading 4.20.0 -> 4.22.0 requires an intermediate 4.21.z.
// The function searches all 4.21.z nodes that satisfy two conditions:
//   - A direct unconditional edge exists from currentVersion to the candidate
//   - A direct unconditional edge exists from the candidate to targetVersion
//
// Among all qualifying candidates, it returns the one with the highest patch
// version (e.g. 4.21.9 over 4.21.5).
func findBestIntermediateVersion(g *graph, upstream *url.URL, channel, currentVersion, targetVersion string) (string, error) {
	if g == nil {
		return "", fmt.Errorf("cincinnati graph is nil")
	}
	if _, err := semver.NewVersion(currentVersion); err != nil {
		return "", fmt.Errorf("invalid current version %q: %w", currentVersion, err)
	}
	target, err := semver.NewVersion(targetVersion)
	if err != nil {
		return "", fmt.Errorf("invalid target version %q: %w", targetVersion, err)
	}

	// Step 1: Parse all node versions and locate the current and target nodes
	// by their exact version string.
	currentIdx := -1
	targetIdx := -1
	nodes := make([]*semver.Version, len(g.Nodes))
	for i, n := range g.Nodes {
		v, err := semver.NewVersion(n.Version)
		if err != nil {
			continue
		}
		nodes[i] = v
		if n.Version == currentVersion {
			currentIdx = i
		}
		if n.Version == targetVersion {
			targetIdx = i
		}
	}
	if currentIdx < 0 {
		return "", fmt.Errorf("current version %s not found in update graph %s with channel %s",
			currentVersion, upstream.String(), channel)
	}
	if targetIdx < 0 {
		return "", fmt.Errorf("target version %s not found in update graph %s with channel %s",
			targetVersion, upstream.String(), channel)
	}

	// Step 2: Build an adjacency map from unconditional edges only.
	// Conditional edges (with risks) are excluded — CVO evaluates those on
	// the spoke and we don't want to auto-select a risky intermediate.
	outEdges := make(map[int]map[int]struct{}, len(g.Nodes))
	for _, e := range g.Edges {
		if len(e) < 2 {
			continue
		}
		from, to := e[0], e[1]
		if outEdges[from] == nil {
			outEdges[from] = map[int]struct{}{}
		}
		outEdges[from][to] = struct{}{}
	}

	// Step 3: Find the best intermediate — must be target.Minor-1 (e.g. 4.21
	// for a 4.20 -> 4.22 upgrade), reachable from current, and with an edge to
	// target. Pick the highest patch version among all qualifying candidates.
	intermediateMinor := target.Minor - 1
	var best string
	var bestVer *semver.Version
	for i, v := range nodes {
		if v == nil {
			continue
		}
		// Only consider nodes in the intermediate minor release.
		if v.Major != target.Major || v.Minor != intermediateMinor {
			continue
		}
		// Must have a direct edge: current -> candidate.
		if _, ok := outEdges[currentIdx][i]; !ok {
			continue
		}
		// Must have a direct edge: candidate -> target.
		if _, ok := outEdges[i][targetIdx]; !ok {
			continue
		}
		// Keep the highest patch version.
		if bestVer == nil || v.Compare(*bestVer) > 0 {
			bestVer = v
			best = g.Nodes[i].Version
		}
	}
	if best == "" {
		return "", fmt.Errorf(
			"unable to select intermediate version for %s to %s upgrade: "+
				"no valid upgrade path found in the update graph %s with channel %s",
			currentVersion, targetVersion, upstream.String(), channel)
	}
	return best, nil
}
