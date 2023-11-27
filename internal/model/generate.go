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

package model

// The models used by the ACM search GraphQL server are part of the 'stolostron/search-v2-api'
// project, but we don't want to import it because that would bring many other dependencies
// that may be problematic in the future. Instead of that we copy that code into our project.
//
//go:generate curl -Lso searchapi.go https://raw.githubusercontent.com/stolostron/search-v2-api/234de1a26d82b3de2af74cf21aa1226db8e5f26a/graph/model/models_gen.go
