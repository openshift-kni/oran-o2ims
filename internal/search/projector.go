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

import "slices"

// Projector defines how to remove fields from an object.
type Projector struct {
	// Include is the list of paths that will be included in the result when the projector
	// is evaluated. An empty list means that all paths will be included.
	Include []Path

	// Exclude is the list of paths that will be excluded from the result when the projector
	// is evaluated. An empty list means that no path will be excluded.
	Exclude []Path
}

// Empty returns true iif the projector has no include or exclude paths.
func (p *Projector) Empty() bool {
	return p == nil || (len(p.Include) == 0 && len(p.Exclude) == 0)
}

// Clone creates a deep copy of the projector.
func (p *Projector) Clone() *Projector {
	if p == nil {
		return nil
	}
	clone := &Projector{}
	if p.Include != nil {
		clone.Include = make([]Path, len(p.Include))
		for i, path := range p.Include {
			clone.Include[i] = slices.Clone(path)
		}
	}
	if p.Exclude != nil {
		clone.Exclude = make([]Path, len(p.Exclude))
		for i, path := range p.Exclude {
			clone.Exclude[i] = slices.Clone(path)
		}
	}
	return clone
}
