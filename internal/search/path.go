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
	"slices"
)

// Path represents the path of an attribute inside a complex data type. Each value of the slice is
// an attribute name, starting with the outermost field name.
type Path []string

// Clone creates a deep copy of the path.
func (p Path) Clone() Path {
	return slices.Clone(p)
}
