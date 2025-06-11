/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"unicode"
)

// selectorLexerBuilder contains the data and logic needed to create a new lexical scanner for
// filter expressions. Don't create instances of this directly, use the newSelectorLexer function
// instead.
type selectorLexerBuilder struct {
	logger *slog.Logger
	source string
}

// selectorLexer is a lexical scanner for the filter expression language. Don't create instances of
// this type directly, use the newSelectorLexer function instead.
type selectorLexer struct {
	logger *slog.Logger
	mode   selectorLexerMode
	buffer *bytes.Buffer
}

// selectorSymbol represents the terminal symbols of the expression filter language.
type selectorSymbol int

const (
	selectorSymbolEnd selectorSymbol = iota
	selectorSymbolLeftParenthesis
	selectorSymbolRightParenthesis
	selectorSymbolIdentifier
	selectorSymbolComma
	selectorSymbolSlash
	selectorSymbolSemicolon
	selectorSymbolString
)

// String generates a string representation of the terminal symbol.
func (s selectorSymbol) String() string {
	switch s {
	case selectorSymbolEnd:
		return "End"
	case selectorSymbolLeftParenthesis:
		return "LeftParenthesis"
	case selectorSymbolRightParenthesis:
		return "RightParenthesis"
	case selectorSymbolIdentifier:
		return "Identifier"
	case selectorSymbolComma:
		return "Comma"
	case selectorSymbolSlash:
		return "Slash"
	case selectorSymbolSemicolon:
		return "Semicolon"
	case selectorSymbolString:
		return "String"
	default:
		return fmt.Sprintf("Unknown:%d", s)
	}
}

// selectorToken represents the tokens returned by the lexical scanner. Each token contains the
// terminal symbol and its text.
type selectorToken struct {
	Symbol selectorSymbol
	Text   string
}

// String geneates a string representation of the token.
func (t *selectorToken) String() string {
	if t == nil {
		return "Nil"
	}
	switch t.Symbol {
	case selectorSymbolIdentifier:
		return fmt.Sprintf("%s:%s", t.Symbol, t.Text)
	default:
		return t.Symbol.String()
	}
}

// selectorLexerMode represents the mode of the lexer. We need two modes because string literals are
// treated differently when they are values: quoting them is optional, so there is no way from the
// parser distinguish an identifier from a string literal. To address that the parser will
// explicitly change the mode when it expects values instead of identifiers.
type selectorLexerMode int

const (
	// selectorLexerDefaultMode is used by default when the parser expects identifiers.
	selectorLexerDefaultMode selectorLexerMode = iota

	// selectorLexerValuesMode is used when the parser expects values instead of identifiers.
	selectorLexerValuesMode
)

// String generates a string representation of the mode.
func (m selectorLexerMode) String() string {
	switch m {
	case selectorLexerDefaultMode:
		return "Default"
	case selectorLexerValuesMode:
		return "Values"
	default:
		return fmt.Sprintf("Unknown:%d", m)
	}
}

// newSelectorLexer creates a builder that can then be used to configure and create lexers.
func newSelectorLexer() *selectorLexerBuilder {
	return &selectorLexerBuilder{}
}

// SetLogger sets the logger that the lexer will use to write log messesages. This is mandatory.
func (b *selectorLexerBuilder) SetLogger(value *slog.Logger) *selectorLexerBuilder {
	b.logger = value
	return b
}

// SetSource sets the source string to parse. This is mandatory.
func (b *selectorLexerBuilder) SetSource(value string) *selectorLexerBuilder {
	b.source = value
	return b
}

// Build uses the data stored in the builder to create a new lexer.
func (b *selectorLexerBuilder) Build() (result *selectorLexer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.source == "" {
		err = errors.New("source is mandatory")
		return
	}

	// Create and populate the object:
	result = &selectorLexer{
		logger: b.logger,
		mode:   selectorLexerDefaultMode,
		buffer: bytes.NewBufferString(b.source),
	}
	return
}

// SetMode sets the mode. This will be called by the parser to explicitly change the mode.
func (l *selectorLexer) SetMode(mode selectorLexerMode) {
	l.mode = mode
}

// FetchToken fetches the next token from the source.
func (l *selectorLexer) FetchToken() (token *selectorToken, err error) {
	switch l.mode {
	case selectorLexerDefaultMode:
		token, err = l.fetchInDefaultMode()
	case selectorLexerValuesMode:
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

func (l *selectorLexer) fetchInDefaultMode() (token *selectorToken, err error) {
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
				token = &selectorToken{
					Symbol: selectorSymbolLeftParenthesis,
					Text:   "(",
				}
				return
			case r == ')':
				token = &selectorToken{
					Symbol: selectorSymbolRightParenthesis,
					Text:   ")",
				}
				return
			case r == ',':
				token = &selectorToken{
					Symbol: selectorSymbolComma,
					Text:   ",",
				}
				return
			case r == '/':
				token = &selectorToken{
					Symbol: selectorSymbolSlash,
					Text:   "/",
				}
				return
			case r == ';':
				token = &selectorToken{
					Symbol: selectorSymbolSemicolon,
					Text:   ";",
				}
				return
			case r == '~':
				state = S2
			case r == 0:
				token = &selectorToken{
					Symbol: selectorSymbolEnd,
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
				token = &selectorToken{
					Symbol: selectorSymbolIdentifier,
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

func (l *selectorLexer) fetchInValuesMode() (token *selectorToken, err error) {
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
				token = &selectorToken{
					Symbol: selectorSymbolComma,
					Text:   ",",
				}
				return
			case r == ')':
				token = &selectorToken{
					Symbol: selectorSymbolRightParenthesis,
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
				token = &selectorToken{
					Symbol: selectorSymbolString,
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
				token = &selectorToken{
					Symbol: selectorSymbolString,
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
				token = &selectorToken{
					Symbol: selectorSymbolString,
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

func (l *selectorLexer) readRune() rune {
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

func (l *selectorLexer) unreadRune() {
	err := l.buffer.UnreadRune()
	if err != nil {
		l.logger.Error(
			"Unexpected error while unreading rune",
			"error", err,
		)
	}
}
