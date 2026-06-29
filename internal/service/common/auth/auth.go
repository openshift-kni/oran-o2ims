/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"

	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"

	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
)

var containerID string

func init() {
	containerID, _ = os.Hostname()
}

func clientIP(req *http.Request) string {
	if xff := req.Header.Get("x-forwarded-for"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return host
	}
	return req.RemoteAddr
}

func tokenClaimsAttrs(req *http.Request) []any {
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Subject  string `json:"sub"`
		Audience any    `json:"aud"`
		Exp      any    `json:"exp"`
		ClientID string `json:"client_id"`
		Azp      string `json:"azp"`
		Scope    string `json:"scope"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	var attrs []any
	if claims.Issuer != "" {
		attrs = append(attrs, slog.String("issuer", claims.Issuer))
	}
	if claims.Subject != "" {
		attrs = append(attrs, slog.String("subject", claims.Subject))
	}
	if claims.Audience != nil {
		attrs = append(attrs, slog.Any("audience", claims.Audience))
	}
	if claims.Exp != nil {
		attrs = append(attrs, slog.Any("expiration", claims.Exp))
	}
	if claims.ClientID != "" {
		attrs = append(attrs, slog.String("clientId", claims.ClientID))
	}
	if claims.Azp != "" {
		attrs = append(attrs, slog.String("authorizedParty", claims.Azp))
	}
	if claims.Scope != "" {
		attrs = append(attrs, slog.String("scope", claims.Scope))
	}
	return attrs
}

// Authenticator defines an authentication handler that is capable of authenticating a user from an incoming
// JWT bearer token.  The purpose of this step is to confirm the identity of the user making the request.  This
// could include any of username, userid, or group as well as any extra attributes associated with the request
// identity.  Authorization is performed in a later step.  The actual authentication step is delegated to the supplied
// request handler.  This could be any request handler (i.e., Kubernetes TokenReview, OIDC token validation, etc..)
func Authenticator(oauthHandler, kubernetesHandler authenticator.Request) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := logging.AppendCtx(req.Context(), slog.String("container", containerID))
			ctx = logging.AppendCtx(ctx, slog.String("clientIp", clientIP(req)))
			req = req.WithContext(ctx)

			value := req.Header.Get("x-forwarded-for")
			handler := oauthHandler
			if handler == nil || value == "" {
				// If the request did not pass through the ingress, then this is an internal request, or oauth isn't
				// enabled, then use the Kubernetes authenticator.
				handler = kubernetesHandler
			}

			response, ok, err := handler.AuthenticateRequest(req)
			if err != nil {
				args := []any{slog.Any("error", err), slog.String("method", req.Method), slog.String("path", req.URL.Path)}
				args = append(args, tokenClaimsAttrs(req)...)
				slog.WarnContext(req.Context(), "authentication failed", args...)
				middleware.ProblemDetails(w, fmt.Sprintf("failed to authenticate request: %v", err), http.StatusUnauthorized)
				return
			}

			if !ok {
				args := []any{slog.String("method", req.Method), slog.String("path", req.URL.Path)}
				args = append(args, tokenClaimsAttrs(req)...)
				slog.WarnContext(req.Context(), "authentication rejected", args...)
				middleware.ProblemDetails(w, "unable to authenticate request", http.StatusUnauthorized)
				return
			}

			// Load the user details into the context so that the Authorizers have access to it.
			req = req.WithContext(request.WithUser(req.Context(), response.User))

			// Proceed to the next layer of handler
			next.ServeHTTP(w, req)
		})
	}
}

// convertMethodToVerb converts an HTTP method name to a Kubernetes API verb.
func convertMethodToVerb(method string) string {
	switch method {
	case "GET":
		return "get"
	case "POST":
		return "create"
	case "PUT":
		return "update"
	case "DELETE":
		return "delete"
	case "PATCH":
		return "patch"
	}
	return method
}

// Authorizer defines an authorization handler that authorizes the request.  This must be executed
// after the Authenticator handler so that the requester's User Info is attached to the context.  If
// no User Info is present in the context, then an error will be returned.  The actual authorization
// step is delegated to the Kubernetes authorizer which performs a SubjectAccessReview.
func Authorizer(kubernetesAuthorizer authorizer.Authorizer) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := logging.AppendCtx(req.Context(), slog.String("container", containerID))
			ctx = logging.AppendCtx(ctx, slog.String("clientIp", clientIP(req)))
			req = req.WithContext(ctx)

			// Retrieve the User info from the request context.  It should have been placed there by a successful
			// invocation of the Authenticator handler.
			user, ok := request.UserFrom(req.Context())
			if !ok {
				middleware.ProblemDetails(w, "user not in context", http.StatusBadRequest)
				return
			}

			// Populate the minimum fields required by the Kubernetes handler
			attributes := authorizer.AttributesRecord{
				User: user,
				Verb: convertMethodToVerb(req.Method),
				Path: req.URL.Path,
			}

			decision, reason, err := kubernetesAuthorizer.Authorize(req.Context(), attributes)
			if err != nil {
				msg := fmt.Sprintf("Authorization for user '%s' failed", attributes.User.GetName())
				slog.ErrorContext(req.Context(), msg,
					slog.String("user", user.GetName()), slog.Any("groups", user.GetGroups()),
					slog.String("verb", attributes.Verb), slog.String("path", attributes.Path),
					slog.Any("error", err))
				middleware.ProblemDetails(w, msg, http.StatusInternalServerError)
				return
			}

			if decision != authorizer.DecisionAllow {
				msg := fmt.Sprintf("Authorization not allowed for user '%s'", attributes.User.GetName())
				slog.DebugContext(req.Context(), msg,
					slog.String("user", user.GetName()), slog.Any("groups", user.GetGroups()),
					slog.String("verb", attributes.Verb), slog.String("path", attributes.Path),
					slog.Any("decision", decision), slog.String("reason", reason))
				middleware.ProblemDetails(w, msg, http.StatusForbidden)
				return
			}

			slog.DebugContext(req.Context(), "authorization allowed",
				slog.String("user", user.GetName()), slog.Any("groups", user.GetGroups()),
				slog.String("verb", attributes.Verb), slog.String("path", attributes.Path))

			// Proceed to the next layer of handler
			next.ServeHTTP(w, req)
		})
	}
}
