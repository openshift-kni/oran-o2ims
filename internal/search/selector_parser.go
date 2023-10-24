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
	"strings"
)

// SelectorParserBuilder contains the logic and data needed to create filter expression parsers.
// Don't create instances of this type directly, use the NewSelectorParser function instead.
type SelectorParserBuilder struct {
	logger *slog.Logger
}

// SelectorParser knows how to parse filter expressions. Don't create instances of this type
// directly, use the NewSelectorParser function instead.
type SelectorParser struct {
	logger *slog.Logger
}

// selectorParserTask contains the data needed to perform the parsing of one filter expression. A
// new one will be created each time that the Parse method is called.
type selectorParserTask struct {
	logger *slog.Logger
	lexer  *selectorLexer
	token  *selectorToken
}

// NewSelectorParser creates a builder that can then be used to configure and create expression
// filter // parsers. The builder can be reused to create multiple parsers with identical
// configuration.
func NewSelectorParser() *SelectorParserBuilder {
	return &SelectorParserBuilder{}
}

// SetLogger sets the logger that the parser will use to write log messages. This is mandatory.
func (b *SelectorParserBuilder) SetLogger(value *slog.Logger) *SelectorParserBuilder {
	b.logger = value
	return b
}

// Build uses the configuration stored in the builder to create a new parser.
func (b *SelectorParserBuilder) Build() (result *SelectorParser, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create and populate the object:
	result = &SelectorParser{
		logger: b.logger,
	}
	return
}

// Parse parses the give filter expression. If it succeeds it returns the object representing
// that expression. If it fails it returns an error.
func (p *SelectorParser) Parse(text string) (selector *Selector, err error) {
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
	lexer, err := newSelectorLexer().
		SetLogger(p.logger).
		SetSource(text).
		Build()
	if err != nil {
		return
	}

	// Create and run the task:
	task := &selectorParserTask{
		logger: p.logger,
		lexer:  lexer,
	}
	selector = task.parseSelector()
	return
}

func (t *selectorParserTask) parseSelector() *Selector {
	var terms []*Term
	for {
		term := t.parseTerm()
		terms = append(terms, term)
		if t.checkToken(selectorSymbolSemicolon) {
			t.fetchToken()
			continue
		}
		if t.checkToken(selectorSymbolEnd) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting semicolon or end of input",
			t.currentToken(),
		))
	}
	return &Selector{
		Terms: terms,
	}
}

func (t *selectorParserTask) parseTerm() *Term {
	t.consumeToken(selectorSymbolLeftParenthesis)
	operator := t.parseOperator()
	t.consumeToken(selectorSymbolComma)
	path := t.parsePath()
	t.consumeToken(selectorSymbolComma)
	t.lexer.SetMode(selectorLexerValuesMode)
	values := t.parseOptionalValues()
	t.lexer.SetMode(selectorLexerDefaultMode)
	t.consumeToken(selectorSymbolRightParenthesis)
	return &Term{
		Operator: operator,
		Path:     path,
		Values:   values,
	}
}

func (t *selectorParserTask) parseOperator() Operator {
	name := t.parseIdentifier()
	switch strings.ToLower(name) {
	case "cont":
		return Cont
	case "eq":
		return Eq
	case "gt":
		return Gt
	case "gte":
		return Gte
	case "in":
		return In
	case "lt":
		return Lt
	case "lte":
		return Lte
	case "ncont":
		return Ncont
	case "neq":
		return Neq
	case "nin":
		return Nin
	default:
		panic(fmt.Errorf("unknown operator '%s'", name))
	}
}

func (t *selectorParserTask) parsePath() []string {
	var segments []string
	for {
		segment := t.parseIdentifier()
		segments = append(segments, segment)
		if t.checkToken(selectorSymbolSlash) {
			t.fetchToken()
			continue
		}
		if t.checkToken(selectorSymbolComma) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting slash or comma",
			t.currentToken().Text,
		))
	}
	return segments
}

func (t *selectorParserTask) parseIdentifier() string {
	token := t.currentToken()
	t.consumeToken(selectorSymbolIdentifier)
	return token.Text
}

func (t *selectorParserTask) parseOptionalValues() []any {
	if t.checkToken(selectorSymbolRightParenthesis) {
		return []any{}
	}
	if t.checkToken(selectorSymbolString) {
		return t.parseValues()
	}
	panic(fmt.Errorf(
		"unexpected token '%s' while expecting value or right parenthesis",
		t.currentToken().Text,
	))
}

func (t *selectorParserTask) parseValues() []any {
	var values []any
	for {
		value := t.parseValue()
		values = append(values, value)
		if t.checkToken(selectorSymbolComma) {
			t.fetchToken()
			continue
		}
		if t.checkToken(selectorSymbolRightParenthesis) {
			break
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting comma or right parenthesis",
			t.currentToken().Text,
		))
	}
	return values
}

func (t *selectorParserTask) parseValue() any {
	token := t.currentToken()
	t.consumeToken(selectorSymbolString)
	return token.Text
}

// currentToken resturns the current token, fetching it from the lexer if needed.
func (t *selectorParserTask) currentToken() *selectorToken {
	t.ensureToken()
	return t.token
}

// fetchToken discard the current token and fetches a new one from the lexer.
func (t *selectorParserTask) fetchToken() {
	token, err := t.lexer.FetchToken()
	if err != nil {
		panic(err)
	}
	t.token = token
}

// checkToken returns true if the current token has the given symbol.
func (t *selectorParserTask) checkToken(symbol selectorSymbol) bool {
	t.ensureToken()
	return t.token.Symbol == symbol
}

// consumeToken checks that the symbol of the current token and then discards it, so that the next
// time that a token is needed a new one will be fetched from the lexer. If the symbol is not the
// given one then it panics.
func (t *selectorParserTask) consumeToken(symbol selectorSymbol) {
	t.ensureToken()
	if t.token.Symbol != symbol {
		var expected string
		switch symbol {
		case selectorSymbolEnd:
			expected = "end of input"
		case selectorSymbolLeftParenthesis:
			expected = "left parenthesis"
		case selectorSymbolRightParenthesis:
			expected = "right parenthesis"
		case selectorSymbolIdentifier:
			expected = "identifier"
		case selectorSymbolComma:
			expected = "comma"
		case selectorSymbolSlash:
			expected = "slash"
		case selectorSymbolSemicolon:
			expected = "semicolon"
		case selectorSymbolString:
			expected = "string"
		}
		panic(fmt.Errorf(
			"unexpected token '%s' while expecting %s",
			t.token.Text, expected,
		))
	}
	t.token = nil
}

// ensureToken makes sure the current token is populated, fetching it from the lexer if needed.
func (t *selectorParserTask) ensureToken() {
	if t.token == nil {
		t.fetchToken()
	}
}
