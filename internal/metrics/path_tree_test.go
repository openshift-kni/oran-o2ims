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

// This file contains tests for the URL path tree.

package metrics

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable(
	"Add",
	func(original string, paths []string, expected string) {
		var tree *pathTree
		err := json.Unmarshal([]byte(original), &tree)
		Expect(err).ToNot(HaveOccurred())
		for _, path := range paths {
			tree.add(path)
		}
		actual, err := json.Marshal(tree)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(MatchJSON(expected))
	},
	Entry(
		"Empty path",
		`{}`,
		[]string{
			``,
		},
		`{}`,
	),
	Entry(
		"Non existing path with one segment",
		`{}`,
		[]string{
			`/api`,
		},
		`{
			"api": null
		}`,
	),
	Entry(
		"Non existing path with two segments",
		`{}`,
		[]string{
			`/api/my`,
		},
		`{
			"api": {
				"my": null
			}
		}`,
	),
	Entry(
		"Non existing path with three segments",
		`{}`,
		[]string{
			`/api/my/v1`,
		},
		`{
			"api": {
				"my": {
					"v1": null
				}
			}
		}`,
	),
	Entry(
		"Existing path with one segment",
		`{
			"api": null
		}`,
		[]string{
			`/api`,
		},
		`{
			"api": null
		}`,
	),
	Entry(
		"Existing path with two segments",
		`{
			"api": {
				"my": null
			}
		}`,
		[]string{
			`/api/my`,
		},
		`{
			"api": {
				"my": null
			}
		}`,
	),
	Entry(
		"Existing path with three segments",
		`{
			"api": {
				"my": {
					"v1": null
				}
			}
		}`,
		[]string{
			`/api/my/v1`,
		},
		`{
			"api": {
				"my": {
					"v1": null
				}
			}
		}`,
	),
	Entry(
		"Appends to partially existing path",
		`{
			"api": null
		}`,
		[]string{
			`/api/my`,
		},
		`{
			"api": {
				"my": null
			}
		}`,
	),
	Entry(
		"Adds default token URL",
		`{
			"api": {
				"my": null
			}
		}`,
		[]string{
			`/auth/realms/redhat-external/protocol/openid-connect/token`,
		},
		`{
			"api": {
				"my": null
			},
			"auth": {
				"realms": {
					"redhat-external": {
						"protocol": {
							"openid-connect": {
								"token": null
							}
						}
					}
				}
			}
		}`,
	),
	Entry(
		"Merges prefix",
		`{
			"api": {
				"my": null
			}
		}`,
		[]string{
			`/api/my`,
			`/api/your`,
			`/api/their`,
		},
		`{
			"api": {
				"my": null,
				"your": null,
				"their": null
			}
		}`,
	),
)
