/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"reflect"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockDBModel struct {
	RecordID      int                    `db:"record_id"`
	SomeUUID      uuid.UUID              `db:"some_uuid"`
	SomeMapPtr    *map[string]string     `db:"some_map_ptr"`
	SomeStringPtr *string                `db:"some_string_ptr"`
	RaisedTime    time.Time              `db:"raised_time"`
	ChangedTime   *time.Time             `db:"changed_time"`
	Extensions    map[string]interface{} `db:"extensions"`
	CreatedAt     time.Time              `db:"created_at"`
}

func (m *mockDBModel) TableName() string {
	return "mock_table"
}

func (m *mockDBModel) PrimaryKey() string {
	return "record_id"
}

func (m *mockDBModel) OnConflict() string {
	return "record_id"
}

var _ = Describe("Utils", func() {
	Describe("GetTrackingUUID", func() {
		It("returns UUID unchanged when key is UUID", func() {
			id := uuid.New()
			result := GetTrackingUUID(id)
			Expect(result).To(Equal(id))
		})

		It("returns deterministic UUID for string key", func() {
			key := "LOC-001"
			result1 := GetTrackingUUID(key)
			result2 := GetTrackingUUID(key)
			Expect(result1).To(Equal(result2)) // deterministic
			Expect(result1).NotTo(Equal(uuid.Nil))
		})

		It("returns different UUIDs for different string keys", func() {
			result1 := GetTrackingUUID("LOC-001")
			result2 := GetTrackingUUID("LOC-002")
			Expect(result1).NotTo(Equal(result2))
		})

		It("returns UUID using SHA1 namespace for string keys", func() {
			key := "test-location"
			expected := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(key))
			result := GetTrackingUUID(key)
			Expect(result).To(Equal(expected))
		})

		It("handles empty string key", func() {
			result := GetTrackingUUID("")
			Expect(result).NotTo(Equal(uuid.Nil))
			// Empty string should still produce a deterministic UUID
			Expect(result).To(Equal(GetTrackingUUID("")))
		})

		It("handles other types by converting to string", func() {
			// Integer
			result1 := GetTrackingUUID(42)
			result2 := GetTrackingUUID(42)
			Expect(result1).To(Equal(result2))
			Expect(result1).NotTo(Equal(uuid.Nil))

			// Different integers produce different UUIDs
			Expect(GetTrackingUUID(42)).NotTo(Equal(GetTrackingUUID(43)))
		})
	})

	Describe("DB tags", func() {
		It("returns all tags of the alarm_event_record", func() {
			ar := mockDBModel{}
			tags := GetAllDBTagsFromStruct(&ar)

			st := reflect.TypeOf(ar)
			Expect(tags).To(HaveLen(st.NumField()))
			Expect(tags).To(ConsistOf(
				"record_id", "raised_time", "changed_time",
				"some_uuid", "some_map_ptr", "some_string_ptr",
				"extensions", "created_at"))
		})

		It("returns only the tags of RecordID and Extensions fields", func() {
			ar := mockDBModel{}
			tags := GetDBTagsFromStructFields(&ar, "RecordID", "Extensions")

			Expect(tags).To(HaveLen(2))
			Expect(tags).To(ConsistOf("record_id", "extensions"))
		})

		It("ignores non-existing fields", func() {
			ar := mockDBModel{}
			tags := GetDBTagsFromStructFields(&ar, "RecordID", "nonExistentField")
			Expect(len(tags)).To(Equal(1))
			Expect(tags).To(ConsistOf("record_id"))
		})

		It("excludes nil pointers", func() {
			ar := mockDBModel{}
			tags := GetNonNilDBTagsFromStruct(&ar)
			Expect(tags).To(ConsistOf(
				"record_id", "raised_time", "some_uuid",
				"extensions", "created_at"))
			columns, values := GetColumnsAndValues(&ar, tags)
			Expect(columns).To(ConsistOf(tags.Columns()))
			Expect(len(values)).To(Equal(len(columns)))
			Expect(values).To(ConsistOf(ar.RecordID, ar.RaisedTime, ar.SomeUUID, ar.Extensions, ar.CreatedAt))
		})

		It("includes non-nil pointers", func() {
			changedTime := time.Now()
			ar := mockDBModel{
				ChangedTime: &changedTime,
			}
			tags := GetNonNilDBTagsFromStruct(&ar)
			Expect(tags.Columns()).To(ConsistOf(
				"record_id", "raised_time", "some_uuid", "changed_time",
				"extensions", "created_at"))
			columns, values := GetColumnsAndValues(&ar, tags)
			Expect(columns).To(ConsistOf(tags.Columns()))
			Expect(len(values)).To(Equal(len(columns)))
			Expect(values).To(ConsistOf(ar.RecordID, ar.RaisedTime, ar.SomeUUID, ar.ChangedTime, ar.Extensions, ar.CreatedAt))
		})

		It("returns no fields when compare the same structs", func() {
			ar := mockDBModel{}
			tags := CompareObjects(&ar, &ar)
			Expect(tags).To(BeEmpty())
		})

		It("returns no fields when different but identical structs", func() {
			t1 := mockDBModel{}
			t2 := mockDBModel{}
			tags := CompareObjects(&t1, &t2)
			Expect(tags).To(BeEmpty())
		})

		It("returns fields of differing non-pointers", func() {
			t1 := mockDBModel{RaisedTime: time.Now()}
			t2 := mockDBModel{}
			tags := CompareObjects(&t1, &t2)
			Expect(tags.Columns()).To(ConsistOf("raised_time"))
		})

		It("ignores excluded fields", func() {
			t1 := mockDBModel{RaisedTime: time.Now()}
			t2 := mockDBModel{}
			tags := CompareObjects(&t1, &t2, "RaisedTime")
			Expect(tags.Columns()).To(BeEmpty())
		})

		It("returns fields of pointers with different nil-ness", func() {
			now := time.Now()
			t1 := mockDBModel{ChangedTime: &now}
			t2 := mockDBModel{}
			tags := CompareObjects(&t1, &t2)
			Expect(tags.Columns()).To(ConsistOf("changed_time"))
		})

		It("returns fields of pointers with different values", func() {
			someMap := map[string]string{"a": "1"}
			anotherMap := map[string]string{"a": "2"}
			someString := "some string"
			anotherString := "another string"
			now := time.Now()
			later := time.Now().Add(1 * time.Minute)
			t1 := mockDBModel{
				ChangedTime:   &now,
				SomeStringPtr: &someString,
				SomeMapPtr:    &someMap}
			t2 := mockDBModel{
				ChangedTime:   &later,
				SomeStringPtr: &anotherString,
				SomeMapPtr:    &anotherMap}
			tags := CompareObjects(&t1, &t2)
			Expect(tags.Columns()).To(ConsistOf("changed_time", "some_string_ptr", "some_map_ptr"))
		})

		It("returns no fields when pointer addresses are different but the values are the same", func() {
			someMap := map[string]string{"a": "1"}
			sameMap := map[string]string{"a": "1"}
			something := ""
			somethingAgain := ""
			now := time.Now()
			again := now
			t1 := mockDBModel{
				ChangedTime:   &now,
				SomeStringPtr: &something,
				SomeMapPtr:    &someMap}
			t2 := mockDBModel{
				ChangedTime:   &again,
				SomeStringPtr: &somethingAgain,
				SomeMapPtr:    &sameMap}
			tags := CompareObjects(&t1, &t2)
			Expect(tags.Columns()).To(BeEmpty())
		})
	})
})
