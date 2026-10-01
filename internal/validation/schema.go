/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package validation

import (
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// ValidateJSONSchema validates input against a JSON schema.
func ValidateJSONSchema(schema, input any) error {
	schemaLoader := gojsonschema.NewGoLoader(schema)
	inputLoader := gojsonschema.NewGoLoader(input)

	result, err := gojsonschema.Validate(schemaLoader, inputLoader)
	if err != nil {
		return fmt.Errorf("failed when validating the input against the schema: %w", err)
	}
	if result.Valid() {
		return nil
	}
	var errs []string
	for _, description := range result.Errors() {
		errs = append(errs, description.String())
	}
	return fmt.Errorf("invalid input: %s", strings.Join(errs, "; "))
}
