// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
)

// SystemVendor system vendor
//
// swagger:model system_vendor
type SystemVendor struct {

	// manufacturer
	Manufacturer string `json:"manufacturer,omitempty"`

	// product name
	ProductName string `json:"product_name,omitempty"`

	// serial number
	SerialNumber string `json:"serial_number,omitempty"`

	// Whether the machine appears to be a virtual machine or not
	Virtual bool `json:"virtual,omitempty"`
}

// Validate validates this system vendor
func (m *SystemVendor) Validate(formats strfmt.Registry) error {
	return nil
}

// ContextValidate validates this system vendor based on context it is used
func (m *SystemVendor) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (m *SystemVendor) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *SystemVendor) UnmarshalBinary(b []byte) error {
	var res SystemVendor
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
