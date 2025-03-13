/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package search

import (
	"slices"
)

// Path represents the path of an attribute inside a complex data type. Each value of the slice is
// an attribute name, starting with the outermost field name.
type Path []string

// Clone creates a deep copy of the path.
func (p Path) Clone() Path {
	return slices.Clone(p)
}
