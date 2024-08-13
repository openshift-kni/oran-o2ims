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

package service

import (
	"context"
	"log/slog"

	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/jq"
	"github.com/openshift-kni/oran-o2ims/internal/search"
)

type subscriptionInfo struct {
	filters                search.Selector
	uris                   string
	consumerSubscriptionId string
}

// This file contains oran alarm notification serer searcher funcality for matching subscriptions
// at 1st step linear search

type alarmSubscriptionSearcherBuilder struct {
	logger *slog.Logger
	jqTool *jq.Tool
}

func newAlarmSubscriptionSearcherBuilder() *alarmSubscriptionSearcherBuilder {
	return &alarmSubscriptionSearcherBuilder{}
}

func (b *alarmSubscriptionSearcherBuilder) SetLogger(
	value *slog.Logger) *alarmSubscriptionSearcherBuilder {
	b.logger = value
	return b
}
func (b *alarmSubscriptionSearcherBuilder) SetJqTool(
	value *jq.Tool) *alarmSubscriptionSearcherBuilder {
	b.jqTool = value
	return b
}

type alarmSubscriptionSearcher struct {
	logger *slog.Logger
	jqTool *jq.Tool
	// map with prebuilt selector
	subscriptionInfoMap map[string]subscriptionInfo

	// Parser used for the subscription filters
	selectorParser *search.SelectorParser
}

func (b *alarmSubscriptionSearcherBuilder) build() (result *alarmSubscriptionSearcher, err error) {

	// Create the filter expression parser:
	selectorParser, err := search.NewSelectorParser().
		SetLogger(b.logger).
		Build()
	if err != nil {
		b.logger.Error(
			"failed to create filter expression parser: ",
			slog.String("error", err.Error()),
		)
		return
	}

	result = &alarmSubscriptionSearcher{
		logger:              b.logger,
		jqTool:              b.jqTool,
		subscriptionInfoMap: map[string]subscriptionInfo{},
		selectorParser:      selectorParser,
	}

	return
}

// NOTE the function should be called by a function that is holding the semophone
func (b *alarmSubscriptionSearcher) getSubFilters(filterStr, subId string) (err error) {

	result, err := b.selectorParser.Parse(filterStr)

	if err != nil {
		b.logger.Debug(
			"getSubFilters failed to parse the filter string ",
			slog.String("filter string", filterStr),
		)
		return
	}

	subInfo := b.subscriptionInfoMap[subId]
	subInfo.filters = *result
	b.subscriptionInfoMap[subId] = subInfo

	return
}

// NOTE the function should be called by a function that is holding the semophone
func (b *alarmSubscriptionSearcher) pocessSubscriptionMapForSearcher(subscriptionMap *map[string]data.Object, // nolint: gocritic
	jqTool *jq.Tool) (err error) {

	for key, value := range *subscriptionMap {

		b.subscriptionInfoMap[key] = subscriptionInfo{}

		subInfo := b.subscriptionInfoMap[key]
		// get uris
		var uris string
		err = jqTool.Evaluate(`.callback`, value, &uris)
		if err != nil {
			b.logger.Error(
				"Subscription  ", key,
				" does not have callback included",
			)
			continue
		}
		subInfo.uris = uris

		var consumerId string
		err = jqTool.Evaluate(`.consumerSubscriptionId`, value, &consumerId)
		if err == nil {
			subInfo.consumerSubscriptionId = consumerId
		}

		b.subscriptionInfoMap[key] = subInfo

		// get filter from data object
		var filter string
		err = jqTool.Evaluate(`.filter`, value, &filter)
		if err != nil {
			b.logger.Debug(
				"Subscription  ", key,
				" does not have filter included",
			)
		}

		err = b.getSubFilters(filter, key)
		if err != nil {
			b.logger.Debug(
				"pocessSubscriptionMapForSearcher ",
				"subscription: ", key,
				" error", err.Error(),
			)
		}
	}
	return
}

// following function is on the path trigger by alerts originated from alert manager
// and query the subscription data structure to get matched the subscription
// The read lock is needed here to protect the read access to the data
func (h *AlarmNotificationHandler) getSubscriptionIdsFromAlarm(ctx context.Context, alarm data.Object) (result alarmSubIdSet) {

	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.RUnlock()
	result = alarmSubIdSet{}

	for subId, subInfo := range h.subscriptionSearcher.subscriptionInfoMap {

		match, err := h.selectorEvaluator.Evaluate(ctx, &subInfo.filters, alarm)
		if err != nil {
			h.logger.Debug(
				"pocessSubscriptionMapForSearcher ",
				"subscription: ", subId,
				slog.String("error", err.Error()),
			)
			continue
		}
		if match {
			h.logger.Debug(
				"pocessSubscriptionMapForSearcher MATCH ",
				"subscription: ", subId,
			)
			result[subId] = struct{}{}
		}
	}
	return
}

func (h *AlarmNotificationHandler) getSubscriptionInfo(ctx context.Context, subId string) (result subscriptionInfo, ok bool) { // nolint: unparam
	h.subscriptionMapMemoryLock.RLock()
	defer h.subscriptionMapMemoryLock.RUnlock()
	result, ok = h.subscriptionSearcher.subscriptionInfoMap[subId]
	return
}
