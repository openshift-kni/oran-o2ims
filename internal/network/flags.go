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
	"strings"

	"github.com/spf13/pflag"
)

// AddListenerFlags adds to the given flag set the flags needed to configure a network listener. It
// receives the name of the listerner and the default address. For example, to configure an API
// listener:
//
//	network.AddListenerFlags("API", "localhost:8000")
//
// The name will be converted to lower case to generate a prefix for the flags, and will be used
// unchanged as a prefix for the help text. The above example will result in the following flags:
//
//	--api-listener-address string API listen address. (default "localhost:8000")
func AddListenerFlags(set *pflag.FlagSet, name, addr string) {
	_ = set.String(
		listenerFlagName(name, listenerAddrFlagSuffix),
		addr,
		fmt.Sprintf("%s listen address.", name),
	)
}

// Names of the flags:
const (
	listenerAddrFlagSuffix = "listener-address"
)

// listenerFlagName calculates a complete flag name from a listener name and a flag name suffix.
// For example, if the listener name is 'API' and the flag name suffix is 'listener-address' it
// returns 'api-listener-address'.
func listenerFlagName(name, suffix string) string {
	return fmt.Sprintf("%s-%s", strings.ToLower(name), suffix)
}
