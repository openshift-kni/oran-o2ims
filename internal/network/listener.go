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
	"fmt"
	"log/slog"
	"net"

	"github.com/spf13/pflag"
)

// ListenerBuilder contains the data and logic needed to create a network listener. Don't create
// instances of this object directly, use the NewListener function instead.
type ListenerBuilder struct {
	logger  *slog.Logger
	network string
	address string
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
	if flags != nil {
		listenerAddrFlagName := listenerFlagName(name, listenerAddrFlagSuffix)
		value, err := flags.GetString(listenerAddrFlagName)
		if err == nil {
			b.SetAddress(value)
		}
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

// Build uses the data stored in the builder to create a new network listener.
func (b *ListenerBuilder) Build() (result net.Listener, err error) {
	// Check parameters:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}
	if b.network == "" {
		err = fmt.Errorf("network is mandatory")
		return
	}
	if b.address == "" {
		err = fmt.Errorf("address is mandatory")
		return
	}

	// Create and populate the object:
	result, err = net.Listen(b.network, b.address)
	return
}

// Common listener names:
const (
	APIListener = "API"
)

// Common listener addresses:
const (
	APIAddress = "localhost:8000"
)
