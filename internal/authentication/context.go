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

// This file contains functions that extract information from the context.

package authentication

import (
	"context"
)

// contextKey is the type used to store the authentication information in the context.
type contextKey int

const (
	subjectContextKey contextKey = iota
)

// ContextWithSubject creates a new context containing the given subject.
func ContextWithSubject(parent context.Context, subject *Subject) context.Context {
	return context.WithValue(parent, subjectContextKey, subject)
}

// SubjectFromContext extracts the subject from the context. Panics if there is no subject in the
// context.
func SubjectFromContext(ctx context.Context) *Subject {
	subject := ctx.Value(subjectContextKey)
	switch subject := subject.(type) {
	case *Subject:
		return subject
	default:
		panic("failed to get subject from context")
	}
}
