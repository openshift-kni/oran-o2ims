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

package search

import (
	"errors"
	"log/slog"
)

// ProjectorParserBuilder contains the logic and data needed to create field selection parsers.
// Don't create instances of this type directly, use the NewProjectorParser function instead.
type ProjectorParserBuilder struct {
	logger *slog.Logger
}

// ProjectorParser knows how to parse field selectors. Don't create instances of this type
// directly, use the NewProjectorParser function instead.
type ProjectorParser struct {
	logger      *slog.Logger
	pathsParser *PathsParser
}

// NewProjectorParser creates a builder that can then be used to configure and create field
// selector parsers.
func NewProjectorParser() *ProjectorParserBuilder {
	return &ProjectorParserBuilder{}
}

// SetLogger sets the logger that the parser will use to write log messages. This is mandatory.
func (b *ProjectorParserBuilder) SetLogger(value *slog.Logger) *ProjectorParserBuilder {
	b.logger = value
	return b
}

// Build uses the configuration stored in the builder to create a new parser.
func (b *ProjectorParserBuilder) Build() (result *ProjectorParser, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create the paths parser:
	pathsParser, err := NewPathsParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &ProjectorParser{
		logger:      b.logger,
		pathsParser: pathsParser,
	}
	return
}

// Parse parses the give field selector. If it succeeds it returns the object selector. If it fails
// it returns an error.
func (p *ProjectorParser) Parse(include, exclude string) (result *Projector, err error) {
	// Parse the paths:
	var includePaths, excludePaths []Path
	if include != "" {
		includePaths, err = p.pathsParser.Parse(include)
		if err != nil {
			return
		}
	}
	if exclude != "" {
		excludePaths, err = p.pathsParser.Parse(exclude)
		if err != nil {
			return
		}
	}

	// Create the result:
	result = &Projector{
		Include: includePaths,
		Exclude: excludePaths,
	}
	return
}
