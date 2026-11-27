package spec

import (
	"reflect"
	"slices"
	"strings"
)

// Segment represents a single segment as defined in [RFC 9535 Section 1.4.2],
// consisting of a list of [Selector] values.
//
// [RFC 9535 Section 1.4.2]: https://www.rfc-editor.org/rfc/rfc9535.html#name-segments
type Segment struct {
	selectors  []Selector
	descendant bool
}

// Child creates and returns a [Segment] that uses sel to select values from a
// JSON object or array.
func Child(sel ...Selector) *Segment {
	return &Segment{selectors: sel}
}

// Descendant creates and returns a [Segment] that uses sel to select values
// from a JSON object or array or any of its descendant objects and arrays.
func Descendant(sel ...Selector) *Segment {
	return &Segment{selectors: sel, descendant: true}
}

// Selectors returns s's [Selector] values.
func (s *Segment) Selectors() []Selector {
	return s.selectors
}

// String returns a string representation of seg. A [Child] [Segment]
// formats as:
//
//	[<selectors>]
//
// A [Descendant] [Segment] formats as:
//
//	..⁠[<selectors>])
func (s *Segment) String() string {
	buf := new(strings.Builder)
	if s.descendant {
		buf.WriteString("..")
	}
	buf.WriteByte('[')
	for i, sel := range s.selectors {
		if i > 0 {
			buf.WriteByte(',')
		}
		sel.writeTo(buf)
	}
	buf.WriteByte(']')
	return buf.String()
}

// Select selects and returns values from current or root, for each of s's
// selectors. Defined by the [Selector] interface.
func (s *Segment) Select(current, root any) []any {
	ret := make([]any, 0, len(s.selectors))
	for _, sel := range s.selectors {
		ret = append(ret, sel.Select(current, root)...)
	}
	if s.descendant {
		ret = append(ret, s.descend(current, root)...)
	}
	return slices.Clip(ret)
}

// SelectLocated selects and returns values as [LocatedNode] values from
// current or root for each of seg's selectors. Defined by the [Selector]
// interface.
func (s *Segment) SelectLocated(current, root any, parent NormalizedPath) []*LocatedNode {
	ret := make([]*LocatedNode, 0, len(s.selectors))
	for _, sel := range s.selectors {
		ret = append(ret, sel.SelectLocated(current, root, parent)...)
	}
	if s.descendant {
		ret = append(ret, s.descendLocated(current, root, parent)...)
	}
	return slices.Clip(ret)
}

// descend recursively executes [Segment.Select] for each value in current
// and/or root and its descendants and returns the results.
func (s *Segment) descend(current, root any) []any {
	switch val := current.(type) {
	case []any:
		ret := make([]any, 0, len(val))
		for _, v := range val {
			ret = append(ret, s.Select(v, root)...)
		}
		return slices.Clip(ret)
	case map[string]any:
		ret := make([]any, 0, len(val))
		for _, v := range val {
			ret = append(ret, s.Select(v, root)...)
		}
		return slices.Clip(ret)
	default:
		value := reflect.ValueOf(current)
		switch value.Kind() {
		case reflect.Slice:
			// Descend into any other slice that contains slices or maps.
			switch value.Type().Elem().Kind() {
			case reflect.Slice, reflect.Map:
				ret := make([]any, 0, value.Len())
				for i := range value.Len() {
					ret = append(ret, s.Select(value.Index(i).Interface(), root)...)
				}
				return slices.Clip(ret)
			default:
				return make([]any, 0)
			}
		case reflect.Map:
			// Descend into any map[string]* that contains slices or maps.
			if value.Type().Key().Kind() != reflect.String {
				return make([]any, 0)
			}
			switch value.Type().Elem().Kind() {
			case reflect.Slice, reflect.Map:
				ret := make([]any, 0, value.Len())
				for _, k := range value.MapKeys() {
					ret = append(ret, s.Select(value.MapIndex(k).Interface(), root)...)
				}
				return slices.Clip(ret)
			default:
				return make([]any, 0)
			}
		default:
			return make([]any, 0)
		}
	}
}

// descend recursively executes [q] for each value in current and/or root and
// its descendants and returns the results.
func (s *Segment) descendLocated(current, root any, parent NormalizedPath) []*LocatedNode {
	switch val := current.(type) {
	case []any:
		ret := make([]*LocatedNode, 0, len(val))
		for i, v := range val {
			ret = append(ret, s.SelectLocated(v, root, append(parent, Index(i)))...)
		}
		return slices.Clip(ret)
	case map[string]any:
		ret := make([]*LocatedNode, 0, len(val))
		for k, v := range val {
			ret = append(ret, s.SelectLocated(v, root, append(parent, Name(k)))...)
		}
		return slices.Clip(ret)
	default:
		value := reflect.ValueOf(current)
		switch value.Kind() {
		case reflect.Slice:
			// Descend into any other slice that contains slices or maps.
			switch value.Type().Elem().Kind() {
			case reflect.Slice, reflect.Map:
				ret := make([]*LocatedNode, 0, value.Len())
				for i := range value.Len() {
					ret = append(ret, s.SelectLocated(
						value.Index(i).Interface(), root, append(parent, Index(i)),
					)...)
				}
				return slices.Clip(ret)
			default:
				return make([]*LocatedNode, 0)
			}
		case reflect.Map:
			// Descend into any map[string]* that contains slices or maps.
			if value.Type().Key().Kind() != reflect.String {
				return make([]*LocatedNode, 0)
			}
			switch value.Type().Elem().Kind() {
			case reflect.Slice, reflect.Map:
				ret := make([]*LocatedNode, 0, value.Len())
				for _, k := range value.MapKeys() {
					ret = append(ret, s.SelectLocated(
						value.MapIndex(k).Interface(), root, append(parent, Name(k.String())),
					)...)
				}
				return slices.Clip(ret)
			default:
				return make([]*LocatedNode, 0)
			}
		default:
			return make([]*LocatedNode, 0)
		}
	}
}

// isSingular returns true if the segment selects at most one node. Defined by
// the [Selector] interface.
func (s *Segment) isSingular() bool {
	if s.descendant || len(s.selectors) != 1 {
		return false
	}
	return s.selectors[0].isSingular()
}

// IsDescendant returns true if the segment is a [Descendant] selector that
// recursively select the children of a JSON value.
func (s *Segment) IsDescendant() bool { return s.descendant }
