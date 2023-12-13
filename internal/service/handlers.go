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

//go:generate mockgen -source=handlers.go -package=service -destination=handlers_mock.go
package service

import (
	"context"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

// ListRequest represents a request for a collection of objects.
type ListRequest struct {
	// Variables contains the values of the path variables. For example, if the request path is
	// like this:
	//
	//	/o2ims-infrastructureInventory/v1/resourcePools/123/resources
	//
	// Then it will contain '123'.
	//
	// These path variables are ordered from more specific to less specific, the opposite of
	// what appears in the request path. This is intended to simplify things because most
	// handlers will only be interested in the most specific variable and therefore they
	// can just use index zero.
	//
	// This will be empty for top level collections, where there are no path variables, for
	// example:
	//
	//	/o2ims-infrastructureInventory/v1/resourcePools
	Variables []string

	// Selector selects the objects to return.
	Selector *search.Selector

	// Projector is the list of field paths to return.
	Projector *search.Projector
}

// ListResponse represents the response to the request to get the list of items of a collection.
type ListResponse struct {
	Items data.Stream
}

// GetRequest represents a request for an individual object.
type GetRequest struct {
	// Variables contains the values of the path variables. For example, if the request path is
	// like this:
	//
	//	/o2ims-infrastructureInventory/v1/resourcePools/123/resources/456
	//
	// Then it will contain '456' and '123'.
	//
	// These path variables are ordered from more specific to less specific, the opposite of
	// what appears in the request path. This is intended to simplify things because most
	// handlers will only be interested in the most specific identifier and therefore they
	// can just use index zero.
	Variables []string

	// Projector describes how to remove fields from the result.
	Projector *search.Projector
}

// GetResponse represents the response to the request to get an individual object.
type GetResponse struct {
	Object data.Object
}

// CollectionHandler is the interface implemented by objects that know how to handle requests to
// list the items of a collection of objects.
type CollectionHandler interface {
	List(ctx context.Context, request *ListRequest) (response *ListResponse, err error)
}

// ObjectHandler is the interface implemented by objects that now how to handle requests to get the
// details of an object.
type ObjectHandler interface {
	Get(ctx context.Context, request *GetRequest) (response *GetResponse, err error)
}

// Handler is the interface implemented by objects that knows how to handle requests to list the
// items of a collection, as well as requests to get a specific object.
type Handler interface {
	CollectionHandler
	ObjectHandler
}
