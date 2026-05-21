/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package listener

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("processResourceTypeChangeNotification", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("handles deleted change type without accessing repository", func() {
		notification := ResourceTypeChangeNotification{
			ResourceTypeID: uuid.New(),
			ChangeType:     "deleted",
		}
		payload, err := json.Marshal(notification)
		Expect(err).NotTo(HaveOccurred())

		err = processResourceTypeChangeNotification(ctx, nil, &pgconn.Notification{
			Payload: string(payload),
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns error for invalid JSON payload", func() {
		err := processResourceTypeChangeNotification(ctx, nil, &pgconn.Notification{
			Payload: "not-valid-json",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to unmarshal"))
	})

	It("handles unknown change type without error", func() {
		notification := ResourceTypeChangeNotification{
			ResourceTypeID: uuid.New(),
			ChangeType:     "unknown-type",
		}
		payload, err := json.Marshal(notification)
		Expect(err).NotTo(HaveOccurred())

		err = processResourceTypeChangeNotification(ctx, nil, &pgconn.Notification{
			Payload: string(payload),
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
