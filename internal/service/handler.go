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

package service

import (
	"net/http"

	"github.com/jhernand/o2ims/internal/filter"
)

// Request represents a request that includes an optional filter expression.
type Request struct {
	HTTP   *http.Request
	Filter *filter.Expr
}

// Handler is the interface implemented by objects that know how to handle HTTP requests containing
// filter expressions.
type Handler interface {
	Serve(w http.ResponseWriter, r *Request)
}
