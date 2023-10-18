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

package data

import "github.com/jhernand/o2ims/internal/streaming"

// Object represents an object containing a list of fields, each with a name and a value.
type Object []Field

// Fields is a field of an object, with a name and a value.
type Field struct {
	Name  string
	Value any
}

// Stream is a stream of objects.
type Stream = streaming.Stream[Object]

// StreamFunc creates an string using the given function.
type StreamFunc = streaming.StreamFunc[Object]

// ErrEnd is the error returned by a stream when there are no more items.
var ErrEnd = streaming.ErrEnd

// Null is an empty stream of objects.
var Null = streaming.Null[Object]

// Pour creates a stream that contains the objects in the given slice.
var Pour = streaming.Pour[Object]

// Repeat creates a stream that repeats the same object multiple times.
var Repeat = streaming.Repeat[Object]
