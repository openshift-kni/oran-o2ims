package utils

import (
	"reflect"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

type DBTag map[string]string

const includeNilValues = false
const excludeNilValues = true

// Columns is used in the Columns method of the SelectBuilder to convert the DBTag to a slice of any.
func (r DBTag) Columns() []any {
	columns := make([]any, 0, len(r))
	for _, tag := range r {
		columns = append(columns, tag)
	}

	return columns
}

// Fields is used get the list of field names from the DBTag map
func (r DBTag) Fields() []string {
	fields := make([]string, 0, len(r))
	for f := range r {
		fields = append(fields, f)
	}

	return fields
}

// getDBTagsFromStruct returns a map of field names to their db tags.
func getDBTagsFromStruct[T db.Model](s T, excludeNilValues bool) DBTag {
	tags := make(DBTag)

	st := reflect.TypeOf(s)
	sv := reflect.ValueOf(s)
	if st.Kind() != reflect.Struct {
		st = st.Elem()
		sv = sv.Elem()
	}

	for i := 0; i < st.NumField(); i++ {
		fieldName := st.Field(i).Name
		tagValue := st.Field(i).Tag.Get("db")
		switch {
		case !excludeNilValues:
			tags[fieldName] = tagValue
		case st.Field(i).Type.Kind() != reflect.Pointer:
			tags[fieldName] = tagValue
		default:
			fieldValue := sv.Field(i)
			if !fieldValue.IsNil() {
				tags[fieldName] = tagValue
			}
		}
	}

	return tags
}

// GetNonNilDBTagsFromStruct returns a map of field names to their db tags.  Only non-pointer fields
// or non-nil pointer fields are considered.
func GetNonNilDBTagsFromStruct[T db.Model](s T) DBTag {
	return getDBTagsFromStruct(s, excludeNilValues)
}

// GetAllDBTagsFromStruct returns a map of field names to their db tags.
func GetAllDBTagsFromStruct[T db.Model](s T) DBTag {
	return getDBTagsFromStruct(s, includeNilValues)
}

// GetColumnsAndValues returns the list of values associated to the field names specified in the tags parameter.  Both the
// columns and the values are returned together to ensure that they are aligned.
func GetColumnsAndValues[T db.Model](s T, tags DBTag) ([]string, []any) {
	columns := make([]string, 0, len(tags))
	values := make([]any, 0, len(tags))

	st := reflect.TypeOf(s)
	sv := reflect.ValueOf(s)
	if st.Kind() != reflect.Struct {
		st = st.Elem()
		sv = sv.Elem()
	}

	for fieldName, columnName := range tags {
		if field, ok := st.FieldByName(fieldName); ok {
			if field.Type.Kind() != reflect.Pointer {
				columns = append(columns, columnName)
				values = append(values, sv.FieldByName(fieldName).Interface())
			} else {
				fieldValue := sv.FieldByName(fieldName)
				if !fieldValue.IsNil() {
					columns = append(columns, columnName)
					values = append(values, fieldValue.Interface())
				}
			}
		}
	}

	return columns, values
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

// CompareObjects compares two objects and returns the tags for fields having differing values.  Any field names listed
// in `excluded` are ignored.
func CompareObjects[T db.Model](a, b T, excluded ...string) DBTag {
	tags := make(DBTag)
	excludedFields := make(map[string]bool)
	for _, field := range excluded {
		excludedFields[field] = true
	}

	ta := reflect.TypeOf(a)
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)
	if ta.Kind() != reflect.Struct {
		ta = ta.Elem()
		va = va.Elem()
		vb = vb.Elem()
	}

	for i := 0; i < ta.NumField(); i++ {
		field := ta.Field(i)
		fieldName := field.Name
		tagValue := field.Tag.Get("db")
		aFieldValue := va.FieldByName(fieldName)
		bFieldValue := vb.FieldByName(fieldName)
		if _, found := excludedFields[fieldName]; found {
			// ignore any fields that were explicitly excluded by the caller.
			continue
		}

		switch {
		case field.Type.Kind() != reflect.Ptr:
			// Non-pointer value compare them directly
			if !reflect.DeepEqual(aFieldValue.Interface(), bFieldValue.Interface()) {
				tags[fieldName] = tagValue
			}
		case aFieldValue.IsNil() != bFieldValue.IsNil():
			// Pointer nil-ness are different so include this field
			tags[fieldName] = tagValue
		case !aFieldValue.IsNil():
			// Compare non-nil values
			if !reflect.DeepEqual(aFieldValue.Interface(), bFieldValue.Interface()) {
				tags[fieldName] = tagValue
			}
		}
	}

	return tags
}
