/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package authentication

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v4"
	jsoniter "github.com/json-iterator/go"
	"github.com/spf13/pflag"
)

// HandlerWrapperBuilder contains the data and logic needed to create a wrapper that knows how to
// convert an HTTP handler into another one that also performs authentication using the JSON web
// token from the authorization header.
//
// The locations of the JSON web key sets used to verify the signatures of the tokens should be
// specified calling the AddKeysFile or AddKeysURL methods of the builder. If no JSON web key
// set location is specified then access will be granted to any client.
//
// Don't create instances of this object directly, use the NewHandlerWrapper function instead.
type HandlerWrapperBuilder struct {
	logger        *slog.Logger
	publicPaths   []string
	guestSubject  *Subject
	keysFiles     []string
	keysURLs      []string
	keysCA        *x509.CertPool
	keysCAFile    string
	keysInsecure  bool
	keysToken     string
	keysTokenFile string
	realm         string
	tolerance     time.Duration
}

type handlerWrapper struct {
	logger        *slog.Logger
	publicPaths   []*regexp.Regexp
	guestSubject  *Subject
	tokenParser   *jwt.Parser
	keysFiles     []string
	keysURLs      []string
	keysToken     string
	keysTokenFile string
	keysClient    *http.Client
	keys          *sync.Map
	lastKeyReload time.Time
	realm         string
	tolerance     time.Duration
	jsonAPI       jsoniter.API
}

type handlerObject struct {
	wrapper *handlerWrapper
	handler http.Handler
}

// NewHandlerWrapper creates a builder that can then be configured and used to create
// authentication handler wrappers. This wrapper is a function that transforms an HTTP
// handler into another that performs authentication using the JWT token in the authorization
// header.
func NewHandlerWrapper() *HandlerWrapperBuilder {
	return &HandlerWrapperBuilder{
		guestSubject: Guest,
		realm:        defaultRealm,
	}
}

// SetLogger sets the logger that the middleware will use to send messages to the log. This is
// mandatory.
func (b *HandlerWrapperBuilder) SetLogger(value *slog.Logger) *HandlerWrapperBuilder {
	b.logger = value
	return b
}

// AddPublicPath adds a regular expression that defines parts of the URL space that considered
// public, and therefore require no authentication. This method may be called multiple times and
// then all the given regular expressions will be used to check what parts of the URL space are public.
func (b *HandlerWrapperBuilder) AddPublicPath(value string) *HandlerWrapperBuilder {
	b.publicPaths = append(b.publicPaths, value)
	return b
}

// SetGuestSubject sets the subject that will be added to the context for public parts of the URL
// space if no authentication details are provided in the request. The default is to use the
// built-in guest subject, which has only one 'guest' identity.
func (b *HandlerWrapperBuilder) SetGuestSubject(value *Subject) *HandlerWrapperBuilder {
	b.guestSubject = value
	return b
}

// AddKeysFile adds a file containing a JSON web key set that will be used to verify the signatures
// of the tokens. The keys from this file will be loaded when a token is received containing an
// unknown key identifier.
//
// If no keys file or URL are provided then all requests will be accepted and the guest subject
// Will be added to the context.
func (b *HandlerWrapperBuilder) AddKeysFile(value string) *HandlerWrapperBuilder {
	if value != "" {
		b.keysFiles = append(b.keysFiles, value)
	}
	return b
}

// AddKeysURL sets the URL containing a JSON web key set that will be used to verify the signatures
// of the tokens. The keys from these URLs will be loaded when a token is received containing an
// unknown key identifier.
//
// If no keys file or URL are provided then all requests will be accepted and the guest subject
// Will be added to the context.
func (b *HandlerWrapperBuilder) AddKeysURL(value string) *HandlerWrapperBuilder {
	if value != "" {
		b.keysURLs = append(b.keysURLs, value)
	}
	return b
}

// SetKeysCA sets the certificate authorities that will be trusted when verifying the certificate
// of the web server where keys are loaded from.
func (b *HandlerWrapperBuilder) SetKeysCA(value *x509.CertPool) *HandlerWrapperBuilder {
	b.keysCA = value
	return b
}

// SetKeysCAFile sets the file containing the certificates of the certificate authorities
// that will be trusted when verifying the certificate of the web server where keys are loaded
// from.
func (b *HandlerWrapperBuilder) SetKeysCAFile(value string) *HandlerWrapperBuilder {
	b.keysCAFile = value
	return b
}

// SetKeysInsecure sets the flag that indicates that the certificate of the web server where the
// keys are loaded from should not be checked. The default is false and changing it to true makes
// the token verification insecure, so refrain from doing that in security sensitive environments.
func (b *HandlerWrapperBuilder) SetKeysInsecure(value bool) *HandlerWrapperBuilder {
	b.keysInsecure = value
	return b
}

// SetKeysToken sets the bearer token that will be used in the HTTP requests to download JSON web
// key sets. This is optional, by default no token is used.
func (b *HandlerWrapperBuilder) SetKeysToken(value string) *HandlerWrapperBuilder {
	b.keysToken = value
	return b
}

// SetKeysTokenFile sets the name of the file containing the bearer token that will be used in the
// HTTP requests to download JSON web key sets.
//
// This is intended for use when running inside a Kubernetes cluster and using service account
// tokens for authentication. In that case it is convenient to set this to the following value:
//
//	/run/secrets/kubernetes.io/serviceaccount/token
//
// Kubernetes writes in that file the token of the service account of the pod. That token grants
// access to the following JSON web key set URL, which should be set using the AddKeysURL method:
//
//	https://kubernetes/openid/v1/jwks
//
// This is optional, by default no token file is used.
func (b *HandlerWrapperBuilder) SetKeysTokenFile(value string) *HandlerWrapperBuilder {
	b.keysTokenFile = value
	return b
}

// SetRealm sets the realm that will be returned in the WWW-Authenticate request when authentication
// fails. This optional and the default value is O2IMS.
func (b *HandlerWrapperBuilder) SetRealm(value string) *HandlerWrapperBuilder {
	b.realm = value
	return b
}

// SetTolerance sets the maximum time that a token will be considered valid after it has expired.
// For example, to accept requests with tokens that have expired up to five minutes ago:
//
//	wrapper, err := authentication.NewHandler().
//		SetLogger(logger).
//		SetKeysURL("https://...").
//		SetTolerance(5 * time.Minute).
//		Build()
//	if err != nil {
//		...
//	}
//
// The default value is zero tolerance.
func (b *HandlerWrapperBuilder) SetTolerance(value time.Duration) *HandlerWrapperBuilder {
	b.tolerance = value
	return b
}

// SetFlags sets the command line flags that should be used to configure the wrapper. This is
// optional.
func (b *HandlerWrapperBuilder) SetFlags(flags *pflag.FlagSet) *HandlerWrapperBuilder {
	if flags != nil {
		if flags.Changed(jwksFileFlagName) {
			values, err := flags.GetStringArray(jwksFileFlagName)
			if err == nil {
				for _, value := range values {
					b.AddKeysFile(value)
				}
			}
		}
		if flags.Changed(jwksURLFlagName) {
			values, err := flags.GetStringArray(jwksURLFlagName)
			if err == nil {
				for _, value := range values {
					b.AddKeysURL(value)
				}
			}
		}
		if flags.Changed(jwksTokenFlagName) {
			value, err := flags.GetString(jwksTokenFlagName)
			if err == nil {
				b.SetKeysToken(value)
			}
		}
		if flags.Changed(jwksTokenFileFlagName) {
			value, err := flags.GetString(jwksTokenFileFlagName)
			if err == nil {
				b.SetKeysTokenFile(value)
			}
		}
		if flags.Changed(jwksCAFileFlagName) {
			value, err := flags.GetString(jwksCAFileFlagName)
			if err == nil {
				b.SetKeysCAFile(value)
			}
		}
	}
	return b
}

// Build uses the data stored in the builder to create a new authentication handler.
func (b *HandlerWrapperBuilder) Build() (result func(http.Handler) http.Handler, err error) {
	// Check parameters:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}
	if b.guestSubject == nil {
		err = fmt.Errorf("guest subject is mandatory")
		return
	}
	if b.realm == "" {
		err = fmt.Errorf("realm is mandatory")
		return
	}
	if b.tolerance < 0 {
		err = fmt.Errorf("tolerance must be zero or positive")
		return
	}

	// Check that all the configured keys files exist:
	for _, file := range b.keysFiles {
		var info os.FileInfo
		info, err = os.Stat(file)
		if err != nil {
			err = fmt.Errorf("keys file '%s' doesn't exist: %w", file, err)
			return
		}
		if !info.Mode().IsRegular() {
			err = fmt.Errorf("keys file '%s' isn't a regular file", file)
			return
		}
	}

	// Check that all the configured keys URLs are valid HTTPS URLs:
	for _, addr := range b.keysURLs {
		var parsed *url.URL
		parsed, err = url.Parse(addr)
		if err != nil {
			err = fmt.Errorf("keys URL '%s' isn't a valid URL: %w", addr, err)
			return
		}
		if !strings.EqualFold(parsed.Scheme, "https") {
			err = fmt.Errorf(
				"keys URL '%s' doesn't use the HTTPS protocol: %w",
				addr, err,
			)
			return
		}
	}

	// Load the keys CA file:
	var keysCA *x509.CertPool
	if b.keysCA != nil {
		keysCA = b.keysCA
	} else {
		keysCA, err = x509.SystemCertPool()
		if err != nil {
			return
		}
	}
	if b.keysCAFile != "" {
		var data []byte
		data, err = os.ReadFile(b.keysCAFile)
		if err != nil {
			err = fmt.Errorf("failed to read CA file '%s': %w", b.keysCAFile, err)
			return
		}
		keysCA = keysCA.Clone()
		ok := keysCA.AppendCertsFromPEM(data)
		if !ok {
			b.logger.Warn(
				"No certificate was loaded from CA file",
				slog.String("file", b.keysCAFile),
			)
		}
	}

	// Create the HTTP client that will be used to load the keys:
	keysClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            keysCA,
				InsecureSkipVerify: b.keysInsecure,
			},
		},
	}

	// Try to compile the regular expressions that define the parts of the URL space that are
	// publicPaths:
	publicPaths := make([]*regexp.Regexp, len(b.publicPaths))
	for i, expr := range b.publicPaths {
		publicPaths[i], err = regexp.Compile(expr)
		if err != nil {
			return
		}
	}

	// Create the bearer token parser:
	tokenParser := &jwt.Parser{}

	// Make copies of the lists of keys files and URLs:
	keysFiles := make([]string, len(b.keysFiles))
	copy(keysFiles, b.keysFiles)
	keysURLs := make([]string, len(b.keysURLs))
	copy(keysURLs, b.keysURLs)

	// Create the initial empty map of keys:
	keys := &sync.Map{}

	// Create the JSON API:
	jsonConfig := jsoniter.Config{
		IndentionStep: 2,
	}
	jsonAPI := jsonConfig.Froze()

	// Create and populate the object:
	wrapper := &handlerWrapper{
		logger:        b.logger,
		guestSubject:  b.guestSubject,
		publicPaths:   publicPaths,
		tokenParser:   tokenParser,
		keysFiles:     keysFiles,
		keysURLs:      keysURLs,
		keysToken:     b.keysToken,
		keysTokenFile: b.keysTokenFile,
		keysClient:    keysClient,
		keys:          keys,
		realm:         b.realm,
		tolerance:     b.tolerance,
		jsonAPI:       jsonAPI,
	}
	result = wrapper.wrap

	return
}

func (h *handlerWrapper) wrap(handler http.Handler) http.Handler {
	return &handlerObject{
		wrapper: h,
		handler: handler,
	}
}

func (h *handlerWrapper) serve(handler http.Handler, w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		h.serveWithAuth(handler, w, r, auth)
	} else {
		h.serveWithoutAuth(handler, w, r)
	}
}

func (h *handlerWrapper) serveWithAuth(handler http.Handler,
	w http.ResponseWriter, r *http.Request, auth string) {
	// Get the context:
	ctx := r.Context()

	// Check the authorization header:
	token, err := h.checkAuthorization(ctx, auth)
	if err != nil {
		h.sendError(w, r, err)
		return
	}

	// Add the subject to the context:
	claims := token.Claims.(jwt.MapClaims)
	name := claims["sub"].(string)
	subject := &Subject{
		Token:  token.Raw,
		Name:   name,
		Claims: claims,
	}
	ctx = ContextWithSubject(ctx, subject)
	r = r.WithContext(ctx)

	// Call the wrapped handler:
	handler.ServeHTTP(w, r)
}

func (h *handlerWrapper) serveWithoutAuth(handler http.Handler,
	w http.ResponseWriter, r *http.Request) {
	// Get the context:
	ctx := r.Context()

	// Check if the request is for a public path:
	isPublicPath := false
	for _, publicPath := range h.publicPaths {
		if publicPath.MatchString(r.URL.Path) {
			isPublicPath = true
			break
		}
	}

	// Check if there are keys:
	haveKeys := len(h.keysFiles)+len(h.keysURLs) > 0

	// If the request is for a public path or there are no configured keys then we accept it
	// and pass it to the wrapped handler with the guest subject in the context:
	if isPublicPath || !haveKeys {
		ctx = ContextWithSubject(ctx, h.guestSubject)
		r = r.WithContext(ctx)
		handler.ServeHTTP(w, r)
		return
	}

	// If the request isn't for a public path then we reject it:
	err := errors.New("request doesn't contain the authorization header")
	h.sendError(w, r, err)
}

// checkAuthorization checks if the given authorization header is valid.
func (h *handlerWrapper) checkAuthorization(ctx context.Context,
	auth string) (token *jwt.Token, err error) {
	// Try to extract the bearer token from the authorization header:
	matches := bearerRE.FindStringSubmatch(auth)
	if len(matches) != 3 {
		err = fmt.Errorf("authorization header '%s' is malformed", auth)
		return
	}
	scheme := matches[1]
	if !strings.EqualFold(scheme, "Bearer") {
		err = fmt.Errorf("authentication type '%s' isn't supported", scheme)
		return
	}
	bearer := matches[2]

	// Use the JWT library to verify that the token is correctly signed and that the basic
	// claims are correct:
	token, err = h.checkToken(ctx, bearer)
	if err != nil {
		return
	}

	// The library that we use considers tokens valid if the claims that it checks don't exist,
	// but we want to reject those tokens, so we need to do some additional validations:
	err = h.checkClaims(ctx, token.Claims.(jwt.MapClaims))
	if err != nil {
		return
	}

	return
}

// selectKey selects the key that should be used to verify the given token.
func (h *handlerWrapper) selectKey(ctx context.Context, token *jwt.Token) (key any, err error) {
	// Get the key identifier:
	value, ok := token.Header["kid"]
	if !ok {
		err = fmt.Errorf("token doesn't have a 'kid' field in the header")
		return
	}
	kid, ok := value.(string)
	if !ok {
		err = fmt.Errorf(
			"token has a 'kid' field, but it is a %T instead of a string",
			value,
		)
		return
	}

	// Get the key for that key identifier. If there is no such key and we didn't reload keys
	// recently then we try to reload them now.
	key, ok = h.keys.Load(kid)
	if !ok && time.Since(h.lastKeyReload) > 1*time.Minute {
		err = h.loadKeys(ctx)
		if err != nil {
			return
		}
		h.lastKeyReload = time.Now()
		key, ok = h.keys.Load(kid)
	}
	if !ok {
		err = fmt.Errorf("there is no key for key identifier '%s'", kid)
		return
	}

	return
}

// keyData is the type used to read a single key from a JSON document.
type keyData struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// setData is the type used to read a collection of keys from a JSON document.
type setData struct {
	Keys []keyData `json:"keys"`
}

// loadKeys loads the JSON web key set from the URLs specified in the configuration.
func (h *handlerWrapper) loadKeys(ctx context.Context) error {
	// Load keys from the files given in the configuration:
	for _, keysFile := range h.keysFiles {
		h.logger.InfoContext(
			ctx,
			"Loading keys from file", keysFile,
			slog.String("file", keysFile),
		)
		err := h.loadKeysFile(ctx, keysFile)
		if err != nil {
			h.logger.ErrorContext(
				ctx,
				"Can't load keys from file",
				slog.String("file", keysFile),
				slog.String("error", err.Error()),
			)
		}
	}

	// Load keys from URLs given in the configuration:
	for _, keysURL := range h.keysURLs {
		h.logger.InfoContext(
			ctx,
			"Loading keys from URL",
			slog.String("url", keysURL),
		)
		err := h.loadKeysURL(ctx, keysURL)
		if err != nil {
			h.logger.ErrorContext(
				ctx,
				"Can't load keys from URL",
				slog.String("url", keysURL),
				slog.String("error", err.Error()),
			)
		}
	}

	return nil
}

// loadKeysFile loads a JSON web key set from a file.
func (h *handlerWrapper) loadKeysFile(ctx context.Context, file string) error {
	reader, err := os.Open(file)
	if err != nil {
		return err
	}
	return h.readKeys(ctx, reader)
}

// loadKeysURL loads a JSON we key set from an URL.
func (h *handlerWrapper) loadKeysURL(ctx context.Context, addr string) error {
	request, err := http.NewRequest(http.MethodGet, addr, nil)
	if err != nil {
		return err
	}
	token := h.selectKeysToken(ctx)
	if token != "" {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	request = request.WithContext(ctx)
	response, err := h.keysClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		err := response.Body.Close()
		if err != nil {
			h.logger.ErrorContext(
				ctx,
				"Can't close response body",
				slog.String("url", addr),
				slog.String("error", err.Error()),
			)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"request to load keys from '%s' failed with status code %d",
			addr, response.StatusCode,
		)
	}
	return h.readKeys(ctx, response.Body)
}

// selectKeysToken selects the token that should be for authentication to the server that contains
// the JSON web key set. Note that this will never return an error; if something fails it will
// report it in the log and will return an empty string.
func (h *handlerWrapper) selectKeysToken(ctx context.Context) string {
	// First try to read the token from the configured file:
	if h.keysTokenFile != "" {
		data, err := os.ReadFile(h.keysTokenFile)
		if err != nil {
			h.logger.ErrorContext(
				ctx,
				"Failed to read keys token from file",
				slog.String("file", h.keysTokenFile),
				slog.String("error", err.Error()),
			)
		} else {
			return string(data)
		}
	}

	// If there is no token file or something failed while reading it then return the
	// configured token (which may be empty):
	return h.keysToken
}

// readKeys reads the keys from JSON web key set available in the given reader.
func (h *handlerWrapper) readKeys(ctx context.Context, reader io.Reader) error {
	// Read the JSON data:
	jsonData, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	// Parse the JSON data:
	var setData setData
	err = json.Unmarshal(jsonData, &setData)
	if err != nil {
		return err
	}

	// Convert the key data to actual keys that can be used to verify the signatures of the
	// tokens:
	for _, keyData := range setData.Keys {
		h.logger.DebugContext(
			ctx,
			"Key data",
			slog.String("kid", keyData.Kid),
			slog.String("kty", keyData.Kty),
			slog.String("alg", keyData.Alg),
			slog.String("e", keyData.E),
			slog.String("n", keyData.N),
		)
		if keyData.Kid == "" {
			h.logger.ErrorContext(
				ctx,
				"Can't read key because 'kid' is empty",
			)
			continue
		}
		if keyData.Kty == "" {
			h.logger.ErrorContext(
				ctx,
				"Can't read key because 'kty' is empty",
				slog.String("kid", keyData.Kid),
			)
			continue
		}
		if keyData.Alg == "" {
			h.logger.ErrorContext(
				ctx,
				"Can't read key because 'alg' is empty",
				slog.String("kid", keyData.Kid),
			)
			continue
		}
		if keyData.E == "" {
			h.logger.ErrorContext(
				ctx,
				"Can't read key because 'e' is empty",
				slog.String("kid", keyData.Kid),
			)
			continue
		}
		if keyData.E == "" {
			h.logger.ErrorContext(
				ctx,
				"Can't read key because 'n' is empty",
				slog.String("kid", keyData.Kid),
			)
			continue
		}
		var key any
		key, err = h.parseKey(keyData)
		if err != nil {
			h.logger.ErrorContext(
				ctx,
				"Key will be ignored because it can't be parsed",
				slog.String("kid", keyData.Kid),
				slog.String("error", err.Error()),
			)
			continue
		}
		h.keys.Store(keyData.Kid, key)
		h.logger.InfoContext(
			ctx,
			"Loaded key",
			slog.String("kid", keyData.Kid),
		)
	}

	return nil
}

// parseKey converts the key data loaded from the JSON document to an actual key that can be used
// to verify the signatures of tokens.
func (h *handlerWrapper) parseKey(data keyData) (key any, err error) {
	// Check key type:
	if data.Kty != "RSA" {
		err = fmt.Errorf("key type '%s' isn't supported", data.Kty)
		return
	}

	// Decode the e and n values:
	nb, err := base64.RawURLEncoding.DecodeString(data.N)
	if err != nil {
		return
	}
	eb, err := base64.RawURLEncoding.DecodeString(data.E)
	if err != nil {
		return
	}

	// Create the key:
	key = &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: int(new(big.Int).SetBytes(eb).Int64()),
	}

	return
}

// checkToken checks if the token is valid. If it is valid it returns the parsed token,.
func (h *handlerWrapper) checkToken(ctx context.Context, bearer string) (token *jwt.Token,
	err error) {
	token, err = h.tokenParser.ParseWithClaims(
		bearer, jwt.MapClaims{},
		func(token *jwt.Token) (key any, err error) {
			return h.selectKey(ctx, token)
		},
	)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to parse token",
			slog.String("!token", bearer),
			slog.String("error", err.Error()),
		)
		switch typed := err.(type) {
		case *jwt.ValidationError:
			switch {
			case typed.Errors&jwt.ValidationErrorMalformed != 0:
				err = errors.New("bearer token is malformed")
			case typed.Errors&jwt.ValidationErrorUnverifiable != 0:
				err = errors.New("bearer token can't be verified")
			case typed.Errors&jwt.ValidationErrorSignatureInvalid != 0:
				err = errors.New("signature of bearer token isn't valid")
			case typed.Errors&jwt.ValidationErrorExpired != 0:
				// When the token is expired according to the JWT library we may
				// still want to accept it if we have a configured tolerance:
				if h.tolerance > 0 {
					var remaining time.Duration
					_, remaining, err = tokenRemaining(token, time.Now())
					if err != nil {
						h.logger.ErrorContext(
							ctx,
							"Failed to check token duration",
							slog.String("error", err.Error()),
						)
						remaining = 0
					}
					if -remaining > h.tolerance {
						err = errors.New("bearer token is expired")
					}
				} else {
					err = errors.New("bearer token is expired")
				}
			case typed.Errors&jwt.ValidationErrorIssuedAt != 0:
				err = errors.New("bearer token was issued in the future")
			case typed.Errors&jwt.ValidationErrorNotValidYet != 0:
				err = errors.New("bearer token isn't valid yet")
			default:
				err = errors.New("bearer token isn't valid")
			}
		default:
			err = errors.New("bearer token is malformed")
		}
	}
	return
}

// checkClaims checks that the required claims are present and that they have valid values.
func (h *handlerWrapper) checkClaims(ctx context.Context, claims jwt.MapClaims) error {
	// The `typ` claim is optional, but if it exists the value must be `Bearer`:
	value, ok := claims["typ"]
	if ok {
		typ, ok := value.(string)
		if !ok {
			return fmt.Errorf(
				"bearer token type claim contains incorrect string value '%v'",
				value,
			)
		}
		if !strings.EqualFold(typ, "Bearer") {
			return fmt.Errorf(
				"bearer token type '%s' isn't allowed",
				typ,
			)
		}
	}

	// Check the format of the `sub` claim:
	_, err := h.checkStringClaim(ctx, claims, "sub")
	if err != nil {
		return err
	}

	// Check the format of the issue and expiration date claims:
	_, err = h.checkTimeClaim(ctx, claims, "iat")
	if err != nil {
		return err
	}
	_, err = h.checkTimeClaim(ctx, claims, "exp")
	if err != nil {
		return err
	}

	return nil
}

// checkStringClaim checks that the given claim exists and that the value is a string. If it exist
// it returns the value.
func (h *handlerWrapper) checkStringClaim(ctx context.Context, claims jwt.MapClaims,
	name string) (result string, err error) {
	value, err := h.checkClaim(ctx, claims, name)
	if err != nil {
		return
	}
	text, ok := value.(string)
	if !ok {
		err = fmt.Errorf(
			"bearer token claim '%s' with value '%v' should be a string, but it is "+
				"of type '%T'",
			name, value, value,
		)
		return
	}
	result = text
	return
}

// checkTimeClaim checks that the given claim exists and that the value is a time. If it exists it
// returns the value.
func (h *handlerWrapper) checkTimeClaim(ctx context.Context, claims jwt.MapClaims,
	name string) (result time.Time, err error) {
	value, err := h.checkClaim(ctx, claims, name)
	if err != nil {
		return
	}
	seconds, ok := value.(float64)
	if !ok {
		err = fmt.Errorf(
			"bearer token claim '%s' contains incorrect time value '%v'",
			name, value,
		)
		return
	}
	result = time.Unix(int64(seconds), 0)
	return
}

// checkClaim checks that the given claim exists. If it exists it returns the value.
func (h *handlerWrapper) checkClaim(ctx context.Context, claims jwt.MapClaims,
	name string) (result any, err error) {
	value, ok := claims[name]
	if !ok {
		err = fmt.Errorf(
			"bearer token doesn't contain required claim '%s'",
			name,
		)
		return
	}
	result = value
	return
}

// sendError sends an error response to the client with the message of the given error.
func (h *handlerWrapper) sendError(w http.ResponseWriter, r *http.Request, err error) {
	// Get the context:
	ctx := r.Context()

	// Convert to upper case the first letter of the error message:
	detail := err.Error()
	first, length := utf8.DecodeRuneInString(detail)
	if first == utf8.RuneError {
		h.logger.ErrorContext(
			ctx,
			"Failed to get first rune of error message",
			slog.String("message", detail),
		)
	} else {
		first = unicode.ToUpper(first)
		detail = string(first) + detail[length:]
	}

	// Send the response:
	w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer realm=\"%s\"", h.realm))
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	writer := jsoniter.NewStream(h.jsonAPI, w, 512)
	writer.WriteObjectStart()
	writer.WriteObjectField("status")
	writer.WriteInt(http.StatusUnauthorized)
	writer.WriteMore()
	writer.WriteObjectField("detail")
	writer.WriteString(detail)
	writer.WriteObjectEnd()
	writer.Flush()
}

// ServeHTTP is the implementation of the http.Handler interface.
func (h *handlerObject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.wrapper.serve(h.handler, w, r)
}

// tokenRemaining determines if the given token will eventually expire (offile access tokens and
// opaque tokens, for example, never expire) and the time till it expires. That time will be
// positive if the token isn't expired, and negative if the token has already expired.
//
// For tokens that don't have the `exp` claim, or that have it with value zero (typical for offline
// access tokens) the result will always be `false` and zero.
func tokenRemaining(token *jwt.Token, now time.Time) (expires bool, duration time.Duration,
	err error) {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		err = fmt.Errorf("expected map claims but got %T", claims)
		return
	}
	var exp float64
	claim, ok := claims["exp"]
	if !ok {
		return
	}
	exp, ok = claim.(float64)
	if !ok {
		err = fmt.Errorf("expected floating point 'exp' but got %T", claim)
		return
	}
	if exp == 0 {
		return
	}
	duration = time.Unix(int64(exp), 0).Sub(now)
	expires = true
	return
}

// Regular expression used to extract the bearer token from the authorization header:
var bearerRE = regexp.MustCompile(`^([a-zA-Z0-9]+)\s+(.*)$`)

// Default realm:
const defaultRealm = "O2IMS"
