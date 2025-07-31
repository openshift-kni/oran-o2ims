/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/notifier"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// Compile time check for interface compliance
var _ notifier.SubscriptionProvider = (*SubscriptionStorageProvider)(nil)

// NotificationTransformer defines a function provider by a domain-specific module that knows how to apply any last
// minute transformations to a notification that are subscription-specific.
type NotificationTransformer interface {
	Transform(subscription *notifier.SubscriptionInfo, notification *notifier.Notification) (*notifier.Notification, error)
}

// SubscriptionStorageProvider implements the SubscriptionProvider interface as a means to abstract the concrete
// subscription type out of the Notifier
type SubscriptionStorageProvider struct {
	repository  *CommonRepository
	transformer NotificationTransformer
}

// NewSubscriptionStorageProvider creates a new SubscriptionProvider
func NewSubscriptionStorageProvider(repository *CommonRepository, transformer NotificationTransformer) notifier.SubscriptionProvider {
	return &SubscriptionStorageProvider{
		repository:  repository,
		transformer: transformer,
	}
}

// GetSubscriptions returns the list of subscriptions persisted to the database
func (p *SubscriptionStorageProvider) GetSubscriptions(ctx context.Context) ([]notifier.SubscriptionInfo, error) {
	records, err := p.repository.GetSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var subscriptions []notifier.SubscriptionInfo
	for _, result := range records {
		subscriptions = append(subscriptions, *models.SubscriptionToInfo(&result))
	}

	return subscriptions, nil
}

// UpdateSubscription updates the subscription on behalf of the Notifier.  Currently only supports setting the event
// cursor.
func (p *SubscriptionStorageProvider) UpdateSubscription(ctx context.Context, subscription *notifier.SubscriptionInfo) error {
	record, err := p.repository.GetSubscription(ctx, subscription.SubscriptionID)
	if errors.Is(err, svcutils.ErrNotFound) {
		return fmt.Errorf("subscription %s not found", subscription.SubscriptionID)
	} else if err != nil {
		return fmt.Errorf("failed to get subscription %s: %w", subscription.SubscriptionID, err)
	}

	// Only update the event cursor since that's the only piece of data updated by the notifier
	record.EventCursor = subscription.EventCursor

	_, err = p.repository.UpdateSubscription(ctx, record)
	if err != nil {
		return fmt.Errorf("failed to update subscription %s: %w", subscription.SubscriptionID, err)
	}

	return nil
}

// Matches determines if an event matches the filter defined for this subscription
func (p *SubscriptionStorageProvider) Matches(subscription *notifier.SubscriptionInfo, notification *notifier.Notification) bool {
	// TODO: implement filtering.  Currently not defined in the spec, but a reasonable approach may be to implement
	//  the same kind of filtering as on the API query parameters although may not make complete sense given that
	//  different object types have different fields and a filter can only contain 1 description.
	return true
}

// Transform updates the notification with subscription-specific information.
func (p *SubscriptionStorageProvider) Transform(subscription *notifier.SubscriptionInfo, notification *notifier.Notification) (*notifier.Notification, error) {
	result, err := p.transformer.Transform(subscription, notification)
	if err != nil {
		return nil, fmt.Errorf("failed to transform notification: %w", err)
	}
	return result, nil
}
