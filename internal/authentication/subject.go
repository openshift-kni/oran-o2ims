/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

// This file contains functions that extract information from the context.

package authentication

// Subject represents an entity, such as person or a service account.
type Subject struct {
	// Token is the raw token.
	Token string

	// Name is the name of the subject, typically extracted from the 'sub' claim.
	Name string

	// Claims is the complete set of claims extracted from the token.
	Claims map[string]any
}
