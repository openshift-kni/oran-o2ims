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

package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/openshift-kni/oran-o2ims/internal/logging"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Client builder contains the data and logic needed to create a Kubernetes API client that
// implements the controller-runtime Client interface. Don't create instances of this type
// directly, use the NewClient function instead.
type ClientBuilder struct {
	logger         *slog.Logger
	kubeconfig     any
	wrappers       []func(http.RoundTripper) http.RoundTripper
	loggingWrapper func(http.RoundTripper) http.RoundTripper
	flags          *pflag.FlagSet
}

// Client is an implementtion of the controller-runtime WithWatch interface with additional
// functionality, like the capability to connect using an SSH tunnel.

type Client struct {
	logger   *slog.Logger
	delegate clnt.WithWatch
}

// set fake client to be used for test.go
func NewFakeClient() *Client {
	client := Client{}
	client.delegate = fake.NewFakeClient()
	return &client
}

// NewClient creates a builder that can then be used to configure and create a Kubernetes API client
// that implements the controller-runtime WithWatch interface.
func NewClient() *ClientBuilder {
	return &ClientBuilder{}
}

// SetLogger sets the logger that the client will use to write to the log.
func (b *ClientBuilder) SetLogger(value *slog.Logger) *ClientBuilder {
	b.logger = value
	return b
}

// AddWrapper adds a function that will be called to wrap the HTTP transport. When multiple wrappers
// are added they will be called in the the reverse order, so that the request processing logic of
// those wrappers will be executed in the right order. For example, example if you want to add a
// wrapper that adds a `X-My` to the request header, and then another wrapper that reads that header
// you should add them in this order:
//
//	client, err := NewClient().
//		SetLogger(logger).
//		AddWrapper(addMyHeader).
//		AddWrapper(readMyHeader).
//		Build()
//	if err != nil {
//		...
//	}
//
// The opposite happens with response processing logic: it happens in the same order that the
// wrappers were added.
//
// The logging wrapper should not be added with this method, but with the SetLoggingWrapper methods,
// otherwise a default logging wrapper will be automatically added.
func (b *ClientBuilder) AddWrapper(value func(http.RoundTripper) http.RoundTripper) *ClientBuilder {
	b.wrappers = append(b.wrappers, value)
	return b
}

// SetLoggingWrapper sets the logging transport wrapper. If this isn't set then a default one will
// be created. Note that this wrapper, either the one explicitly set or the default, will always be
// the last to process requests and the first to process responses.
func (b *ClientBuilder) SetLoggingWrapper(
	value func(http.RoundTripper) http.RoundTripper) *ClientBuilder {
	b.loggingWrapper = value
	return b
}

// SetKubeconfig sets the bytes of the kubeconfig file that will be used to create the client. The
// value can be an array of bytes containing the configuration data or a string containing the name
// of a file. This is optional, and if not specified then the configuration will be loaded from the
// typical default locations: the `~/.kube/config` file, the `KUBECONFIG` environment variable, etc.
func (b *ClientBuilder) SetKubeconfig(value any) *ClientBuilder {
	b.kubeconfig = value
	return b
}

// SetFlags sets the command line flags that should be used to configure the client. This is
// optional.
func (b *ClientBuilder) SetFlags(flags *pflag.FlagSet) *ClientBuilder {
	b.flags = flags
	return b
}

// Build uses the data stored in the builder to configure and create a new Kubernetes API client.
func (b *ClientBuilder) Build() (result *Client, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	switch b.kubeconfig.(type) {
	case nil, []byte, string:
	default:
		err = fmt.Errorf(
			"kubeconfig must nil, an array of bytes or a file name, but it is of type %T",
			b.kubeconfig,
		)
		return
	}

	// Load the configuration:
	config, err := b.loadConfig()
	if err != nil {
		return
	}

	// Create the client:
	delegate, err := clnt.NewWithWatch(config, clnt.Options{})
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &Client{
		logger:   b.logger,
		delegate: delegate,
	}

	return
}

func (b *ClientBuilder) loadConfig() (result *rest.Config, err error) {
	// Load the configuration:
	var clientCfg clientcmd.ClientConfig
	if b.kubeconfig != nil {
		clientCfg, err = b.loadExplicitConfig()
	} else {
		clientCfg, err = b.loadDefaultConfig()
	}
	if err != nil {
		return
	}
	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return
	}

	// Add the logging wrapper:
	loggingWrapper := b.loggingWrapper
	if loggingWrapper == nil {
		loggingWrapper, err = logging.NewTransportWrapper().
			SetLogger(b.logger).
			SetFlags(b.flags).
			AddExclude("^/api(/[^/]+)?$").
			AddExclude("^/apis(/[^/]+/[^/]+)?$").
			Build()
		if err != nil {
			return
		}
	}
	restCfg.Wrap(loggingWrapper)

	// Add the transport wrappers in reverse order, so that the request processing logic will
	// happen in the right order:
	for i := len(b.wrappers) - 1; i >= 0; i-- {
		restCfg.Wrap(b.wrappers[i])
	}

	// Return the resulting REST config:
	result = restCfg
	return
}

// loadDefaultConfig loads the configuration from the typical default locations, the `KUBECONFIG`
// environment variable and the ~/.kube/config` file.
func (b *ClientBuilder) loadDefaultConfig() (result clientcmd.ClientConfig, err error) { // nolint: unparam
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	result = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	return
}

// loadExplicitConfig loads the configuration from the kubeconfig data set explicitly in the
// builder.
func (b *ClientBuilder) loadExplicitConfig() (result clientcmd.ClientConfig, err error) {
	switch typed := b.kubeconfig.(type) {
	case []byte:
		result, err = clientcmd.NewClientConfigFromBytes(typed)
	case string:
		var kcData []byte
		kcData, err = os.ReadFile(typed)
		if err != nil {
			return
		}
		result, err = clientcmd.NewClientConfigFromBytes(kcData)
	default:
		err = fmt.Errorf(
			"kubeconfig must be an array of bytes or a file name, but it is of type %T",
			b.kubeconfig,
		)
	}
	return
}

// Make sure that we implement the controller-runtime interface:
var _ clnt.Client = (*Client)(nil)

func (c *Client) Get(ctx context.Context, key types.NamespacedName, obj clnt.Object,
	opts ...clnt.GetOption) error {
	return c.delegate.Get(ctx, key, obj, opts...)
}

func (c *Client) List(ctx context.Context, list clnt.ObjectList,
	opts ...clnt.ListOption) error {
	return c.delegate.List(ctx, list, opts...)
}

func (c *Client) Create(ctx context.Context, obj clnt.Object, opts ...clnt.CreateOption) error {
	return c.delegate.Create(ctx, obj, opts...)
}

func (c *Client) Delete(ctx context.Context, obj clnt.Object, opts ...clnt.DeleteOption) error {
	return c.delegate.Delete(ctx, obj, opts...)
}

func (c *Client) DeleteAllOf(ctx context.Context, obj clnt.Object,
	opts ...clnt.DeleteAllOfOption) error {
	return c.delegate.DeleteAllOf(ctx, obj, opts...)
}

func (c *Client) Patch(ctx context.Context, obj clnt.Object, patch clnt.Patch,
	opts ...clnt.PatchOption) error {
	return c.delegate.Patch(ctx, obj, patch, opts...)
}

func (c *Client) Update(ctx context.Context, obj clnt.Object, opts ...clnt.UpdateOption) error {
	return c.delegate.Update(ctx, obj, opts...)
}

func (c *Client) Status() clnt.SubResourceWriter {
	return c.delegate.Status()
}

func (c *Client) SubResource(subResource string) clnt.SubResourceClient {
	return c.delegate.SubResource(subResource)
}

func (c *Client) RESTMapper() meta.RESTMapper {
	return c.delegate.RESTMapper()
}

func (c *Client) Scheme() *runtime.Scheme {
	return c.delegate.Scheme()
}

func (c *Client) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return c.delegate.GroupVersionKindFor(obj)
}

func (c *Client) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return c.delegate.IsObjectNamespaced(obj)
}

// watch
func (c *Client) Watch(ctx context.Context, obj clnt.ObjectList, opts ...clnt.ListOption) (watch.Interface, error) {
	return c.delegate.Watch(ctx, obj, opts...)
}

// Close closes the client and releases all the resources it is using. It is specially important to
// call this method when the client is using as SSH tunnel, as otherwise the tunnel will remain
// open.
func (c *Client) Close() error {
	return nil
}
