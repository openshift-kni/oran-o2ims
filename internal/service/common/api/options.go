package api

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Fields that can be included/excluded (i.e., optional complex fields where 'complex' means an object or an array)
const (
	ExtensionsAttribute   = "extensions"
	MemberOfAttribute     = "memberOf"
	CapacityAttribute     = "capacity"
	CapabilitiesAttribute = "capabilities"
)

// FieldOptions implements the attribute selection algorithm specified in ETSI GS NFV-SOL 013 V3.5.1, Section 5.3.2.2
type FieldOptions struct {
	allFields     bool
	includeFields []string
	excludeFields []string
}

// NewFieldOptions creates a new FieldOptions using the specific values from the API query parameters
func NewFieldOptions(allFields, fields, excludeFields *string, excludeDefaults ...string) *FieldOptions {
	result := &FieldOptions{}

	if allFields != nil {
		if value, err := strconv.ParseBool(*allFields); err == nil {
			result.allFields = value
		}
	}

	if fields != nil {
		for _, field := range strings.Split(*fields, ",") {
			result.includeFields = append(result.includeFields, strings.TrimSpace(field))
		}
	}

	if excludeFields != nil {
		for _, field := range strings.Split(*excludeFields, ",") {
			result.excludeFields = append(result.excludeFields, strings.TrimSpace(field))
		}
	}

	result.excludeFields = append(result.excludeFields, excludeDefaults...)

	return result
}

// NewDefaultFieldOptions defines the default implementation assuming no parameters were supplied which includes all
// attributes
func NewDefaultFieldOptions() *FieldOptions {
	return &FieldOptions{
		allFields: true,
	}
}

// IsIncluded determines if a given attribute should be included.
func (o *FieldOptions) IsIncluded(fieldName string) bool {
	if o.allFields {
		return true
	}

	if len(o.includeFields) > 0 {
		for _, field := range o.includeFields {
			parts := strings.Split(field, "/")
			if fieldName == parts[0] {
				return true
			}
		}
		return false
	}

	for _, field := range o.excludeFields {
		if fieldName == field {
			return false
		}
	}

	return true
}

const maxNestingLevel = 5

func (o *FieldOptions) findFieldByName(v reflect.Type, depth int, fieldName string) error {
	if depth > maxNestingLevel {
		return fmt.Errorf("struct nesting limit reached")
	}

	names := strings.Split(fieldName, "/")
	name := names[depth-1]
	found := false
	for i := range v.NumField() {
		field := v.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" {
			continue
		}
		if strings.Split(tag, ",")[0] == name {
			if len(names) == depth {
				found = true
				break
			}
			// Check nested objects
			switch field.Type.Kind() {
			case reflect.Ptr, reflect.Slice:
				ft := field.Type.Elem()
				if ft.Kind() != reflect.Struct {
					return fmt.Errorf("field %s cannot exist on a primitive data type", fieldName)
				}
				if err := o.findFieldByName(ft, depth+1, fieldName); err != nil {
					return err
				}
				found = true
			case reflect.Struct:
				if err := o.findFieldByName(field.Type, depth+1, fieldName); err != nil {
					return err
				}
				found = true
			default:
				return fmt.Errorf("field %s is not a struct or slice", fieldName)
			}
		}
	}

	if !found {
		return fmt.Errorf("field %s does not exist", fieldName)
	}

	return nil
}

// Validate determines if any of the fields specified don't exist
func (o *FieldOptions) Validate(object interface{}) error {
	fieldNames := make([]string, 0)
	fieldNames = append(fieldNames, o.includeFields...)
	fieldNames = append(fieldNames, o.excludeFields...)
	v := reflect.TypeOf(object)
	for _, fieldName := range fieldNames {
		if err := o.findFieldByName(v, 1, fieldName); err != nil {
			return err
		}
	}
	return nil
}
