/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package clients

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

// AuthorizationEditor implements a function type defined by the oapi-codegen tooling which intercepts an HTTP request
// and inserts a bearer token into the Authorization header.  This enables us to get a fresh token periodically to
// ensure that we handle our service account tokens being refreshed.
type AuthorizationEditor struct {
	Source oauth2.TokenSource
}

// Editor inserts a bearer token into the Authorization header.
func (s *AuthorizationEditor) Editor(_ context.Context, req *http.Request) error {
	t, err := s.Source.Token()
	if err != nil {
		return fmt.Errorf("failed to retrieve token: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.AccessToken))
	return nil
}
