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
	"fmt"
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
	logger *slog.Logger
}

// projectorParserTask contains the data needed to perform the parsing of one field selection
// specification. A new one will be created each time that the Parse method is called.
type projectorParserTask struct {
	logger *slog.Logger
	lexer  *projectorLexer
	token  *projectorToken
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

	// Create and populate the object:
	result = &ProjectorParser{
		logger: b.logger,
	}
	return
}

// Parse parses the give field selector. If it succeeds it returns the object selector. If it fails
// it returns an error.
func (p *ProjectorParser) Parse(text string) (result [][]string, err error) {
	// In order to simplify the rest of the parsing code we will panic when an error is
	// detected. This recovers from those panics and converts them into regular errors.
	defer func() {
		fault := recover()
		if fault != nil {
			p.logger.Error(
				"Failed to parse",
				"text", text,
				"error", err,
			)
			err = fault.(error)
		}
	}()

	// Create the lexer:
	lexer, err := newProjectorLexer().
		SetLogger(p.logger).
		SetSource(text).
		Build()
	if err != nil {
		return
	}

	// Create and run the parse task:
	task := &projectorParserTask{
		logger: p.logger,
		lexer:  lexer,
	}
	result = task.parseProjector()
	return
}

func (t *projectorParserTask) parseProjector() [][]string {
	var paths [][]string
	for {
		path := t.parsePath()
		paths = append(paths, path)
		if t.checkToken(projectorSymbolComma) {
			t.fetchToken()
			continue
		}
		if t.checkToken(projectorSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting comma or right parenthesis",
			t.currentToken().Text,
		))
	}
	return paths
}

func (t *projectorParserTask) parsePath() []string {
	var segments []string
	for {
		segment := t.parseIdentifier()
		segments = append(segments, segment)
		if t.checkToken(projectorSymbolSlash) {
			t.fetchToken()
			continue
		}
		if t.checkToken(projectorSymbolComma) {
			break
		}
		if t.checkToken(projectorSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting slash or comma",
			t.currentToken().Text,
		))
	}
	return segments
}

func (t *projectorParserTask) parseIdentifier() string {
	token := t.currentToken()
	t.consumeToken(projectorSymbolIdentifier)
	return token.Text
}

// currentToken resturns the current token, fetching it from the lexer if needed.
func (t *projectorParserTask) currentToken() *projectorToken {
	t.ensureToken()
	return t.token
}

// fetchToken discard the current token and fetches a new one from the lexer.
func (t *projectorParserTask) fetchToken() {
	token, err := t.lexer.FetchToken()
	if err != nil {
		panic(err)
	}
	t.token = token
}

// checkToken returns true if the current token has the given symbol.
func (t *projectorParserTask) checkToken(symbol projectorSymbol) bool {
	t.ensureToken()
	return t.token.Symbol == symbol
}

// consumeToken checks that the symbol of the current token and then discards it, so that the next
// time that a token is needed a new one will be fetched from the lexer. If the symbol is not the
// given one then it panics.
func (t *projectorParserTask) consumeToken(symbol projectorSymbol) {
	t.ensureToken()
	if t.token.Symbol != symbol {
		var expected string
		switch symbol {
		case projectorSymbolEnd:
			expected = "end of input"
		case projectorSymbolIdentifier:
			expected = "identifier"
		case projectorSymbolComma:
			expected = "comma"
		case projectorSymbolSlash:
			expected = "slash"
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting %s",
			t.token.Text, expected,
		))
	}
	t.token = nil
}

// ensureToken makes sure the current token is populated, fetching it from the lexer if needed.
func (t *projectorParserTask) ensureToken() {
	if t.token == nil {
		t.fetchToken()
	}
}
