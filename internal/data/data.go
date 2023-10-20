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

import (
	"fmt"

	"github.com/PaesslerAG/jsonpath"

	"github.com/jhernand/o2ims/internal/streaming"
)

// Object represents an object containing a list of fields, each with a name and a value.
type Object = map[string]any

func GetString(o Object, path string) (result string, err error) {
	value, err := jsonpath.Get(path, o)
	if err != nil {
		return
	}
	result, ok := value.(string)
	if !ok {
		err = fmt.Errorf("value of path '%s' isn't a string", path)
	}
	return
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

// Select creates a new stream that only contains the items of the source stream that return true
// for the given selector. Note that the actual calls to the select will not happen when this
// function is called, they will happen only when the stream is eventually consumed.
var Select = streaming.Select[Object]

// Map creates a stream that contains the result of transforming the objects of the given stream
// with a mapper. Note that the actual calls to the mapper will not happen when this function is
// called, they will happen only when the stream is eventually consumed.
var Map = streaming.Map[Object, Object]
