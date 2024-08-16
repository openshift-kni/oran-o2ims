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

package network

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"slices"

	"github.com/spf13/pflag"
)

// ListenerBuilder contains the data and logic needed to create a network listener. Don't create
// instances of this object directly, use the NewListener function instead.
type ListenerBuilder struct {
	logger       *slog.Logger
	network      string
	address      string
	tlsCrt       string
	tlsKey       string
	tlsProtocols []string
}

// NewListener creates a builder that can then used to configure and create a network listener.
func NewListener() *ListenerBuilder {
	return &ListenerBuilder{
		network: "tcp",
	}
}

// SetLogger sets the logger that the listener will use to send messages to the log. This is
// mandatory.
func (b *ListenerBuilder) SetLogger(value *slog.Logger) *ListenerBuilder {
	b.logger = value
	return b
}

// SetFlags sets the command line flags that should be used to configure the listener.
//
// The name is used to select the options when there are multiple listeners. For example, if it
// is 'API' then it will only take into accounts the flags starting with '--api'.
//
// This is optional.
func (b *ListenerBuilder) SetFlags(flags *pflag.FlagSet, name string) *ListenerBuilder {
	if flags == nil {
		return b
	}

	var (
		flag  string
		value string
		err   error
	)
	failure := func() {
		b.logger.Error(
			"Failed to get flag value",
			slog.String("flag", "flag"),
			slog.String("error", err.Error()),
		)
	}

	// Address:
	flag = listenerFlagName(name, listenerAddrFlagSuffix)
	value, err = flags.GetString(flag)
	if err != nil {
		failure()
	} else {
		b.SetAddress(value)
	}

	// TLS certificate:
	flag = listenerFlagName(name, listenerTLSCrtFlagSuffix)
	value, err = flags.GetString(flag)
	if err != nil {
		failure()
	} else {
		b.SetTLSCrt(value)
	}

	// TLS key:
	flag = listenerFlagName(name, listenerTLSKeyFlagSuffix)
	value, err = flags.GetString(flag)
	if err != nil {
		failure()
	} else {
		b.SetTLSKey(value)
	}

	return b
}

// SetNetwork sets the network. This is optional and the default is TCP.
func (b *ListenerBuilder) SetNetwork(value string) *ListenerBuilder {
	b.network = value
	return b
}

// SetAddress sets the listen address. This is mandatory.
func (b *ListenerBuilder) SetAddress(value string) *ListenerBuilder {
	b.address = value
	return b
}

// SetTLSCrt sets the file that contains the certificate file, in PEM format.
func (b *ListenerBuilder) SetTLSCrt(value string) *ListenerBuilder {
	b.tlsCrt = value
	return b
}

// SetTLSKey sets the file that contains the key file, in PEM format.
func (b *ListenerBuilder) SetTLSKey(value string) *ListenerBuilder {
	b.tlsKey = value
	return b
}

// AddTLSProtocol adds a protocol that will be supported during the TLS negotiation.
func (b *ListenerBuilder) AddTLSProtocol(value string) *ListenerBuilder {
	b.tlsProtocols = append(b.tlsProtocols, value)
	return b
}

// Build uses the data stored in the builder to create a new network listener.
func (b *ListenerBuilder) Build() (result net.Listener, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.network == "" {
		err = errors.New("network is mandatory")
		return
	}
	if b.address == "" {
		err = errors.New("address is mandatory")
		return
	}
	if b.tlsCrt != "" && b.tlsKey == "" {
		err = errors.New("TLS key is mandatory when certificate is specified")
		return
	}
	if b.tlsKey != "" && b.tlsCrt == "" {
		err = errors.New("TLS certificate is mandatory when key is specified")
		return
	}

	// Try to load the certificates
	var tlsCrt tls.Certificate
	if b.tlsCrt != "" && b.tlsKey != "" {
		tlsCrt, err = tls.LoadX509KeyPair(b.tlsCrt, b.tlsKey)
		if err != nil {
			return
		}
		b.logger.Info(
			"Loaded TLS key and certificate",
			"key", b.tlsKey,
			"crt", b.tlsCrt,
		)
	}

	// Create the listener:
	listener, err := net.Listen(b.network, b.address)
	if err != nil {
		return
	}
	if tlsCrt.Certificate != nil {
		listener = tls.NewListener(listener, &tls.Config{
			Certificates: []tls.Certificate{
				tlsCrt,
			},
			MinVersion: tls.VersionTLS12,
			NextProtos: slices.Clone(b.tlsProtocols),
		})
	}

	// Return the listener:
	result = listener

	return
}

// Common listener names:
const (
	APIListener     = "API"
	MetricsListener = "Metrics"
)

// Common listener addresses:
const (
	APIAddress     = "localhost:8000"
	MetricsAddress = "localhost:8001"
)
