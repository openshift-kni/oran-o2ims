package auth

import (
	"fmt"
	"log/slog"
	"net/http"

	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints/request"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/middleware"
)

// Authenticator defines an authentication handler that is capable of authenticating a user from an incoming
// JWT bearer token.  The purpose of this step is to confirm the identity of the user making the request.  This
// could include any of username, userid, or group as well as any extra attributes associated with the request
// identity.  Authorization is performed in a later step.  The actual authentication step is delegated to the supplied
// request handler.  This could be any request handler (i.e., Kubernetes TokenReview, OIDC token validation, etc..)
func Authenticator(oauthHandler, kubernetesHandler authenticator.Request) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			value := req.Header.Get("x-forwarded-for")
			handler := oauthHandler
			if handler == nil || value == "" {
				// If the request did not pass through the ingress, then this is an internal request, or oauth isn't
				// enabled, then use the Kubernetes authenticator.
				handler = kubernetesHandler
			}

			response, ok, err := handler.AuthenticateRequest(req)
			if err != nil {
				middleware.ProblemDetails(w, fmt.Sprintf("failed to authenticate request: %v", err), http.StatusUnauthorized)
				return
			}

			if !ok {
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
				slog.Error(msg, "user", user, "verb", attributes.Verb, "path", attributes.Path, "error", err)
				middleware.ProblemDetails(w, msg, http.StatusInternalServerError)
				return
			}

			if decision != authorizer.DecisionAllow {
				msg := fmt.Sprintf("Authorization not allowed for user '%s'", attributes.User.GetName())
				slog.Debug(msg, "user", user, "verb", attributes.Verb, "path", attributes.Path, "decision", decision, "reason", reason)
				middleware.ProblemDetails(w, msg, http.StatusForbidden)
				return
			}

			// Proceed to the next layer of handler
			next.ServeHTTP(w, req)
		})
	}
}
