package utils

import (
	"reflect"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

type DBTag map[string]string
type DBValue map[string]interface{}

const IncludeNilValues = false
const ExcludeNilValues = true

// Columns is used in the Columns method of the SelectBuilder to convert the DBTag to a slice of any.
func (r DBTag) Columns() []any {
	columns := make([]any, 0, len(r))
	for _, tag := range r {
		columns = append(columns, tag)
	}

	return columns
}

// Values is used in the Values method of the SelectBuilder to convert the DBValue to a slice of any.
func (r DBValue) Values() []any {
	values := make([]any, 0, len(r))
	for _, value := range r {
		values = append(values, value)
	}

	return values
}

// GetAllDBTagsFromStruct returns a map of field names to their db tags.
func GetAllDBTagsFromStruct[T db.Model](s T, excludeNilValues bool) DBTag {
	tags := make(DBTag)

	st := reflect.TypeOf(s)
	sv := reflect.ValueOf(s)
	if st.Kind() != reflect.Struct {
		st = st.Elem()
		sv = sv.Elem()
	}

	for i := 0; i < st.NumField(); i++ {
		fieldName := st.Field(i).Name
		switch {
		case !excludeNilValues:
			tags[fieldName] = st.Field(i).Tag.Get("db")
		case st.Field(i).Type.Kind() != reflect.Pointer:
			tags[fieldName] = st.Field(i).Tag.Get("db")
		default:
			fieldValue := sv.Field(i)
			if !fieldValue.IsNil() {
				tags[fieldName] = st.Field(i).Tag.Get("db")
			}
		}
	}

	return tags
}

// GetFieldValues returns the list of values associated to the field names specified in the tags parameter.
func GetFieldValues[T db.Model](s T, tags DBTag) []any {
	values := make([]any, 0, len(tags))

	st := reflect.TypeOf(s)
	sv := reflect.ValueOf(s)
	if st.Kind() != reflect.Struct {
		st = st.Elem()
		sv = sv.Elem()
	}

	for fieldName := range tags {
		if field, ok := st.FieldByName(fieldName); ok {
			if field.Type.Kind() != reflect.Pointer {
				values = append(values, sv.FieldByName(fieldName).Interface())
			} else {
				fieldValue := sv.FieldByName(fieldName)
				if !fieldValue.IsNil() {
					values = append(values, fieldValue.Interface())
				}
			}
		}
	}

	return values
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
