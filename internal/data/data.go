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
	"strings"

	"github.com/PaesslerAG/jsonpath"
	"github.com/imdario/mergo"
	"github.com/itchyny/gojq"
	"github.com/thoas/go-funk"

	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/streaming"
)

// Object represents an object containing a list of fields, each with a name and a value.
type Object = map[string]any

// Array represents a list of objecs.
type Array = []any

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

func JQString(o Object, source string) (result string, err error) {
	query, err := gojq.Parse(source)
	if err != nil {
		return
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return
	}
	iter := code.Run(o)
	value, ok := iter.Next()
	if !ok {
		err = fmt.Errorf("query '%s' didn't return a value", source)
		return
	}
	text, ok := value.(string)
	if !ok {
		err = fmt.Errorf(
			"query '%s' returned a value of type '%T' instead of a string",
			source, value,
		)
		return
	}
	result = text
	return
}

func GetArray(o Object, path string) (result []any, err error) {
	value, err := jsonpath.Get(path, o)
	if err != nil {
		return
	}
	result, ok := value.([]any)
	if !ok {
		err = fmt.Errorf("value of path '%s' isn't an array", value)
	}
	return
}

func GetObj(o Object, path string) (result Object, err error) {
	value, err := jsonpath.Get(path, o)
	if err != nil {
		return
	}
	result, ok := value.(Object)
	if !ok {
		err = fmt.Errorf("value of path '%s' isn't an object", value)
	}
	return
}

func GetLabelsMap(labels string) (labelsMap Object) {
	labelsArr := strings.Split(labels, "; ")
	labelsMap = Object{}
	for _, label := range labelsArr {
		keyValue := strings.Split(label, "=")
		labelsMap[keyValue[0]] = keyValue[1]
	}
	return
}

func GetExtensions(input Object, extensions []string, jqTool *jq.Tool) (output Object, err error) {
	for _, extension := range extensions {
		var value Object
		err = jqTool.Evaluate(extension, input, &value)
		if err != nil {
			continue
		}
		if value != nil {
			if funk.Contains(funk.Values(value), nil) {
				// Ignore 'nil' values
				continue
			}
			// Append value to output
			err = mergo.Merge(
				&output,
				value,
				mergo.WithOverride,
			)
			if err != nil {
				continue
			}
		}
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

// Collect collects all the items in the given stream and returns an slice containing them.
var Collect = streaming.Collect[Object]
