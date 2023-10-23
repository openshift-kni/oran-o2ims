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

package selector

import (
	"errors"
	"fmt"
	"log/slog"
)

// ParserBuilder contains the logic and data needed to create field selector parsers. Don't
// create instances of this type directly, use the NewParser function instead.
type ParserBuilder struct {
	logger *slog.Logger
}

// Parser knows how to parse field selectors. Don't create instances of this type directly, use
// the NewParser function instead.
type Parser struct {
	logger *slog.Logger
}

// parseTask contains the data needed to perform the parsing of one field selector. A new one
// will be created each time that the Parse method is called.
type parseTask struct {
	logger *slog.Logger
	lexer  *exprLexer
	token  *exprToken
}

// NewParser creates a builder that can then be used to configure and create field selector
// parsers.
func NewParser() *ParserBuilder {
	return &ParserBuilder{}
}

// SetLogger sets the logger that the parser will use to write log messages. This is mandatory.
func (b *ParserBuilder) SetLogger(value *slog.Logger) *ParserBuilder {
	b.logger = value
	return b
}

// Build uses the configuration stored in the builder to create a new parser.
func (b *ParserBuilder) Build() (result *Parser, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &Parser{
		logger: b.logger,
	}
	return
}

// Parse parses the give field selector. If it succeeds it returns the object selector. If it fails
// it returns an error.
func (p *Parser) Parse(text string) (result [][]string, err error) {
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
	lexer, err := newExprLexer().
		SetLogger(p.logger).
		SetSource(text).
		Build()
	if err != nil {
		return
	}

	// Create and run the parse task:
	task := &parseTask{
		logger: p.logger,
		lexer:  lexer,
	}
	result = task.parsePaths()
	return
}

func (t *parseTask) parsePaths() [][]string {
	var paths [][]string
	for {
		path := t.parsePath()
		paths = append(paths, path)
		if t.checkToken(exprSymbolComma) {
			t.fetchToken()
			continue
		}
		if t.checkToken(exprSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting comma or right parenthesis",
			t.currentToken().Text,
		))
	}
	return paths
}

func (t *parseTask) parsePath() []string {
	var segments []string
	for {
		segment := t.parseIdentifier()
		segments = append(segments, segment)
		if t.checkToken(exprSymbolSlash) {
			t.fetchToken()
			continue
		}
		if t.checkToken(exprSymbolComma) {
			break
		}
		if t.checkToken(exprSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting slash or comma",
			t.currentToken().Text,
		))
	}
	return segments
}

func (t *parseTask) parseIdentifier() string {
	token := t.currentToken()
	t.consumeToken(exprSymbolIdentifier)
	return token.Text
}

// currentToken resturns the current token, fetching it from the lexer if needed.
func (t *parseTask) currentToken() *exprToken {
	t.ensureToken()
	return t.token
}

// fetchToken discard the current token and fetches a new one from the lexer.
func (t *parseTask) fetchToken() {
	token, err := t.lexer.FetchToken()
	if err != nil {
		panic(err)
	}
	t.token = token
}

// checkToken returns true if the current token has the given symbol.
func (t *parseTask) checkToken(symbol exprSymbol) bool {
	t.ensureToken()
	return t.token.Symbol == symbol
}

// consumeToken checks that the symbol of the current token and then discards it, so that the next
// time that a token is needed a new one will be fetched from the lexer. If the symbol is not the
// given one then it panics.
func (t *parseTask) consumeToken(symbol exprSymbol) {
	t.ensureToken()
	if t.token.Symbol != symbol {
		var expected string
		switch symbol {
		case exprSymbolEnd:
			expected = "end of input"
		case exprSymbolIdentifier:
			expected = "identifier"
		case exprSymbolComma:
			expected = "comma"
		case exprSymbolSlash:
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
func (t *parseTask) ensureToken() {
	if t.token == nil {
		t.fetchToken()
	}
}
