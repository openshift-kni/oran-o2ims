/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package typederrors

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrors(t *testing.T) {
	e := errors.New("a standard error")
	ge := GenericError{
		Message: "a GenericError",
		Err:     nil,
	}
	gew := GenericError{
		Message: "a GenericError wraps a standard error",
		Err:     e,
	}
	ew := fmt.Errorf("a standard error wraps a GenericError: %w", ge)
	te := NewTokenError(nil, "a TokenError")
	tew := NewTokenError(e, "a TokenError wraps a %s", "standard error")
	cme := NewConfigMapError(nil, "a ConfigMapError")
	cmew := NewConfigMapError(e, "a ConfigMapError wraps a %s", "standard error")
	cmew2 := NewConfigMapError(te, "a ConfigMapError wraps a %s", "TokenError")
	ew2 := fmt.Errorf("a standard error wraps a TokenError: %w", te)
	cmew3 := NewConfigMapError(ew2, "a ConfigMapError wraps a %s which wraps a %s", "standard error", "TokenError")

	tests := []struct {
		description            string
		wrappedError           error
		errorType              error
		expectedMessage        string
		expectIsConfigMapError bool
		expectIsTokenError     bool
		expectWrap             bool
	}{
		{
			description:            "a standard error wraps a GenericError",
			errorType:              ew,
			wrappedError:           ge,
			expectedMessage:        "a standard error wraps a GenericError: a GenericError",
			expectIsConfigMapError: false,
			expectIsTokenError:     false,
			expectWrap:             true,
		},
		{
			description:            "a GenericError wraps a standard error",
			wrappedError:           e,
			errorType:              gew,
			expectedMessage:        "a GenericError wraps a standard error",
			expectIsConfigMapError: false,
			expectIsTokenError:     false,
			expectWrap:             true,
		},
		{
			description:            "a ConfigMapError wraps a standard error",
			wrappedError:           e,
			errorType:              cmew,
			expectedMessage:        "a ConfigMapError wraps a standard error",
			expectIsConfigMapError: true,
			expectIsTokenError:     false,
			expectWrap:             true,
		},
		{
			description:            "a ConfigMapError does not wrap an error",
			wrappedError:           nil,
			errorType:              cme,
			expectedMessage:        "a ConfigMapError",
			expectIsConfigMapError: true,
			expectIsTokenError:     false,
			expectWrap:             false,
		},
		{
			description:            "a ConfigMapError wraps a TokenError",
			wrappedError:           te,
			errorType:              cmew2,
			expectedMessage:        "a ConfigMapError wraps a TokenError",
			expectIsConfigMapError: true,
			expectIsTokenError:     true,
			expectWrap:             true,
		},
		{
			description:            "a TokenError wraps a standard error",
			wrappedError:           e,
			errorType:              tew,
			expectedMessage:        "a TokenError wraps a standard error",
			expectIsConfigMapError: false,
			expectIsTokenError:     true,
			expectWrap:             true,
		},
		{
			description:            "a ConfigMapError wraps a standard error which wraps a TokenError (check TokenError wrapped)",
			wrappedError:           te,
			errorType:              cmew3,
			expectedMessage:        "a ConfigMapError wraps a standard error which wraps a TokenError",
			expectIsConfigMapError: true,
			expectIsTokenError:     true,
			expectWrap:             true,
		},
		{
			description:            "a ConfigMapError wraps a standard error which wraps a TokenError (check standard error wrapped)",
			wrappedError:           ew2,
			errorType:              cmew3,
			expectedMessage:        "a ConfigMapError wraps a standard error which wraps a TokenError",
			expectIsConfigMapError: true,
			expectIsTokenError:     true,
			expectWrap:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.errorType.Error() != tt.expectedMessage {
				t.Errorf("expected message: '%s', got '%s'", tt.expectedMessage, tt.errorType.Error())
			}

			if errors.Is(tt.errorType, tt.wrappedError) != tt.expectWrap {
				t.Errorf("expected wrap: %v", tt.expectWrap)
			}

			if IsConfigMapError(tt.errorType) != tt.expectIsConfigMapError {
				t.Errorf("expected IsConfigMapError: %v", tt.expectIsConfigMapError)
			}

			if IsTokenError(tt.errorType) != tt.expectIsTokenError {
				t.Errorf("expected IsTokenError: %v", tt.expectIsTokenError)
			}
		})
	}
}
