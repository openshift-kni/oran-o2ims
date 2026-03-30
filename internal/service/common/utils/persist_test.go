/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pashagolub/pgxmock/v4"
)

// mockLocationModel is a mock model with string primary key (like Location)
type mockLocationModel struct {
	GlobalLocationID string                 `db:"global_location_id"`
	Name             string                 `db:"name"`
	Description      string                 `db:"description"`
	GenerationID     int                    `db:"generation_id"`
	Extensions       map[string]interface{} `db:"extensions"`
	CreatedAt        *time.Time             `db:"created_at"`
}

func (m mockLocationModel) TableName() string {
	return "location"
}

func (m mockLocationModel) PrimaryKey() string {
	return "global_location_id"
}

func (m mockLocationModel) OnConflict() string {
	return ""
}

// mockResourcePoolModel is a mock model with UUID primary key (like ResourcePool)
type mockResourcePoolModel struct {
	ResourcePoolID uuid.UUID              `db:"resource_pool_id"`
	Name           string                 `db:"name"`
	Description    string                 `db:"description"`
	GenerationID   int                    `db:"generation_id"`
	Extensions     map[string]interface{} `db:"extensions"`
	CreatedAt      *time.Time             `db:"created_at"`
}

func (m mockResourcePoolModel) TableName() string {
	return "resource_pool"
}

func (m mockResourcePoolModel) PrimaryKey() string {
	return "resource_pool_id"
}

func (m mockResourcePoolModel) OnConflict() string {
	return ""
}

var _ = Describe("Find with string keys", func() {
	// These tests verify that the Find function (used by PersistObject) correctly
	// handles string keys like Location's GlobalLocationID

	var (
		ctx  context.Context
		mock pgxmock.PgxPoolIface
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		mock.Close()
	})

	It("should find location by string key", func() {
		locationID := "LOC-001"
		now := time.Now()

		rows := pgxmock.NewRows([]string{
			"global_location_id", "name", "description", "generation_id", "extensions", "created_at",
		}).AddRow(locationID, "Test Location", "Description", 1, map[string]interface{}{"key": "value"}, &now)

		mock.ExpectQuery(`SELECT .* FROM location`).
			WithArgs(locationID).
			WillReturnRows(rows)

		result, err := Find[mockLocationModel](ctx, mock, locationID)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.GlobalLocationID).To(Equal(locationID))
		Expect(result.Name).To(Equal("Test Location"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("should return ErrNotFound when string key not found (negative test)", func() {
		locationID := "NON-EXISTENT"

		rows := pgxmock.NewRows([]string{
			"global_location_id", "name", "description", "generation_id", "extensions", "created_at",
		}) // Empty rows

		mock.ExpectQuery(`SELECT .* FROM location`).
			WithArgs(locationID).
			WillReturnRows(rows)

		result, err := Find[mockLocationModel](ctx, mock, locationID)

		Expect(err).To(MatchError(ErrNotFound))
		Expect(result).To(BeNil())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("should handle empty string key", func() {
		locationID := ""
		now := time.Now()

		rows := pgxmock.NewRows([]string{
			"global_location_id", "name", "description", "generation_id", "extensions", "created_at",
		}).AddRow(locationID, "Empty ID Location", "", 0, nil, &now)

		mock.ExpectQuery(`SELECT .* FROM location`).
			WithArgs(locationID).
			WillReturnRows(rows)

		result, err := Find[mockLocationModel](ctx, mock, locationID)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.GlobalLocationID).To(Equal(""))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})

var _ = Describe("Find with UUID keys", func() {
	var (
		ctx  context.Context
		mock pgxmock.PgxPoolIface
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		mock.Close()
	})

	It("should find resource pool by UUID key", func() {
		poolID := uuid.New()
		now := time.Now()

		rows := pgxmock.NewRows([]string{
			"resource_pool_id", "name", "description", "generation_id", "extensions", "created_at",
		}).AddRow(poolID, "Test Pool", "Description", 1, map[string]interface{}{"key": "value"}, &now)

		mock.ExpectQuery(`SELECT .* FROM resource_pool`).
			WithArgs(poolID).
			WillReturnRows(rows)

		result, err := Find[mockResourcePoolModel](ctx, mock, poolID)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.ResourcePoolID).To(Equal(poolID))
		Expect(result.Name).To(Equal("Test Pool"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("should return ErrNotFound when UUID key not found (negative test)", func() {
		poolID := uuid.New()

		rows := pgxmock.NewRows([]string{
			"resource_pool_id", "name", "description", "generation_id", "extensions", "created_at",
		}) // Empty rows

		mock.ExpectQuery(`SELECT .* FROM resource_pool`).
			WithArgs(poolID).
			WillReturnRows(rows)

		result, err := Find[mockResourcePoolModel](ctx, mock, poolID)

		Expect(err).To(MatchError(ErrNotFound))
		Expect(result).To(BeNil())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})

var _ = Describe("PersistObject with transaction", func() {
	// These tests verify PersistObject behavior with transactions
	// using the mock transaction returned by pgxmock

	var (
		ctx  context.Context
		mock pgxmock.PgxPoolIface
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock, err = pgxmock.NewPool()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		mock.Close()
	})

	It("should detect no changes with string key (Location)", func() {
		locationID := "LOC-001"
		now := time.Now()

		// Same object with identical values
		location := mockLocationModel{
			GlobalLocationID: locationID,
			Name:             "Same Name",
			Description:      "Same Description",
			GenerationID:     1,
			Extensions:       map[string]interface{}{"key": "value"},
			CreatedAt:        &now,
		}

		// Begin transaction
		mock.ExpectBegin()

		// Expect Find query to return the same object
		findRows := pgxmock.NewRows([]string{
			"global_location_id", "name", "description", "generation_id", "extensions", "created_at",
		}).AddRow(locationID, "Same Name", "Same Description", 1, map[string]interface{}{"key": "value"}, &now)

		mock.ExpectQuery(`SELECT .* FROM location`).
			WithArgs(locationID).
			WillReturnRows(findRows)

		// Start transaction
		tx, err := mock.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Execute PersistObject:no update should happen since objects are identical
		before, after, err := PersistObject(ctx, tx, location, locationID)

		// Verify
		Expect(err).ToNot(HaveOccurred())
		Expect(before).ToNot(BeNil())
		Expect(after).ToNot(BeNil())
		Expect(before.Name).To(Equal(after.Name))
		Expect(before.GlobalLocationID).To(Equal(locationID))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("should handle find error with string key (negative test)", func() {
		locationID := "LOC-ERROR"

		location := mockLocationModel{
			GlobalLocationID: locationID,
			Name:             "Test Location",
		}

		// Begin transaction
		mock.ExpectBegin()

		// Expect Find query to return error (not ErrNoRows)
		mock.ExpectQuery(`SELECT .* FROM location`).
			WithArgs(locationID).
			WillReturnError(pgx.ErrTxClosed) // force an error

		// Start transaction
		tx, err := mock.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Execute PersistObject
		before, after, err := PersistObject(ctx, tx, location, locationID)

		// Verify error handling
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get object"))
		Expect(before).To(BeNil())
		Expect(after).To(BeNil())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
})
