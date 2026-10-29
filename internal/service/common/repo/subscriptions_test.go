/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"github.com/pashagolub/pgxmock/v5"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/repo"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
)

func TestSubscriptionStorageProvider_GetSubscription(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	mock, err := pgxmock.NewPool()
	g.Expect(err).ToNot(HaveOccurred())
	defer mock.Close()

	provider := repo.NewSubscriptionStorageProvider(&repo.CommonRepository{Db: mock}, nil)

	// Happy path: a matching subscription is returned as a generic SubscriptionInfo.
	id := uuid.New()
	mock.ExpectQuery("SELECT (.+) FROM subscription WHERE").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"subscription_id", "callback", "event_cursor"}).
			AddRow(&id, "http://x", 0))

	info, err := provider.GetSubscription(ctx, id)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(info.SubscriptionID).To(Equal(id))
	g.Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
}

func TestSubscriptionStorageProvider_GetSubscription_NotFound(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	mock, err := pgxmock.NewPool()
	g.Expect(err).ToNot(HaveOccurred())
	defer mock.Close()

	provider := repo.NewSubscriptionStorageProvider(&repo.CommonRepository{Db: mock}, nil)

	// Not found: ErrNotFound must be preserved so the notifier can detect deletion.
	id := uuid.New()
	mock.ExpectQuery("SELECT (.+) FROM subscription WHERE").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"subscription_id", "callback", "event_cursor"}))

	_, err = provider.GetSubscription(ctx, id)
	g.Expect(err).To(MatchError(svcutils.ErrNotFound))
	g.Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
}
