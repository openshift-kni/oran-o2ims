package utils

import (
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockDBModel struct {
	RecordID    int                    `db:"record_id"`
	RaisedTime  time.Time              `db:"raised_time"`
	ChangedTime *time.Time             `db:"changed_time"`
	Extensions  map[string]interface{} `db:"extensions"`
	CreatedAt   time.Time              `db:"created_at"`
}

func (m *mockDBModel) TableName() string {
	return "mock_table"
}

func (m *mockDBModel) PrimaryKey() string {
	return "record_id"
}

var _ = Describe("Utils", func() {
	Describe("DB tags", func() {
		It("returns all tags of the alarm_event_record", func() {
			ar := mockDBModel{}
			tags := GetAllDBTagsFromStruct(&ar)

			st := reflect.TypeOf(ar)
			Expect(tags).To(HaveLen(st.NumField()))
			Expect(tags).To(ConsistOf(
				"record_id", "raised_time", "changed_time",
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
				"record_id", "raised_time",
				"extensions", "created_at"))
			columns, values := GetColumnsAndValues(&ar, tags)
			Expect(columns).To(ConsistOf(tags.Columns()))
			Expect(len(values)).To(Equal(len(columns)))
			Expect(values).To(ConsistOf(ar.RecordID, ar.RaisedTime, ar.Extensions, ar.CreatedAt))
		})

		It("includes non-nil pointers", func() {
			changedTime := time.Now()
			ar := mockDBModel{
				ChangedTime: &changedTime,
			}
			tags := GetNonNilDBTagsFromStruct(&ar)
			Expect(tags.Columns()).To(ConsistOf(
				"record_id", "raised_time", "changed_time",
				"extensions", "created_at"))
			columns, values := GetColumnsAndValues(&ar, tags)
			Expect(columns).To(ConsistOf(tags.Columns()))
			Expect(len(values)).To(Equal(len(columns)))
			Expect(values).To(ConsistOf(ar.RecordID, ar.RaisedTime, ar.ChangedTime, ar.Extensions, ar.CreatedAt))
		})
	})
})
