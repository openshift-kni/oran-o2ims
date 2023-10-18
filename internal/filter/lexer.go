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

package filter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"unicode"
)

// exprLexerBuilder contains the data and logic needed to create a new lexical scanner for filter
// expressions. Don't create instances of this directly, use the newExprLexer function instead.
type exprLexerBuilder struct {
	logger *slog.Logger
	source string
}

// exprLexer is a lexical scanner for the filter expression language. Don't create instances of
// this type directly, use the newExprLexer function instead.
type exprLexer struct {
	logger *slog.Logger
	mode   exprLexerMode
	buffer *bytes.Buffer
}

// exprSymbol represents the terminal symbols of the expression filter language.
type exprSymbol int

const (
	exprSymbolEnd exprSymbol = iota
	exprSymbolLeftParenthesis
	exprSymbolRightParenthesis
	exprSymbolIdentifier
	exprSymbolComma
	exprSymbolSlash
	exprSymbolSemicolon
	exprSymbolString
)

// String generates a string representation of the terminal symbol.
func (s exprSymbol) String() string {
	switch s {
	case exprSymbolEnd:
		return "End"
	case exprSymbolLeftParenthesis:
		return "LeftParenthesis"
	case exprSymbolRightParenthesis:
		return "RightParenthesis"
	case exprSymbolIdentifier:
		return "Identifier"
	case exprSymbolComma:
		return "Comma"
	case exprSymbolSlash:
		return "Slash"
	case exprSymbolSemicolon:
		return "Semicolon"
	case exprSymbolString:
		return "String"
	default:
		return fmt.Sprintf("Unknown:%d", s)
	}
}

// exprToken represents the tokens returned by the lexical scanner. Each token contains the
// terminal symbol and its text.
type exprToken struct {
	Symbol exprSymbol
	Text   string
}

// String geneates a string representation of the token.
func (t *exprToken) String() string {
	if t == nil {
		return "Nil"
	}
	switch t.Symbol {
	case exprSymbolIdentifier:
		return fmt.Sprintf("%s:%s", t.Symbol, t.Text)
	default:
		return t.Symbol.String()
	}
}

// expreLexerMode represents the mode of the lexer. We need two modes because string literals are
// treated differently when they are values: quoting them is optional, so there is no way from the
// parser distinguish an identifier from a string literal. To address that the parser will
// explicitly change the mode when it expects values instead of identifiers.
type exprLexerMode int

const (
	// exprDefaultMode is used by default when the parser expects identifiers.
	exprDefaultMode exprLexerMode = iota

	// exprValuesMode is used when the parser expects values instead of identifiers.
	exprValuesMode
)

// String generates a string representation of the mode.
func (m exprLexerMode) String() string {
	switch m {
	case exprDefaultMode:
		return "Default"
	case exprValuesMode:
		return "Values"
	default:
		return fmt.Sprintf("Unknown:%d", m)
	}
}

// newExprLexer creates a builder that can then be used to configure and create lexers.
func newExprLexer() *exprLexerBuilder {
	return &exprLexerBuilder{}
}

// SetLogger sets the logger that the lexer will use to write log messesages. This is mandatory.
func (b *exprLexerBuilder) SetLogger(value *slog.Logger) *exprLexerBuilder {
	b.logger = value
	return b
}

// SetSource sets the source string to parse. This is mandatory.
func (b *exprLexerBuilder) SetSource(value string) *exprLexerBuilder {
	b.source = value
	return b
}

// Build uses the data stored in the builder to create a new lexer.
func (b *exprLexerBuilder) Build() (result *exprLexer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if len(b.source) == 0 {
		err = errors.New("source is mandatory")
		return
	}

	// Create and populate the object:
	result = &exprLexer{
		logger: b.logger,
		mode:   exprDefaultMode,
		buffer: bytes.NewBufferString(b.source),
	}
	return
}

// SetMode sets the mode. This will be called by the parser to explicitly change the mode.
func (l *exprLexer) SetMode(mode exprLexerMode) {
	l.mode = mode
}

// FetchToken fetches the next token from the source.
func (l *exprLexer) FetchToken() (token *exprToken, err error) {
	switch l.mode {
	case exprDefaultMode:
		token, err = l.fetchInDefaultMode()
	case exprValuesMode:
		token, err = l.fetchInValuesMode()
	default:
		err = fmt.Errorf("unknown mode %d", l.mode)
	}
	if token != nil {
		l.logger.Debug(
			"Fetched token",
			slog.String("mode", l.mode.String()),
			slog.String("symbol", token.Symbol.String()),
			slog.String("lexeme", token.Text),
		)
	}
	return
}

func (l *exprLexer) fetchInDefaultMode() (token *exprToken, err error) {
	type State int
	const (
		S0 State = iota
		S1
		S2
	)
	state := S0
	lexeme := &bytes.Buffer{}
	for {
		r := l.readRune()
		switch state {
		case S0:
			switch {
			case unicode.IsSpace(r):
				state = S0
			case unicode.IsLetter(r) || r == '_' || r == '@':
				lexeme.WriteRune(r)
				state = S1
			case r == '(':
				token = &exprToken{
					Symbol: exprSymbolLeftParenthesis,
					Text:   "(",
				}
				return
			case r == ')':
				token = &exprToken{
					Symbol: exprSymbolRightParenthesis,
					Text:   ")",
				}
				return
			case r == ',':
				token = &exprToken{
					Symbol: exprSymbolComma,
					Text:   ",",
				}
				return
			case r == '/':
				token = &exprToken{
					Symbol: exprSymbolSlash,
					Text:   "/",
				}
				return
			case r == ';':
				token = &exprToken{
					Symbol: exprSymbolSemicolon,
					Text:   ";",
				}
				return
			case r == '~':
				state = S2
			case r == 0:
				token = &exprToken{
					Symbol: exprSymbolEnd,
				}
				return
			default:
				err = fmt.Errorf(
					"unexpected character '%c' while expecting start of "+
						"identifier",
					r,
				)
				return
			}
		case S1:
			switch {
			case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
				lexeme.WriteRune(r)
				state = S1
			case r == '~':
				state = S2
			default:
				l.unreadRune()
				token = &exprToken{
					Symbol: exprSymbolIdentifier,
					Text:   lexeme.String(),
				}
				return
			}
		case S2:
			switch r {
			case '0':
				lexeme.WriteRune('~')
				state = S0
			case '1':
				lexeme.WriteRune('/')
				state = S0
			case 'b':
				lexeme.WriteRune('@')
				state = S0
			default:
				err = fmt.Errorf(
					"unknown escape sequence '~%c', valid escape sequences "+
						"are '~0' for '/', '~' for '/' and '~b' for '@'",
					r,
				)
				return
			}
		default:
			err = fmt.Errorf("unknown state %d", state)
			return
		}
	}
}

func (l *exprLexer) fetchInValuesMode() (token *exprToken, err error) {
	type State int
	const (
		S0 State = iota
		S1
		S2
		S3
		S4
	)
	state := S0
	lexeme := &bytes.Buffer{}
	for {
		r := l.readRune()
		switch state {
		case S0:
			switch {
			case unicode.IsSpace(r):
				lexeme.WriteRune(r)
				state = S1
			case r == '\'':
				lexeme.Reset()
				state = S3
			case r == ',':
				token = &exprToken{
					Symbol: exprSymbolComma,
					Text:   ",",
				}
				return
			case r == ')':
				token = &exprToken{
					Symbol: exprSymbolRightParenthesis,
					Text:   ")",
				}
				return
			case r == 0:
				err = fmt.Errorf(
					"unexpected end of input while expecting start of " +
						"value, comma or right parenthesis",
				)
				return
			default:
				lexeme.WriteRune(r)
				state = S2
			}
		case S1:
			switch {
			case unicode.IsSpace(r):
				lexeme.WriteRune(r)
				state = S1
			case r == '\'':
				lexeme.Reset()
				state = S3
			case r == ',' || r == ')':
				l.unreadRune()
				token = &exprToken{
					Symbol: exprSymbolString,
					Text:   lexeme.String(),
				}
				return
			case r == 0:
				err = fmt.Errorf(
					"unexpected end of input while expecting continuation of " +
						"value, comma or right parenthesis",
				)
				return
			default:
				lexeme.WriteRune(r)
				state = S2
			}
		case S2:
			switch {
			case r == ',' || r == ')':
				l.unreadRune()
				token = &exprToken{
					Symbol: exprSymbolString,
					Text:   lexeme.String(),
				}
				lexeme.Reset()
				return
			case r == 0:
				err = fmt.Errorf(
					"unexpected end of input while expecting right parenthesis",
				)
				return
			default:
				lexeme.WriteRune(r)
				state = S2
			}
		case S3:
			switch {
			case r == '\'':
				state = S4
			case r == 0:
				err = fmt.Errorf("end of input inside quoted string")
				return
			default:
				lexeme.WriteRune(r)
				state = S3
			}
		case S4:
			switch {
			case unicode.IsSpace(r):
				state = S4
			case r == ',' || r == ')':
				l.unreadRune()
				token = &exprToken{
					Symbol: exprSymbolString,
					Text:   lexeme.String(),
				}
				return
			case r == '\'':
				lexeme.WriteRune('\'')
				state = S3
			case r == 0:
				err = fmt.Errorf("end of input inside quoted string")
				return
			default:
				lexeme.WriteRune(r)
				state = S3
			}
		default:
			err = fmt.Errorf("unknown state %d", state)
			return
		}
	}
}

func (l *exprLexer) readRune() rune {
	r, _, err := l.buffer.ReadRune()
	if errors.Is(err, io.EOF) {
		return 0
	}
	if err != nil {
		l.logger.Error(
			"Unexpected error while reading rune",
			"error", err,
		)
		return 0
	}
	return r
}

func (l *exprLexer) unreadRune() {
	err := l.buffer.UnreadRune()
	if err != nil {
		l.logger.Error(
			"Unexpected error while unreading rune",
			"error", err,
		)
	}
}
