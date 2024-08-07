// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"strconv"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// ClusterDefaultConfig cluster default config
//
// swagger:model cluster_default_config
type ClusterDefaultConfig struct {

	// cluster network cidr
	// Pattern: ^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)[\/]([1-9]|[1-2][0-9]|3[0-2]?)$
	ClusterNetworkCidr string `json:"cluster_network_cidr,omitempty"`

	// cluster network host prefix
	// Maximum: 32
	// Minimum: 1
	ClusterNetworkHostPrefix int64 `json:"cluster_network_host_prefix,omitempty"`

	// cluster networks dualstack
	ClusterNetworksDualstack []*ClusterNetwork `json:"cluster_networks_dualstack"`

	// cluster networks ipv4
	ClusterNetworksIPV4 []*ClusterNetwork `json:"cluster_networks_ipv4"`

	// This provides a list of forbidden hostnames. If this list is empty or not present, this implies that the UI should fall back to a hard coded list.
	ForbiddenHostnames []string `json:"forbidden_hostnames"`

	// inactive deletion hours
	InactiveDeletionHours int64 `json:"inactive_deletion_hours,omitempty"`

	// ntp source
	NtpSource string `json:"ntp_source"`

	// service network cidr
	// Pattern: ^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)[\/]([1-9]|[1-2][0-9]|3[0-2]?)$
	ServiceNetworkCidr string `json:"service_network_cidr,omitempty"`

	// service networks dualstack
	ServiceNetworksDualstack []*ServiceNetwork `json:"service_networks_dualstack"`

	// service networks ipv4
	ServiceNetworksIPV4 []*ServiceNetwork `json:"service_networks_ipv4"`
}

// Validate validates this cluster default config
func (m *ClusterDefaultConfig) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateClusterNetworkCidr(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateClusterNetworkHostPrefix(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateClusterNetworksDualstack(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateClusterNetworksIPV4(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateServiceNetworkCidr(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateServiceNetworksDualstack(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateServiceNetworksIPV4(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *ClusterDefaultConfig) validateClusterNetworkCidr(formats strfmt.Registry) error {
	if swag.IsZero(m.ClusterNetworkCidr) { // not required
		return nil
	}

	if err := validate.Pattern("cluster_network_cidr", "body", m.ClusterNetworkCidr, `^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)[\/]([1-9]|[1-2][0-9]|3[0-2]?)$`); err != nil {
		return err
	}

	return nil
}

func (m *ClusterDefaultConfig) validateClusterNetworkHostPrefix(formats strfmt.Registry) error {
	if swag.IsZero(m.ClusterNetworkHostPrefix) { // not required
		return nil
	}

	if err := validate.MinimumInt("cluster_network_host_prefix", "body", m.ClusterNetworkHostPrefix, 1, false); err != nil {
		return err
	}

	if err := validate.MaximumInt("cluster_network_host_prefix", "body", m.ClusterNetworkHostPrefix, 32, false); err != nil {
		return err
	}

	return nil
}

func (m *ClusterDefaultConfig) validateClusterNetworksDualstack(formats strfmt.Registry) error {
	if swag.IsZero(m.ClusterNetworksDualstack) { // not required
		return nil
	}

	for i := 0; i < len(m.ClusterNetworksDualstack); i++ {
		if swag.IsZero(m.ClusterNetworksDualstack[i]) { // not required
			continue
		}

		if m.ClusterNetworksDualstack[i] != nil {
			if err := m.ClusterNetworksDualstack[i].Validate(formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("cluster_networks_dualstack" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("cluster_networks_dualstack" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *ClusterDefaultConfig) validateClusterNetworksIPV4(formats strfmt.Registry) error {
	if swag.IsZero(m.ClusterNetworksIPV4) { // not required
		return nil
	}

	for i := 0; i < len(m.ClusterNetworksIPV4); i++ {
		if swag.IsZero(m.ClusterNetworksIPV4[i]) { // not required
			continue
		}

		if m.ClusterNetworksIPV4[i] != nil {
			if err := m.ClusterNetworksIPV4[i].Validate(formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("cluster_networks_ipv4" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("cluster_networks_ipv4" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *ClusterDefaultConfig) validateServiceNetworkCidr(formats strfmt.Registry) error {
	if swag.IsZero(m.ServiceNetworkCidr) { // not required
		return nil
	}

	if err := validate.Pattern("service_network_cidr", "body", m.ServiceNetworkCidr, `^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)[\/]([1-9]|[1-2][0-9]|3[0-2]?)$`); err != nil {
		return err
	}

	return nil
}

func (m *ClusterDefaultConfig) validateServiceNetworksDualstack(formats strfmt.Registry) error {
	if swag.IsZero(m.ServiceNetworksDualstack) { // not required
		return nil
	}

	for i := 0; i < len(m.ServiceNetworksDualstack); i++ {
		if swag.IsZero(m.ServiceNetworksDualstack[i]) { // not required
			continue
		}

		if m.ServiceNetworksDualstack[i] != nil {
			if err := m.ServiceNetworksDualstack[i].Validate(formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("service_networks_dualstack" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("service_networks_dualstack" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *ClusterDefaultConfig) validateServiceNetworksIPV4(formats strfmt.Registry) error {
	if swag.IsZero(m.ServiceNetworksIPV4) { // not required
		return nil
	}

	for i := 0; i < len(m.ServiceNetworksIPV4); i++ {
		if swag.IsZero(m.ServiceNetworksIPV4[i]) { // not required
			continue
		}

		if m.ServiceNetworksIPV4[i] != nil {
			if err := m.ServiceNetworksIPV4[i].Validate(formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("service_networks_ipv4" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("service_networks_ipv4" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

// ContextValidate validate this cluster default config based on the context it is used
func (m *ClusterDefaultConfig) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	var res []error

	if err := m.contextValidateClusterNetworksDualstack(ctx, formats); err != nil {
		res = append(res, err)
	}

	if err := m.contextValidateClusterNetworksIPV4(ctx, formats); err != nil {
		res = append(res, err)
	}

	if err := m.contextValidateServiceNetworksDualstack(ctx, formats); err != nil {
		res = append(res, err)
	}

	if err := m.contextValidateServiceNetworksIPV4(ctx, formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *ClusterDefaultConfig) contextValidateClusterNetworksDualstack(ctx context.Context, formats strfmt.Registry) error {

	for i := 0; i < len(m.ClusterNetworksDualstack); i++ {

		if m.ClusterNetworksDualstack[i] != nil {
			if err := m.ClusterNetworksDualstack[i].ContextValidate(ctx, formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("cluster_networks_dualstack" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("cluster_networks_dualstack" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *ClusterDefaultConfig) contextValidateClusterNetworksIPV4(ctx context.Context, formats strfmt.Registry) error {

	for i := 0; i < len(m.ClusterNetworksIPV4); i++ {

		if m.ClusterNetworksIPV4[i] != nil {
			if err := m.ClusterNetworksIPV4[i].ContextValidate(ctx, formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("cluster_networks_ipv4" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("cluster_networks_ipv4" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *ClusterDefaultConfig) contextValidateServiceNetworksDualstack(ctx context.Context, formats strfmt.Registry) error {

	for i := 0; i < len(m.ServiceNetworksDualstack); i++ {

		if m.ServiceNetworksDualstack[i] != nil {
			if err := m.ServiceNetworksDualstack[i].ContextValidate(ctx, formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("service_networks_dualstack" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("service_networks_dualstack" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

func (m *ClusterDefaultConfig) contextValidateServiceNetworksIPV4(ctx context.Context, formats strfmt.Registry) error {

	for i := 0; i < len(m.ServiceNetworksIPV4); i++ {

		if m.ServiceNetworksIPV4[i] != nil {
			if err := m.ServiceNetworksIPV4[i].ContextValidate(ctx, formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("service_networks_ipv4" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("service_networks_ipv4" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

// MarshalBinary interface implementation
func (m *ClusterDefaultConfig) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *ClusterDefaultConfig) UnmarshalBinary(b []byte) error {
	var res ClusterDefaultConfig
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
