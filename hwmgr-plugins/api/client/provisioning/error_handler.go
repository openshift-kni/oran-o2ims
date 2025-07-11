/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package provisioning

import (
	"fmt"
	"log/slog"
	"net/http"
)

// handleErrorResponse processes error responses and returns a formatted error
func (h *HardwarePluginClient) handleErrorResponse(
	responseStatus string,
	problem *ProblemDetails,
	resourceType, resourceID, action string,
) error {
	logger := h.logger.With(
		slog.String("resourceType", resourceType),
		slog.String("resourceID", resourceID),
		slog.String("action", action),
		slog.String("status", responseStatus),
	)

	if problem == nil || problem.Detail == "" {
		logger.Error("Received empty or unexpected error response")
		return fmt.Errorf("empty or unexpected error response for %s '%s': %s", resourceType, resourceID, responseStatus)
	}

	logger.Error("Failed to process request", slog.String("detail", problem.Detail))
	return fmt.Errorf("failed to %s %s '%s': %s - %s", action, resourceType, resourceID, responseStatus, problem.Detail)
}

// getProblemDetails extracts problem details based on status code
//
//nolint:gocyclo
func (h *HardwarePluginClient) getProblemDetails(
	response interface{},
	statusCode int,
) (*ProblemDetails, string) {
	switch resp := response.(type) {
	case *GetAllVersionsResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetAllocatedNodesResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetAllocatedNodeResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetMinorVersionsResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetNodeAllocationRequestsResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *CreateNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *DeleteNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *UpdateNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	case *GetAllocatedNodesFromNodeAllocationRequestResponse:
		switch statusCode {
		case http.StatusBadRequest:
			return resp.ApplicationProblemJSON400, MsgBadRequest
		case http.StatusUnauthorized:
			return resp.ApplicationProblemJSON401, MsgUnauthorized
		case http.StatusForbidden:
			return resp.ApplicationProblemJSON403, MsgForbidden
		case http.StatusNotFound:
			return resp.ApplicationProblemJSON404, MsgNotFound
		case http.StatusInternalServerError:
			return resp.ApplicationProblemJSON500, MsgInternalServerError
		}
	}
	return nil, http.StatusText(statusCode)
}
