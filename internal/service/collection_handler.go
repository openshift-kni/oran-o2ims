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
	"context"

	"github.com/jhernand/o2ims/internal/data"
	"github.com/jhernand/o2ims/internal/search"
)

// CollectionRequest represents a request for a collection of objects.
type CollectionRequest struct {
	Filter   *search.Selector
	Selector [][]string
}

// CollectionResponse represents the response to the request to get the list of items of a collection.
type CollectionResponse struct {
	Items data.Stream
}

// CollectionHandler is the interface implemented by objects that know how to handle requests to
// list the items in a collection of objects.
//
//go:generate mockgen -source=collection_handler.go -package=service -destination=collection_handler_mock.go
type CollectionHandler interface {
	Get(ctx context.Context, request *CollectionRequest) (response *CollectionResponse, err error)
}
