/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"k8s.io/apiserver/pkg/authentication/authenticator"
)

// From HAProxy
const (
	sslClientDERHeaderKey      = "x-ssl-client-der"
	sslChainDERHeaderKey       = "x-ssl-client-chain"
	sslClientVerifiedHeaderKey = "x-ssl-client-verify"
)

// From RFC9440.  We are always behind an HAProxy, and it doesn't yet support RFC9440 but some day it likely will so
// this provides a bit of future-proofing.
const (
	sslClientCertKey  = "Client-Cert"
	sslClientChainKey = "Client-Chain"
)

// Arbitrary key used to insert fingerprint data into the User Info object.  Authenticators are required to select keys
// that cannot be confused with those used by other authenticators.
const fingerprintKey = "o2ims.oran.openshift.io/fingerprint"

// getClientCertificate extracts the client certificate from the incoming request headers.  If none is found, an error
// is returned.
func getClientCertificate(req *http.Request) ([]byte, bool, error) {
	// TODO: Handle case of TLS termination directly on the server rather than at the ingress reverse proxy. However,
	// this is unlikely to become a required use case since we use the ingress proxy to route the request to the
	// correct microservice.  It is not possible to route the request without first terminating TLS since the connection
	// is encrypted and the final API endpoint is not known.

	// Check the standard value from RFC9440 first.
	certString := req.Header.Get(sslClientCertKey)
	if certString == "" {
		// If it doesn't exist, then check those used by HA-Proxy
		verified := req.Header.Get(sslClientVerifiedHeaderKey)
		if verified != "0" {
			// No cert was provided, or the cert provided could not be verified and was allowed
			return nil, false, nil
		}

		certString = req.Header.Get(sslClientDERHeaderKey)
		if certString == "" {
			// This is unexpected
			slog.Error("No client certificate found, but verification passed", "header", sslClientDERHeaderKey)
			return nil, false, nil
		}

		// Remove these so that they are not accidentally referenced or leaked elsewhere
		req.Header.Del(sslClientVerifiedHeaderKey)
		req.Header.Del(sslClientDERHeaderKey)
		req.Header.Del(sslChainDERHeaderKey)
	} else {
		// RFC9440 book-ends the certificate data with colons so we need to remove them before proceeding.
		certString = strings.TrimLeft(strings.TrimRight(certString, ":"), ":")

		// Remove these so that they are not accidentally referenced or leaked elsewhere
		req.Header.Del(sslClientCertKey)
		req.Header.Del(sslClientChainKey)
	}

	// Both HAProxy and RFC9440 use standard Base64 encoding rather than URL-encoded Base64.
	certBytes, err := base64.StdEncoding.DecodeString(certString)
	if err != nil {
		return nil, false, fmt.Errorf("error decoding client certificate: %w", err)
	}

	return certBytes, true, nil
}

// getClientCertificateFingerprint extracts the client certificate SHA256 fingerprint from the incoming request headers.
// If the headers do not include a client certificate or an error occurs while parsing the certificate, then an error is
// returned.
func getClientCertificateFingerprint(req *http.Request) (string, bool, error) {
	certBytes, present, err := getClientCertificate(req)
	if err != nil {
		return "", false, err
	}
	if !present {
		return "", false, nil
	}

	// Parse the decoded values to make sure we have a valid certificate rather than just a
	// spoofed set of bytes that result in the expected SHA256 checksum.  We trust the proxy
	// sitting ahead of us in all cases so that shouldn't be an issue, but just in case.
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return "", false, fmt.Errorf("error parsing client certificate: %w", err)
	}

	fingerprint := sha256.Sum256(cert.Raw)
	result := base64.URLEncoding.EncodeToString(fingerprint[:])
	// RFC8705 requires that superfluous whitespace and trailing equals signs be removed.
	result = strings.TrimSpace(strings.TrimRight(result, "="))
	return result, true, nil
}

type withClientVerification struct {
	authenticator authenticator.Request
}

// WithClientVerification wraps an existing token-based authenticator.Request so that further verification is performed
// on the incoming token to verify the ownership of the token.  It assumes that the wrapped authenticator.Request
// inserts User Info into the request context if it completes successfully.  The security considerations in RFC8705
// should be observed whenever using this feature.  The main point is that we are trusting the headers in the request
// to verify that a client certificate's fingerprint matches the one in the incoming token.  If the proxy in front of
// us is broken or untrustworthy, then it is possible for a client to set its own proxied header values.  This would
// allow it to fake possession/control of the same client certificate that was used to acquire the certificate and
// thereby circumvent this control.  We only have a single network deployment type.  We know that we are always behind
// an OpenShift Ingress Proxy so we trust that it behaves securely and would block any pre-existing certificate related
// headers from being passed through from a malicious client.
func WithClientVerification(request authenticator.Request) authenticator.Request {
	return &withClientVerification{authenticator: request}
}

func (w *withClientVerification) AuthenticateRequest(req *http.Request) (*authenticator.Response, bool, error) {
	response, ok, err := w.authenticator.AuthenticateRequest(req)
	if err != nil {
		return nil, false, err // nolint: wrapcheck
	}
	if !ok {
		return nil, false, nil
	}

	user := response.User
	tokenFingerprintValues, ok := user.GetExtra()[fingerprintKey]
	if !ok {
		// The authorization server did not add the expected fingerprint claim; therefore, this token wasn't bound to
		// the client certificate; hence we do not need to do any further validation.  We could theoretically build in
		// a configuration option that mandates that all tokens must have this claim, but the RFC doesn't mention that
		// as an expected behavior of the Resource Server so for now we'll just return here.  We can re-evaluate later.
		return response, true, nil
	}

	clientFingerprint, present, err := getClientCertificateFingerprint(req)
	if err != nil {
		slog.Error("error extracting client certificate fingerprint", "error", err)
		return nil, false, err
	}

	if !present {
		return nil, false, fmt.Errorf("a client certificate is required")
	}

	if len(tokenFingerprintValues) != 1 {
		slog.Error("unexpected number of fingerprint values", "values", tokenFingerprintValues)
		return nil, false, fmt.Errorf("unexpected number of fingerprint values")
	}

	if tokenFingerprintValues[0] != "" && tokenFingerprintValues[0] != clientFingerprint {
		slog.Debug("fingerprint values do not match", "client", clientFingerprint, "token", tokenFingerprintValues[0])
		return nil, false, fmt.Errorf("client certificate fingerprint mismatch")
	}

	return response, ok, nil
}
