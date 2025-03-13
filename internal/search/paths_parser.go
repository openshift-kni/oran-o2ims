/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	"errors"
	"fmt"
	"log/slog"
)

// PathsParserBuilder contains the logic and data needed to create field paths parsers.
// Don't create instances of this type directly, use the NewPathsParser function instead.
type PathsParserBuilder struct {
	logger *slog.Logger
}

// PathsParser knows how to parse field paths. Don't create instances of this type directly, use
// the NewPathsParser function instead.
type PathsParser struct {
	logger *slog.Logger
}

// pathsParserTask contains the data needed to perform the parsing of field paths. A new one will
// be created each time that the Parse method is called.
type pathsParserTask struct {
	logger *slog.Logger
	lexer  *pathsLexer
	token  *pathsToken
}

// NewPathsParser creates a builder that can then be used to configure and create path parsers.
func NewPathsParser() *PathsParserBuilder {
	return &PathsParserBuilder{}
}

// SetLogger sets the logger that the parser will use to write log messages. This is mandatory.
func (b *PathsParserBuilder) SetLogger(value *slog.Logger) *PathsParserBuilder {
	b.logger = value
	return b
}

// Build uses the configuration stored in the builder to create a new parser.
func (b *PathsParserBuilder) Build() (result *PathsParser, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &PathsParser{
		logger: b.logger,
	}
	return
}

// Parse parses the given field paths.
func (p *PathsParser) Parse(paths ...string) (result []Path, err error) {
	// In order to simplify the rest of the parsing code we will panic when an error is
	// detected. This recovers from those panics and converts them into regular errors.
	defer func() {
		fault := recover()
		if fault != nil {
			p.logger.Error(
				"Failed to parse",
				"text", paths,
				"error", err,
			)
			err = fault.(error)
		}
	}()

	// Parse the paths:
	for _, path := range paths {
		lexer, err := newPathsLexer().
			SetLogger(p.logger).
			SetSource(path).
			Build()
		if err != nil {
			panic(err)
		}
		task := &pathsParserTask{
			logger: p.logger,
			lexer:  lexer,
		}
		result = append(result, task.parsePaths()...)
	}
	return
}

func (t *pathsParserTask) parsePaths() []Path {
	var paths []Path
	for {
		path := t.parsePath()
		paths = append(paths, path)
		if t.checkToken(pathsSymbolComma) {
			t.fetchToken()
			continue
		}
		if t.checkToken(pathsSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting comma or right parenthesis",
			t.currentToken().Text,
		))
	}
	return paths
}

func (t *pathsParserTask) parsePath() Path {
	var segments Path
	for {
		segment := t.parseIdentifier()
		segments = append(segments, segment)
		if t.checkToken(pathsSymbolSlash) {
			t.fetchToken()
			continue
		}
		if t.checkToken(pathsSymbolComma) {
			break
		}
		if t.checkToken(pathsSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting slash or comma",
			t.currentToken().Text,
		))
	}
	return segments
}

func (t *pathsParserTask) parseIdentifier() string {
	token := t.currentToken()
	t.consumeToken(pathsSymbolIdentifier)
	return token.Text
}

// currentToken resturns the current token, fetching it from the lexer if needed.
func (t *pathsParserTask) currentToken() *pathsToken {
	t.ensureToken()
	return t.token
}

// fetchToken discard the current token and fetches a new one from the lexer.
func (t *pathsParserTask) fetchToken() {
	token, err := t.lexer.FetchToken()
	if err != nil {
		panic(err)
	}
	t.token = token
}

// checkToken returns true if the current token has the given symbol.
func (t *pathsParserTask) checkToken(symbol pathsSymbol) bool {
	t.ensureToken()
	return t.token.Symbol == symbol
}

// consumeToken checks that the symbol of the current token and then discards it, so that the next
// time that a token is needed a new one will be fetched from the lexer. If the symbol is not the
// given one then it panics.
func (t *pathsParserTask) consumeToken(symbol pathsSymbol) {
	t.ensureToken()
	if t.token.Symbol != symbol {
		var expected string
		switch symbol {
		case pathsSymbolEnd:
			expected = "end of input"
		case pathsSymbolIdentifier:
			expected = identifierToken
		case pathsSymbolComma:
			expected = "comma"
		case pathsSymbolSlash:
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
func (t *pathsParserTask) ensureToken() {
	if t.token == nil {
		t.fetchToken()
	}
}
