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
	})
})
