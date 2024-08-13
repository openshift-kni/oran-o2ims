/*
Copyright 2024 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

// This file contains the type that describes trees of URL paths used to translate request paths
// into labes suitalbe for use as Prometheus labels.

package metrics

import (
	"strings"
)

// pathTree defines a tree of URL paths that will be used to transform request paths into labels
// suitable for use in Prometheus metrics. For example, a server that has these URL paths:
//
//	/api
//	/api/my
//	/api/my/v1
//	/api/my/v1/resources
//	/api/my/v1/resources/{resource_id}
//	/api/my/v1/resources/{resource_id}/groups
//	/api/my/v1/resources/{resource_id}/groups/{group_id}
//
// Will be described with a tree like this:
//
//	var pathRoot = pathTree{
//		"api": {
//			"my": {
//				"v1": {
//					"resources": {
//						"-": {
//							"groups": {
//								"-": nil,
//							},
//						},
//					},
//				},
//			},
//		},
//	}
//
// Path variables are represented with a dash.
type pathTree map[string]pathTree

// add adds the given branch to this tree.
func (t pathTree) add(path string) {
	path = t.clean(path)
	if path == "" {
		return
	}
	segments := strings.Split(path, "/")
	t.addSegments(segments)
}

func (t pathTree) addSegments(segments []string) {
	if len(segments) == 0 {
		return
	}
	head := segments[0]
	tail := segments[1:]
	next := t[head]
	if next == nil {
		if len(tail) > 0 {
			next = pathTree{}
		}
		t[head] = next
	}
	next.addSegments(tail)
}

func (t pathTree) clean(path string) string {
	for path != "" && strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	for path != "" && strings.HasSuffix(path, "/") {
		path = path[0 : len(path)-1]
	}
	return path
}
