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

package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
)

// StreamBuilder contains the data and logic needed to create an stream that processes list of
// Kubernetes objects. Don't create instances of this type directly, use the NewStream function
// instead.
type StreamBuilder struct {
	logger *slog.Logger
	reader io.Reader
}

// Stream is a stream that processes lists of Kubernetes objects. It assumes that the input
// contains the JSON representation of a list, locates the `items` field and then proceses the
// items in a streaming fashion, without first reading all the complete list of items in memory.
// Don't create instances of this type directly, use the NewStream function instead.
type Stream struct {
	logger   *slog.Logger
	reader   io.Reader
	iterator *jsoniter.Iterator
	found    bool
}

// NewStream creates a builder that can then be used to create and configure a stream that
// processes list of Kubernetes objects.
func NewStream() *StreamBuilder {
	return &StreamBuilder{}
}

// SetLogger sets the logger that the stream will use to write to the log. This is mandatory.
func (b *StreamBuilder) SetLogger(value *slog.Logger) *StreamBuilder {
	b.logger = value
	return b
}

// SetReader sets the input source. This is mandatory.
//
// Note that the stream will automatically close this reader when it reaches the end of the stream,
// so refrain from closing it as it may result in prematurely ending the stream.
func (b *StreamBuilder) SetReader(value io.Reader) *StreamBuilder {
	b.reader = value
	return b
}

func (b *StreamBuilder) Build() (result *Stream, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.reader == nil {
		err = errors.New("reader is mandatory")
		return
	}

	// Create the JSON iterator:
	cfg := jsoniter.Config{}
	api := cfg.Froze()
	iterator := jsoniter.Parse(api, b.reader, 4096)

	// Create and populate the object:
	result = &Stream{
		logger:   b.logger,
		reader:   b.reader,
		iterator: iterator,
		found:    false,
	}
	return
}

// Next is the implementation of the stream interface.
func (s *Stream) Next(ctx context.Context) (item data.Object, err error) {
	if !s.found {
		err = s.find(ctx)
		if err != nil {
			s.close()
			return
		}
	}
	if s.iterator.ReadArray() {
		item, err = s.readObject()
		if err != nil {
			s.close()
			return
		}
		s.logger.Info(
			"Read item",
			"item", item,
		)
	} else {
		s.close()
		err = data.ErrEnd
	}
	return
}

// find reads from the input until it finds the `items` field, ignoring everything before that.
func (s *Stream) find(ctx context.Context) error {
	for {
		field := s.iterator.ReadObject()
		if s.iterator.Error != nil {
			return s.iterator.Error
		}
		if field == "" {
			return nil
		}
		if field == "items" {
			s.found = true
			return nil
		}
		s.iterator.Skip()
		if s.iterator.Error != nil {
			return s.iterator.Error
		}
		s.logger.Debug(
			"Ignored field while looking for items",
			"field", field,
		)
	}
}

// close closes the input reader, if it is closeable.
func (s *Stream) close() {
	closer, ok := s.reader.(io.ReadCloser)
	if ok {
		err := closer.Close()
		if err != nil {
			s.logger.Error(
				"Failed to close reader",
				"error", err,
			)
		}
	}
}

func (s *Stream) readObject() (result data.Object, err error) {
	result = data.Object{}
	s.iterator.ReadMapCB(func(iter *jsoniter.Iterator, name string) bool {
		var value any
		value, err = s.readValue()
		if err != nil {
			return false
		}
		result[name] = value
		return true
	})
	err = s.iterator.Error
	return
}

func (s *Stream) readValue() (result any, err error) {
	next := s.iterator.WhatIsNext()
	switch next {
	case jsoniter.StringValue:
		result = s.iterator.ReadString()
		err = s.iterator.Error
	case jsoniter.NumberValue:
		result = s.iterator.ReadFloat64()
		err = s.iterator.Error
	case jsoniter.NilValue:
		s.iterator.ReadNil()
		result = nil
		err = s.iterator.Error
	case jsoniter.BoolValue:
		result = s.iterator.ReadBool()
		err = s.iterator.Error
	case jsoniter.ArrayValue:
		list := []any{}
		s.iterator.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
			var value any
			value, err = s.readValue()
			if err != nil {
				return false
			}
			list = append(list, value)
			return true
		})
		result = list
	case jsoniter.ObjectValue:
		result, err = s.readObject()
	default:
		err = fmt.Errorf("unknown value type %v", next)
	}
	return
}
