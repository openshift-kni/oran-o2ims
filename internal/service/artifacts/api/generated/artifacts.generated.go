//go:build go1.22

// Package generated provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.4.1 DO NOT EDIT.
package generated

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/runtime"
	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	openapi_types "github.com/oapi-codegen/runtime/types"
	externalRef0 "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

// ManagedInfrastructureTemplate Information about a managed infrastructure template.
type ManagedInfrastructureTemplate struct {
	// ArtifactResourceId Identifier for the managed infrastructure template. This identifier is allocated by the O-Cloud.
	ArtifactResourceId openapi_types.UUID `json:"artifactResourceId"`

	// Description Details about the current managed infrastructure template.
	Description string `json:"description"`

	// Name Human readable description of managed infrastructure template
	Name string `json:"name"`

	// ParameterSchema Defines the parameters required for ClusterTemplate. The parameter definitions should follow the OpenAPI V3 schema and explicitly define required fields.
	ParameterSchema map[string]interface{} `json:"parameterSchema"`
	Version         string                 `json:"version"`
}

// ManagedInfrastructureTemplateId defines model for managedInfrastructureTemplateId.
type ManagedInfrastructureTemplateId = string

// GetManagedInfrastructureTemplatesParams defines parameters for GetManagedInfrastructureTemplates.
type GetManagedInfrastructureTemplatesParams struct {
	// ExcludeFields Comma separated list of field references to exclude from the result.
	//
	// Each field reference is a field name, or a sequence of field names separated by slashes. For
	// example, to exclude the `country` subfield of the `extensions` field:
	//
	// ```
	// exclude_fields=extensions/country
	// ```
	//
	// When this parameter isn't used no field will be excluded.
	//
	// Fields in this list will be excluded even if they are explicitly included using the
	// `fields` parameter.
	ExcludeFields *externalRef0.ExcludeFields `form:"exclude_fields,omitempty" json:"exclude_fields,omitempty"`

	// Fields Comma separated list of field references to include in the result.
	//
	// Each field reference is a field name, or a sequence of field names separated by slashes. For
	// example, to get the `name` field and the `country` subfield of the `extensions` field:
	//
	// ```
	// fields=name,extensions/country
	// ```
	//
	// When this parameter isn't used all the fields will be returned.
	Fields *externalRef0.Fields `form:"fields,omitempty" json:"fields,omitempty"`

	// Filter Search criteria.
	//
	// Contains one or more search criteria, separated by semicolons. Each search criteria is a
	// tuple containing an operator, a field reference and one or more values. The operator can
	// be any of the following strings:
	//
	// | Operator | Meaning                                                     |
	// |----------|-------------------------------------------------------------|
	// | `cont`   | Matches if the field contains the value                     |
	// | `eq`     | Matches if the field is equal to the value                  |
	// | `gt`     | Matches if the field is greater than the value              |
	// | `gte`    | Matches if the field is greater than or equal to the value  |
	// | `in`     | Matches if the field is one of the values                   |
	// | `lt`     | Matches if the field is less than the value                 |
	// | `lte`    | Matches if the field is less than or equal to the the value |
	// | `ncont`  | Matches if the field does not contain the value             |
	// | `neq`    | Matches if the field is not equal to the value              |
	// | `nin`    | Matches if the field is not one of the values               |
	//
	// The field reference is the name of one of the fields of the object, or a sequence of
	// name of fields separated by slashes. For example, to use the `country` sub-field inside
	// the `extensions` field:
	//
	// ```
	// filter=(eq,extensions/country,EQ)
	// ```
	//
	// The values are the arguments of the operator. For example, the `eq` operator compares
	// checks if the value of the field is equal to the value.
	//
	// The `in` and `nin` operators support multiple values. For example, to check if the `country`
	// sub-field inside the `extensions` field is either `ES` or `US:
	//
	// ```
	// filter=(in,extensions/country,ES,US)
	// ```
	//
	// When values contain commas, slashes or spaces they need to be surrounded by single quotes.
	// For example, to check if the `name` field is the string `my cluster`:
	//
	// ```
	// filter=(eq,name,'my cluster')
	// ```
	//
	// When multiple criteria separated by semicolons are used, all of them must match for the
	// complete condition to match. For example, the following will check if the `name` is
	// `my cluster` *and* the `country` extension is `ES`:
	//
	// ```
	// filter=(eq,name,'my cluster');(eq,extensions/country,ES)
	// ```
	//
	// When this parameter isn't used all the results will be returned.
	Filter *externalRef0.Filter `form:"filter,omitempty" json:"filter,omitempty"`
}

// GetManagedInfrastructureTemplateParams defines parameters for GetManagedInfrastructureTemplate.
type GetManagedInfrastructureTemplateParams struct {
	// ExcludeFields Comma separated list of field references to exclude from the result.
	//
	// Each field reference is a field name, or a sequence of field names separated by slashes. For
	// example, to exclude the `country` subfield of the `extensions` field:
	//
	// ```
	// exclude_fields=extensions/country
	// ```
	//
	// When this parameter isn't used no field will be excluded.
	//
	// Fields in this list will be excluded even if they are explicitly included using the
	// `fields` parameter.
	ExcludeFields *externalRef0.ExcludeFields `form:"exclude_fields,omitempty" json:"exclude_fields,omitempty"`

	// Fields Comma separated list of field references to include in the result.
	//
	// Each field reference is a field name, or a sequence of field names separated by slashes. For
	// example, to get the `name` field and the `country` subfield of the `extensions` field:
	//
	// ```
	// fields=name,extensions/country
	// ```
	//
	// When this parameter isn't used all the fields will be returned.
	Fields *externalRef0.Fields `form:"fields,omitempty" json:"fields,omitempty"`
}

// ServerInterface represents all server handlers.
type ServerInterface interface {
	// Get managed infrastructure templates
	// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates)
	GetManagedInfrastructureTemplates(w http.ResponseWriter, r *http.Request, params GetManagedInfrastructureTemplatesParams)
	// Get managed infrastructure templates
	// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates/{managedInfrastructureTemplateId})
	GetManagedInfrastructureTemplate(w http.ResponseWriter, r *http.Request, managedInfrastructureTemplateId ManagedInfrastructureTemplateId, params GetManagedInfrastructureTemplateParams)
}

// ServerInterfaceWrapper converts contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler            ServerInterface
	HandlerMiddlewares []MiddlewareFunc
	ErrorHandlerFunc   func(w http.ResponseWriter, r *http.Request, err error)
}

type MiddlewareFunc func(http.Handler) http.Handler

// GetManagedInfrastructureTemplates operation middleware
func (siw *ServerInterfaceWrapper) GetManagedInfrastructureTemplates(w http.ResponseWriter, r *http.Request) {

	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params GetManagedInfrastructureTemplatesParams

	// ------------- Optional query parameter "exclude_fields" -------------

	err = runtime.BindQueryParameter("form", true, false, "exclude_fields", r.URL.Query(), &params.ExcludeFields)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "exclude_fields", Err: err})
		return
	}

	// ------------- Optional query parameter "fields" -------------

	err = runtime.BindQueryParameter("form", true, false, "fields", r.URL.Query(), &params.Fields)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "fields", Err: err})
		return
	}

	// ------------- Optional query parameter "filter" -------------

	err = runtime.BindQueryParameter("form", true, false, "filter", r.URL.Query(), &params.Filter)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "filter", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetManagedInfrastructureTemplates(w, r, params)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// GetManagedInfrastructureTemplate operation middleware
func (siw *ServerInterfaceWrapper) GetManagedInfrastructureTemplate(w http.ResponseWriter, r *http.Request) {

	var err error

	// ------------- Path parameter "managedInfrastructureTemplateId" -------------
	var managedInfrastructureTemplateId ManagedInfrastructureTemplateId

	err = runtime.BindStyledParameterWithOptions("simple", "managedInfrastructureTemplateId", r.PathValue("managedInfrastructureTemplateId"), &managedInfrastructureTemplateId, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "managedInfrastructureTemplateId", Err: err})
		return
	}

	// Parameter object where we will unmarshal all parameters from the context
	var params GetManagedInfrastructureTemplateParams

	// ------------- Optional query parameter "exclude_fields" -------------

	err = runtime.BindQueryParameter("form", true, false, "exclude_fields", r.URL.Query(), &params.ExcludeFields)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "exclude_fields", Err: err})
		return
	}

	// ------------- Optional query parameter "fields" -------------

	err = runtime.BindQueryParameter("form", true, false, "fields", r.URL.Query(), &params.Fields)
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "fields", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetManagedInfrastructureTemplate(w, r, managedInfrastructureTemplateId, params)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

type UnescapedCookieParamError struct {
	ParamName string
	Err       error
}

func (e *UnescapedCookieParamError) Error() string {
	return fmt.Sprintf("error unescaping cookie parameter '%s'", e.ParamName)
}

func (e *UnescapedCookieParamError) Unwrap() error {
	return e.Err
}

type UnmarshalingParamError struct {
	ParamName string
	Err       error
}

func (e *UnmarshalingParamError) Error() string {
	return fmt.Sprintf("Error unmarshaling parameter %s as JSON: %s", e.ParamName, e.Err.Error())
}

func (e *UnmarshalingParamError) Unwrap() error {
	return e.Err
}

type RequiredParamError struct {
	ParamName string
}

func (e *RequiredParamError) Error() string {
	return fmt.Sprintf("Query argument %s is required, but not found", e.ParamName)
}

type RequiredHeaderError struct {
	ParamName string
	Err       error
}

func (e *RequiredHeaderError) Error() string {
	return fmt.Sprintf("Header parameter %s is required, but not found", e.ParamName)
}

func (e *RequiredHeaderError) Unwrap() error {
	return e.Err
}

type InvalidParamFormatError struct {
	ParamName string
	Err       error
}

func (e *InvalidParamFormatError) Error() string {
	return fmt.Sprintf("Invalid format for parameter %s: %s", e.ParamName, e.Err.Error())
}

func (e *InvalidParamFormatError) Unwrap() error {
	return e.Err
}

type TooManyValuesForParamError struct {
	ParamName string
	Count     int
}

func (e *TooManyValuesForParamError) Error() string {
	return fmt.Sprintf("Expected one value for %s, got %d", e.ParamName, e.Count)
}

// Handler creates http.Handler with routing matching OpenAPI spec.
func Handler(si ServerInterface) http.Handler {
	return HandlerWithOptions(si, StdHTTPServerOptions{})
}

// ServeMux is an abstraction of http.ServeMux.
type ServeMux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type StdHTTPServerOptions struct {
	BaseURL          string
	BaseRouter       ServeMux
	Middlewares      []MiddlewareFunc
	ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

// HandlerFromMux creates http.Handler with routing matching OpenAPI spec based on the provided mux.
func HandlerFromMux(si ServerInterface, m ServeMux) http.Handler {
	return HandlerWithOptions(si, StdHTTPServerOptions{
		BaseRouter: m,
	})
}

func HandlerFromMuxWithBaseURL(si ServerInterface, m ServeMux, baseURL string) http.Handler {
	return HandlerWithOptions(si, StdHTTPServerOptions{
		BaseURL:    baseURL,
		BaseRouter: m,
	})
}

// HandlerWithOptions creates http.Handler with additional options
func HandlerWithOptions(si ServerInterface, options StdHTTPServerOptions) http.Handler {
	m := options.BaseRouter

	if m == nil {
		m = http.NewServeMux()
	}
	if options.ErrorHandlerFunc == nil {
		options.ErrorHandlerFunc = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}

	wrapper := ServerInterfaceWrapper{
		Handler:            si,
		HandlerMiddlewares: options.Middlewares,
		ErrorHandlerFunc:   options.ErrorHandlerFunc,
	}

	m.HandleFunc("GET "+options.BaseURL+"/o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates", wrapper.GetManagedInfrastructureTemplates)
	m.HandleFunc("GET "+options.BaseURL+"/o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates/{managedInfrastructureTemplateId}", wrapper.GetManagedInfrastructureTemplate)

	return m
}

type GetManagedInfrastructureTemplatesRequestObject struct {
	Params GetManagedInfrastructureTemplatesParams
}

type GetManagedInfrastructureTemplatesResponseObject interface {
	VisitGetManagedInfrastructureTemplatesResponse(w http.ResponseWriter) error
}

type GetManagedInfrastructureTemplates200JSONResponse []ManagedInfrastructureTemplate

func (response GetManagedInfrastructureTemplates200JSONResponse) VisitGetManagedInfrastructureTemplatesResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetManagedInfrastructureTemplates400ApplicationProblemPlusJSONResponse externalRef0.ProblemDetails

func (response GetManagedInfrastructureTemplates400ApplicationProblemPlusJSONResponse) VisitGetManagedInfrastructureTemplatesResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(400)

	return json.NewEncoder(w).Encode(response)
}

type GetManagedInfrastructureTemplates500ApplicationProblemPlusJSONResponse externalRef0.ProblemDetails

func (response GetManagedInfrastructureTemplates500ApplicationProblemPlusJSONResponse) VisitGetManagedInfrastructureTemplatesResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(500)

	return json.NewEncoder(w).Encode(response)
}

type GetManagedInfrastructureTemplateRequestObject struct {
	ManagedInfrastructureTemplateId ManagedInfrastructureTemplateId `json:"managedInfrastructureTemplateId"`
	Params                          GetManagedInfrastructureTemplateParams
}

type GetManagedInfrastructureTemplateResponseObject interface {
	VisitGetManagedInfrastructureTemplateResponse(w http.ResponseWriter) error
}

type GetManagedInfrastructureTemplate200JSONResponse ManagedInfrastructureTemplate

func (response GetManagedInfrastructureTemplate200JSONResponse) VisitGetManagedInfrastructureTemplateResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetManagedInfrastructureTemplate400ApplicationProblemPlusJSONResponse externalRef0.ProblemDetails

func (response GetManagedInfrastructureTemplate400ApplicationProblemPlusJSONResponse) VisitGetManagedInfrastructureTemplateResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(400)

	return json.NewEncoder(w).Encode(response)
}

type GetManagedInfrastructureTemplate404ApplicationProblemPlusJSONResponse externalRef0.ProblemDetails

func (response GetManagedInfrastructureTemplate404ApplicationProblemPlusJSONResponse) VisitGetManagedInfrastructureTemplateResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(404)

	return json.NewEncoder(w).Encode(response)
}

type GetManagedInfrastructureTemplate500ApplicationProblemPlusJSONResponse externalRef0.ProblemDetails

func (response GetManagedInfrastructureTemplate500ApplicationProblemPlusJSONResponse) VisitGetManagedInfrastructureTemplateResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(500)

	return json.NewEncoder(w).Encode(response)
}

// StrictServerInterface represents all server handlers.
type StrictServerInterface interface {
	// Get managed infrastructure templates
	// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates)
	GetManagedInfrastructureTemplates(ctx context.Context, request GetManagedInfrastructureTemplatesRequestObject) (GetManagedInfrastructureTemplatesResponseObject, error)
	// Get managed infrastructure templates
	// (GET /o2ims-infrastructureArtifacts/v1/managedInfrastructureTemplates/{managedInfrastructureTemplateId})
	GetManagedInfrastructureTemplate(ctx context.Context, request GetManagedInfrastructureTemplateRequestObject) (GetManagedInfrastructureTemplateResponseObject, error)
}

type StrictHandlerFunc = strictnethttp.StrictHTTPHandlerFunc
type StrictMiddlewareFunc = strictnethttp.StrictHTTPMiddlewareFunc

type StrictHTTPServerOptions struct {
	RequestErrorHandlerFunc  func(w http.ResponseWriter, r *http.Request, err error)
	ResponseErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

func NewStrictHandler(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc) ServerInterface {
	return &strictHandler{ssi: ssi, middlewares: middlewares, options: StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		},
	}}
}

func NewStrictHandlerWithOptions(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc, options StrictHTTPServerOptions) ServerInterface {
	return &strictHandler{ssi: ssi, middlewares: middlewares, options: options}
}

type strictHandler struct {
	ssi         StrictServerInterface
	middlewares []StrictMiddlewareFunc
	options     StrictHTTPServerOptions
}

// GetManagedInfrastructureTemplates operation middleware
func (sh *strictHandler) GetManagedInfrastructureTemplates(w http.ResponseWriter, r *http.Request, params GetManagedInfrastructureTemplatesParams) {
	var request GetManagedInfrastructureTemplatesRequestObject

	request.Params = params

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.GetManagedInfrastructureTemplates(ctx, request.(GetManagedInfrastructureTemplatesRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetManagedInfrastructureTemplates")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(GetManagedInfrastructureTemplatesResponseObject); ok {
		if err := validResponse.VisitGetManagedInfrastructureTemplatesResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// GetManagedInfrastructureTemplate operation middleware
func (sh *strictHandler) GetManagedInfrastructureTemplate(w http.ResponseWriter, r *http.Request, managedInfrastructureTemplateId ManagedInfrastructureTemplateId, params GetManagedInfrastructureTemplateParams) {
	var request GetManagedInfrastructureTemplateRequestObject

	request.ManagedInfrastructureTemplateId = managedInfrastructureTemplateId
	request.Params = params

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.GetManagedInfrastructureTemplate(ctx, request.(GetManagedInfrastructureTemplateRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetManagedInfrastructureTemplate")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(GetManagedInfrastructureTemplateResponseObject); ok {
		if err := validResponse.VisitGetManagedInfrastructureTemplateResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
}

// GetSwagger returns the content of the embedded swagger specification file
// or error if failed to decode
func decodeSpec() ([]byte, error) {
	zipped, err := base64.StdEncoding.DecodeString(strings.Join(swaggerSpec, ""))
	if err != nil {
		return nil, fmt.Errorf("error base64 decoding spec: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}

	return buf.Bytes(), nil
}

var rawSpec = decodeSpecCached()

// a naive cached of a decoded swagger spec
func decodeSpecCached() func() ([]byte, error) {
	data, err := decodeSpec()
	return func() ([]byte, error) {
		return data, err
	}
}

// Constructs a synthetic filesystem for resolving external references when loading openapi specifications.
func PathToRawSpec(pathToFile string) map[string]func() ([]byte, error) {
	res := make(map[string]func() ([]byte, error))
	if len(pathToFile) > 0 {
		res[pathToFile] = rawSpec
	}

	for rawPath, rawFunc := range externalRef0.PathToRawSpec(path.Join(path.Dir(pathToFile), "../../common/api/openapi.yaml")) {
		if _, ok := res[rawPath]; ok {
			// it is not possible to compare functions in golang, so always overwrite the old value
		}
		res[rawPath] = rawFunc
	}
	return res
}

// GetSwagger returns the Swagger specification corresponding to the generated code
// in this file. The external references of Swagger specification are resolved.
// The logic of resolving external references is tightly connected to "import-mapping" feature.
// Externally referenced files must be embedded in the corresponding golang packages.
// Urls can be supported but this task was out of the scope.
func GetSwagger() (swagger *openapi3.T, err error) {
	resolvePath := PathToRawSpec("")

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, url *url.URL) ([]byte, error) {
		pathToFile := url.String()
		pathToFile = path.Clean(pathToFile)
		getSpec, ok := resolvePath[pathToFile]
		if !ok {
			err1 := fmt.Errorf("path not found: %s", pathToFile)
			return nil, err1
		}
		return getSpec()
	}
	var specData []byte
	specData, err = rawSpec()
	if err != nil {
		return
	}
	swagger, err = loader.LoadFromData(specData)
	if err != nil {
		return
	}
	return
}
