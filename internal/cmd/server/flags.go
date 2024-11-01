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

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// AddTokenFlags adds the flags needed to configure a token to the given flag set.
func AddTokenFlags(set *pflag.FlagSet) {
	_ = set.String(
		backendTokenFlagName,
		"",
		"Token for authenticating to the backend server.",
	)
	_ = set.String(
		backendTokenFileFlagName,
		"",
		"File containing the token for authenticating to the backend server.",
	)
}

// GetTokenFlag gets the value of the token flag.
func GetTokenFlag(
	ctx context.Context, set *pflag.FlagSet, logger *slog.Logger) (string, error) {

	backendToken, err := set.GetString(backendTokenFlagName)
	if err != nil {
		errString := fmt.Sprintf(
			"Failed to get backend token flag. %s; %s",
			slog.String("flag", backendTokenFlagName), slog.String("error", err.Error()))
		logger.ErrorContext(
			ctx,
			errString,
		)
		return "", errors.New(errString)
	}

	backendTokenFile, err := set.GetString(backendTokenFileFlagName)
	if err != nil {
		errString := fmt.Sprintf(
			"Failed to get backend token file flag. %s; %s",
			slog.String("flag", backendTokenFileFlagName), slog.String("error", err.Error()))
		logger.ErrorContext(
			ctx,
			errString,
		)
		return "", errors.New(errString)
	}

	// Check that the backend token and token file haven't been simultaneously provided:
	if backendToken != "" && backendTokenFile != "" {
		errString := fmt.Sprintf(
			"backend token flag '--%s' and token file flag '--%s' have both been provided, "+
				"but they are incompatible",
			backendTokenFlagName, backendTokenFileFlagName)

		logger.ErrorContext(
			ctx,
			errString,
			slog.Any(
				"flags",
				[]string{
					backendTokenFlagName,
					backendTokenFileFlagName,
				},
			),
			slog.String("!token", backendToken),
			slog.String("token_file", backendTokenFile),
		)
		return "", errors.New(errString)
	}

	// Read the backend token file if needed:
	if backendToken == "" && backendTokenFile != "" {
		backendTokenData, err := os.ReadFile(backendTokenFile)
		if err != nil {
			errString := fmt.Sprintf(
				"Failed to read backend token file. %s; %s",
				slog.String("file", backendTokenFile), slog.String("error", err.Error()))
			logger.ErrorContext(
				ctx,
				errString,
			)
			return "", errors.New(errString)
		}
		backendToken = strings.TrimSpace(string(backendTokenData))
		logger.InfoContext(
			ctx,
			"Loaded backend token from file",
			slog.String("file", backendTokenFile),
			slog.String("!token", backendToken),
		)
	}

	// Check that we have a token:
	if backendToken == "" {
		errString := fmt.Sprintf(
			"backend token '--%s' or token file '--%s' parameters must be provided",
			backendTokenFlagName,
			backendTokenFileFlagName)
		logger.ErrorContext(ctx, errString)
		return "", errors.New(errString)
	}

	logger.InfoContext(
		ctx,
		"Backend token details",
		slog.String("!token", backendToken),
		slog.String("token_file", backendTokenFile),
	)
	return backendToken, nil
}

// Names of command line flags:
const (
	backendTokenFileFlagName          = "backend-token-file"
	backendTokenFlagName              = "backend-token"
	backendTypeFlagName               = "backend-type"
	backendURLFlagName                = "backend-url"
	cloudIDFlagName                   = "cloud-id"
	extensionsFlagName                = "extensions"
	externalAddressFlagName           = "external-address"
	globalCloudIDFlagName             = "global-cloud-id"
	namespaceFlagName                 = "namespace"
	resourceServerTokenFlagName       = "resource-server-token"
	resourceServerURLFlagName         = "resource-server-url"
	subscriptionConfigmapNameFlagName = "configmap-name"
)
