/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clients

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultExpirationSeconds = int64(10 * 60)
	// Refresh at 80% of TTL to avoid using expired tokens
	refreshFraction = 0.8
)

// TokenRequestTokenSource implements oauth2.TokenSource using the Kubernetes
// TokenRequest API to mint short-lived, audience-scoped tokens.
type TokenRequestTokenSource struct {
	clientset   kubernetes.Interface
	namespace   string
	accountName string
	audience    string

	mu        sync.Mutex
	token     *oauth2.Token
	refreshAt time.Time
}

// NewTokenRequestTokenSource creates a token source that mints audience-scoped
// tokens via the Kubernetes TokenRequest API.
func NewTokenRequestTokenSource(clientset kubernetes.Interface, namespace, accountName, audience string) *TokenRequestTokenSource {
	return &TokenRequestTokenSource{
		clientset:   clientset,
		namespace:   namespace,
		accountName: accountName,
		audience:    audience,
	}
}

// Token returns a cached token or mints a new one via TokenRequest API.
func (ts *TokenRequestTokenSource) Token() (*oauth2.Token, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.token != nil && ts.token.Valid() && time.Now().Before(ts.refreshAt) {
		return ts.token, nil
	}

	expirationSeconds := defaultExpirationSeconds
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}
	if ts.audience != "" {
		tokenRequest.Spec.Audiences = []string{ts.audience}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := ts.clientset.CoreV1().ServiceAccounts(ts.namespace).CreateToken(
		ctx, ts.accountName, tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create token for %s/%s with audience %s: %w",
			ts.namespace, ts.accountName, ts.audience, err)
	}

	expiry := result.Status.ExpirationTimestamp.Time
	refreshPoint := time.Duration(float64(time.Until(expiry)) * refreshFraction)

	token := &oauth2.Token{
		AccessToken: result.Status.Token,
		TokenType:   "Bearer",
		Expiry:      expiry,
	}

	ts.token = token
	ts.refreshAt = time.Now().Add(refreshPoint)
	return token, nil
}
