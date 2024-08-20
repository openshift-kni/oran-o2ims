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

import "github.com/spf13/pflag"

// AddFlags adds the flags related to authentication to the given flag set.
func AddFlags(set *pflag.FlagSet) {
	_ = set.StringArray(
		jwksFileFlagName,
		[]string{},
		"File containing the JSON web key set.",
	)
	_ = set.StringArray(
		jwksURLFlagName,
		[]string{},
		"URL containing the JSON web key set.",
	)
	_ = set.String(
		jwksTokenFlagName,
		"",
		"Bearer token used to download the JSON web key set.",
	)
	_ = set.String(
		jwksTokenFileFlagName,
		"",
		"File containing the bearer token used to download the JSON web key set.",
	)
	_ = set.String(
		jwksCAFileFlagName,
		"",
		"File containing the CA used to verify the TLS certificate of the JSON web "+
			"key set server.",
	)
}

// Names of the flags:
const (
	jwksFileFlagName      = "authn-jwks-file"
	jwksURLFlagName       = "authn-jwks-url"
	jwksTokenFlagName     = "authn-jwks-token"      // nolint: gosec  // attribute names only
	jwksTokenFileFlagName = "authn-jwks-token-file" // nolint: gosec  // attribute names only
	jwksCAFileFlagName    = "authn-jwks-ca-file"
)
