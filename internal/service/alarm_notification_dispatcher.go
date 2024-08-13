package service

import (
	"bytes"
	"context"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
)

// Add is the implementation of the object handler ADD interface.
// receive obsability alarm post and trigger the alarms

func (h *AlarmNotificationHandler) add(ctx context.Context,
	request *AddRequest) (response *AddResponse, err error) {

	h.logger.Debug(
		"AlarmNotificationHandler Add",
	)

	// Following needs to be test alert
	// Map iterm

	// map/translate alert to alarm

	alarmEventRecord, err := h.alarmMapper.MapItem(ctx, request.Object)
	if err != nil {
		h.logger.Debug(
			"AlarmNotificationHandler failed to map the item",
			slog.String("error", err.Error()),
		)
		return
	}

	debug, err := h.jsonAPI.MarshalIndent(&alarmEventRecord, "", " ")
	if err == nil {
		h.logger.Debug(
			"AlarmNotificationHandler alarmEventRecord",
			slog.String("packet", string(debug)),
		)
	}

	subIdSet := h.getSubscriptionIdsFromAlarm(ctx, alarmEventRecord)

	// now look up subscriptions id_set matched and send http packets to URIs
	for key := range subIdSet {
		subInfo, ok := h.getSubscriptionInfo(ctx, key)

		if !ok {
			h.logger.Debug(
				"AlarmNotificationHandler failed to get subinfo key",
				slog.String(": ", key),
			)

			continue
		}

		var obj data.Object
		// TODO:
		// determine if alarmNotificationType needs to be added

		err = h.jqTool.Evaluate(
			`{
				"alarmSubscriptionId": $subId,
				"consumerSubscriptionId": $alarmConsumerSubId,
				"objectRef": $objRef,
				"alarmEventRecord": $alarmEvent
			}`,
			alarmEventRecord, &obj,
			jq.String("$alarmConsumerSubId", subInfo.consumerSubscriptionId),
			jq.String("$objRef", alarmEventRecord["alarmEventRecordId"].(string)),
			jq.String("$subId", key),
			jq.Any("$alarmEvent", alarmEventRecord),
		)

		// following function will send out the notification packet based on subscription
		// and alert received in a go route thread that will not hold the AddRequest ctx.
		go func(pkt data.Object) {
			content, err := h.jsonAPI.MarshalIndent(&pkt, "", " ")
			if err != nil {
				h.logger.Debug(
					"AlarmNotificationHandler failed to get content of new packet",
					slog.String("error", err.Error()),
				)
			}

			// following new buffer usage may need optimization
			resp, err := h.httpClient.Post(subInfo.uris, "application/json", bytes.NewBuffer(content))
			if err != nil {
				h.logger.Debug("AlarmNotificationHandler failed to post packet",
					slog.String("error", err.Error()),
				)
				return
			}

			defer resp.Body.Close()
		}(obj)

	}

	response = &AddResponse{}
	return
}

func (h *AlarmNotificationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check that the content type is acceptable:
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		h.logger.ErrorContext(
			ctx,
			"Received empty content type header",
		)
		SendError(
			w, http.StatusBadRequest,
			"Content type is mandatory, use 'application/json'",
		)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to parse content type",
			slog.String("header", contentType),
			slog.String("error", err.Error()),
		)
		SendError(w, http.StatusBadRequest, "Failed to parse content type '%s'", contentType)
	}
	if !strings.EqualFold(mediaType, "application/json") {
		h.logger.ErrorContext(
			ctx,
			"Unsupported content type",
			slog.String("header", contentType),
			slog.String("media", mediaType),
		)
		SendError(
			w, http.StatusBadRequest,
			"Content type '%s' isn't supported, use 'application/json'",
			mediaType,
		)
		return
	}
	// Parse the request body:
	decoder := h.jsonAPI.NewDecoder(r.Body)
	var object data.Object
	err = decoder.Decode(&object)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to decode input",
			slog.String("error", err.Error()),
		)
		SendError(w, http.StatusBadRequest, "Failed to decode input")
		return
	}

	request := &AddRequest{
		Object: object,
	}

	response, err := h.add(ctx, request)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to add item",
			"error", err,
		)
		SendError(
			w,
			http.StatusInternalServerError,
			"Failed to add item",
		)
		return
	}

	// send response back
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writer := jsoniter.NewStream(h.jsonAPI, w, 0)
	writer.WriteVal(response.Object)
	if writer.Error != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to send object",
			"error", writer.Error.Error(),
		)
	}
	writer.Flush()
	if writer.Error != nil {
		h.logger.ErrorContext(
			ctx,
			"Failed to flush stream",
			"error", writer.Error.Error(),
		)
	}
}
