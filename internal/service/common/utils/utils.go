package utils

import (
	"reflect"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

type DBTag map[string]string

// Columns is used in the Columns method of the SelectBuilder to convert the DBTag to a slice of any.
func (r DBTag) Columns() []any {
	columns := make([]any, 0, len(r))
	for _, tag := range r {
		columns = append(columns, tag)
	}

	return columns
}

// GetAllDBTagsFromStruct returns a map of field names to their db tags.
func GetAllDBTagsFromStruct[T db.Model](s T) DBTag {
	tags := make(DBTag)

	st := reflect.TypeOf(s)
	if st.Kind() != reflect.Struct {
		st = st.Elem()
	}

	for i := 0; i < st.NumField(); i++ {
		tags[st.Field(i).Name] = st.Field(i).Tag.Get("db")
	}

	return tags
}

// GetDBTagsFromStructFields returns a map of field names to their db tags. It only returns the tags of the fields specified.
// Non-existent fields are ignored.
func GetDBTagsFromStructFields[T db.Model](s T, fields ...string) DBTag {
	tags := make(DBTag)

	st := reflect.TypeOf(s)
	if st.Kind() != reflect.Struct {
		st = st.Elem()
	}

	for _, field := range fields {
		f, found := st.FieldByName(field)
		if !found {
			// Ignore fields that are not found
			continue
		}

		tags[f.Name] = f.Tag.Get("db")
	}

	return tags
}
