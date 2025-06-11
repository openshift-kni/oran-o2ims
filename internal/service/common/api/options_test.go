/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package api

import (
	"testing"
)

var allFields = "true"
var includedFields = "name,description"
var includedExtensions = "extensions"
var excludedFields = "extensions"
var defaultExcludeFields = "extensions"

var invalidFields = "description"
var invalidPtrFields = "ptrSubObject/description"
var invalidSubFields = "subObject/description"
var invalidArrayFields = "arraySubObject/description"
var invalidNestingLevelFields = "arraySubObject/another/description"
var invalidPtrObjectTypeFields = "ptrToString/value"
var invalidObjectTypeFields = "string/value"
var validFields = "name, subObject,ptrSubObject,arraySubObject"
var validNestedFields = "subObject/value, arraySubObject/value,ptrSubObject/value"

func TestFieldOptions_IsIncluded(t *testing.T) {
	type MyStruct struct {
		Name        string             `json:"name,omitempty"`
		Description string             `json:"description,omitempty"`
		Extensions  *map[string]string `json:"extensions,omitempty"`
	}
	type fields struct {
		allFields      *string
		includeFields  *string
		excludeFields  *string
		defaultExclude []string
	}
	type args struct {
		fieldName string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name:   "Default Case",
			fields: fields{},
			args:   args{"extensions"},
			want:   true,
		},
		{
			name:   "Include case",
			fields: fields{includeFields: &includedFields},
			args:   args{"extensions"},
			want:   false,
		},
		{
			name:   "Exclude case",
			fields: fields{excludeFields: &excludedFields},
			args:   args{"extensions"},
			want:   false,
		},
		{
			name:   "Default Exclude case",
			fields: fields{defaultExclude: []string{defaultExcludeFields}},
			args:   args{"extensions"},
			want:   false,
		},
		{
			name:   "All Fields case",
			fields: fields{allFields: &allFields, defaultExclude: []string{defaultExcludeFields}},
			args:   args{"extensions"},
			want:   true,
		},
		{
			name:   "Include overrides Exclude case",
			fields: fields{includeFields: &includedExtensions, excludeFields: &excludedFields},
			args:   args{"extensions"},
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewFieldOptions(tt.fields.allFields, tt.fields.includeFields, tt.fields.excludeFields, tt.fields.defaultExclude...)
			if err := o.Validate(MyStruct{}); err != nil {
				t.Error(err)
			}
			if got := o.IsIncluded(tt.args.fieldName); got != tt.want {
				t.Errorf("IsIncluded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFieldOptions_Validate(t *testing.T) {
	type MySubObject struct {
		Value string `json:"value"`
	}
	type MyObject struct {
		Name           string        `json:"name"`
		SubObject      MySubObject   `json:"subObject"`
		PtrSubObject   *MySubObject  `json:"ptrSubObject"`
		ArraySubObject []MySubObject `json:"arraySubObject"`
		PtrToString    *string       `json:"ptrToString"`
		String         string        `json:"string"`
		NoTag          bool
	}

	type fields struct {
		allFields      *string
		includeFields  *string
		excludeFields  *string
		defaultExclude []string
	}
	type args struct {
		object interface{}
	}
	object := MyObject{}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:    "Valid case",
			fields:  fields{includeFields: &validFields},
			args:    args{object: object},
			wantErr: false,
		},
		{
			name:    "Valid nested case",
			fields:  fields{includeFields: &validNestedFields},
			args:    args{object: object},
			wantErr: false,
		},
		{
			name:    "Invalid case",
			fields:  fields{includeFields: &invalidFields},
			args:    args{object: object},
			wantErr: true,
		},
		{
			name:    "Invalid nested case (sub)",
			fields:  fields{includeFields: &invalidSubFields},
			args:    args{object: object},
			wantErr: true,
		},
		{
			name:    "Invalid nested case (ptr)",
			fields:  fields{includeFields: &invalidPtrFields},
			args:    args{object: object},
			wantErr: true,
		},
		{
			name:    "Invalid nested case (array)",
			fields:  fields{includeFields: &invalidArrayFields},
			args:    args{object: object},
			wantErr: true,
		},
		{
			name:    "Invalid nested case (over)",
			fields:  fields{includeFields: &invalidNestingLevelFields},
			args:    args{object: object},
			wantErr: true,
		},
		{
			name:    "Invalid pointer object type",
			fields:  fields{includeFields: &invalidPtrObjectTypeFields},
			args:    args{object: object},
			wantErr: true,
		},
		{
			name:    "Invalid object type",
			fields:  fields{includeFields: &invalidObjectTypeFields},
			args:    args{object: object},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewFieldOptions(tt.fields.allFields, tt.fields.includeFields, tt.fields.excludeFields, tt.fields.defaultExclude...)
			if err := o.Validate(tt.args.object); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
