/*
Copyright (c) 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package filter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
)

// ResolverBuilder contains the logic and data needed to create attribute path resolvers. Don't
// create instances of this type directly, use the NewResolver function instead.
type ResolverBuilder struct {
	logger *slog.Logger
}

// Resolver knows how extract from an object the value of an attribute given its/ path, using the
// reflect package. Don't create instances of this type directly use the NewResolver function
// instead.
type Resolver struct {
	logger *slog.Logger
}

// NewResolver creates a builder that can then be used to configure and create resolvers.
func NewResolver() *ResolverBuilder {
	return &ResolverBuilder{}
}

// SetLogger sets the logger that the resolver will use to write log messages. This is mandatory.
func (b *ResolverBuilder) SetLogger(value *slog.Logger) *ResolverBuilder {
	b.logger = value
	return b
}

// Build uses the configuration stored in the builder to create a new resolver.
func (b *ResolverBuilder) Build() (result *Resolver, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &Resolver{
		logger: b.logger,
	}
	return
}

// Resolve receives the attribute path and the object and returns the value of that attribute.
func (r *Resolver) Resolve(ctx context.Context, path []string, object any) (result any, err error) {
	value, err := r.resolve(ctx, path, reflect.ValueOf(object))
	if err != nil {
		err = fmt.Errorf(
			"failed to resolve '%s': %w",
			strings.Join(path, "/"), err,
		)
		return
	}
	if !value.IsValid() {
		result = nil
	} else {
		result = value.Interface()
	}
	return
}

func (r *Resolver) resolve(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	if len(path) == 0 {
		result = object
		return
	}
	kind := object.Kind()
	switch kind {
	case reflect.Struct:
		result, err = r.resolveStruct(ctx, path, object)
	case reflect.Pointer:
		result, err = r.resolvePointer(ctx, path, object)
	case reflect.Map:
		result, err = r.resolveMap(ctx, path, object)
	case reflect.Interface:
		result, err = r.resolveInterface(ctx, path, object)
	default:
		err = fmt.Errorf(
			"expected struct, slice or map, but found '%s'",
			object.Type(),
		)
	}
	return
}

func (r *Resolver) resolveStruct(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	field := path[0]
	value := object.FieldByName(field)
	if !value.IsValid() {
		typ := object.Type()
		err = fmt.Errorf(
			"struct '%s' from package '%s' doesn't have a '%s' field",
			typ.Name(), typ.PkgPath(), field,
		)
		return
	}
	result, err = r.resolve(ctx, path[1:], value)
	return
}

func (r *Resolver) resolvePointer(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	if object.IsNil() {
		result = reflect.ValueOf(nil)
		return
	}
	value := object.Elem()
	result, err = r.resolve(ctx, path, value)
	return
}

func (r *Resolver) resolveMap(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	key := path[0]
	value := object.MapIndex(reflect.ValueOf(key))
	if !value.IsValid() {
		err = fmt.Errorf("map doesn't have a '%s' key", path[0])
		return
	}
	result, err = r.resolve(ctx, path[1:], value)
	return
}

func (r *Resolver) resolveInterface(ctx context.Context, path []string,
	object reflect.Value) (result reflect.Value, err error) {
	if object.IsNil() {
		result = reflect.ValueOf(nil)
		return
	}
	value := object.Elem()
	result, err = r.resolve(ctx, path, value)
	return
}
