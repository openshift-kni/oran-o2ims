/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package data

import (
	"github.com/openshift-kni/oran-o2ims/internal/streaming"
)

// Object represents an object containing a list of fields, each with a name and a value.
type Object = map[string]any

// Array represents a list of objecs.
type Array = []any

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
