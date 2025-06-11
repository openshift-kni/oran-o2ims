/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	"bytes"
	"fmt"
)

// Selector represents an attribute-based filter expression as defined in section 5.2 of
// [ETSI GS NFV-SOL 013].
//
// [ETSI GS NFV-SOL 013]: https://www.etsi.org/deliver/etsi_gs/NFV-SOL/001_099/013/03.03.01_60/gs_NFV-SOL013v030301p.pdf
type Selector struct {
	Terms []*Term
}

// String generates a string representation of the selector.
func (e *Selector) String() string {
	buffer := &bytes.Buffer{}
	for i, term := range e.Terms {
		if i > 0 {
			buffer.WriteString(";")
		}
		buffer.WriteString(term.String())
	}
	return buffer.String()
}

// Term is each of the terms that compose a elector The selector will match an object when all the
// terms match it -in other words, terms are connected by the and logical directive.
type Term struct {
	// Operator is the operator that used to compare the attribute to the value.
	Operator Operator

	// Path is the path of the attribute. This will always contain at least one segment. Each
	// segment is the name of the attribute. For example in a complex object like this:
	//
	//	{
	//		"extensions": {
	//			"user": "myuser",
	//			"password": "mypassword"
	//		}
	//	}
	//
	// The path to the attribute containing the password will be:
	//
	//	[]string{
	//		"extensions",
	//		"password",
	//	}
	Path []string

	// Values is the list of values that the attribute will be compared to. It will contain
	// only one value for unary operators like `eq` or `neq`, and multiple values for
	// operators like `in` or `nin`.
	Values []any
}

// String generates a string representation of the term.
func (t *Term) String() string {
	buffer := &bytes.Buffer{}
	buffer.WriteString("(")
	buffer.WriteString(t.Operator.String())
	buffer.WriteString(",")
	for i, segment := range t.Path {
		if i > 0 {
			buffer.WriteString("/")
		}
		buffer.WriteString(escapePathSegment(segment))
	}
	buffer.WriteString(",")
	for i, value := range t.Values {
		if i > 0 {
			buffer.WriteString(",")
		}
		text := fmt.Sprintf("%s", value)
		buffer.WriteString(escapePathSegment(escapeValue(text)))
	}
	buffer.WriteString(")")
	return buffer.String()
}

// Operator represents a comparison operator.
type Operator int

const (
	// Cont checks if the attribute is a string that contains the substring given as value.
	Cont Operator = iota

	// Eq check if the attribute is equal to the value.
	Eq

	// Gt checks if the attribute is greater than the value.
	Gt

	// Gte checks if the attribute is greater or equal than the value.
	Gte

	// In checks if the attribute is one of the values.
	In

	// Lt checks if the attribute is less than the value.
	Lt

	// Lte checks if the attribute is less or equal than the value.
	Lte

	// Ncont is the negation of `cont`. It checks if the attribute is a string that does not
	// contain the value.
	Ncont

	// Neq is the negation of `eq`. It checks if the attribute is not equal to the value.
	Neq

	// Nin is the negation of `in`. It checks if the attribute is not one of the values.
	Nin
)

// String generates a string representation of the operator. It panics if used on an unknown
// operator.
func (o Operator) String() string {
	switch o {
	case Cont:
		return "cont"
	case Eq:
		return "eq"
	case Gt:
		return "gt"
	case Gte:
		return "gte"
	case Lt:
		return "lt"
	case In:
		return "in"
	case Lte:
		return "lte"
	case Ncont:
		return "ncont"
	case Neq:
		return "neq"
	case Nin:
		return "nin"
	default:
		panic(fmt.Errorf(
			"unknown operator %d, valid operators are 'cont', 'eq', 'gt', 'gte', "+
				"'in', 'lt', 'lte', 'ncont', 'neq' and 'nin'",
			o,
		))
	}
}

// Key is the special path segment used to represent the name of an attribute.
const Key = "@key"

func escapePathSegment(segment string) string {
	if segment == Key {
		return segment
	}
	buffer := &bytes.Buffer{}
	buffer.Grow(len(segment))
	for _, r := range segment {
		switch r {
		case '~':
			buffer.WriteString("~0")
		case '/':
			buffer.WriteString("~1")
		case '@':
			buffer.WriteString("~b")
		default:
			buffer.WriteRune(r)
		}
	}
	return buffer.String()
}

func escapeValue(text string) string {
	buffer := &bytes.Buffer{}
	buffer.Grow(len(text) + 2)
	buffer.WriteString("'")
	for _, r := range text {
		if r == '\'' {
			buffer.WriteRune('\'')
		}
		buffer.WriteRune(r)
	}
	buffer.WriteString("'")
	return buffer.String()
}
