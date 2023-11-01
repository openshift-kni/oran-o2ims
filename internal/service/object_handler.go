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

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

// ObjectHandler represents a request for an individual object.
type ObjectRequest struct {
	ID string
}

// ObjectResponse represents the response to the request to get an individual object.
type ObjectResponse struct {
	Object data.Object
}

// ObjectHandler is the interface implemented by objects that know how to handle requests to
// get individual objects.
//
//go:generate mockgen -source=object_handler.go -package=service -destination=object_handler_mock.go
type ObjectHandler interface {
	Get(ctx context.Context, request *ObjectRequest) (response *ObjectResponse, err error)
}
