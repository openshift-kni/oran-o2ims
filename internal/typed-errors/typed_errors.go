/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package typederrors

import (
	"errors"
	"fmt"
)

// GenericError is an error structure containing common fields to be
// embedded by specific error types defined below
type GenericError struct {
	Message string
	Err     error
}

func (ge GenericError) Error() string {
	return ge.Message
}

func (ge GenericError) Unwrap() error {
	return ge.Err
}

// ConfigMapError type
type ConfigMapError struct {
	GenericError
}

func NewConfigMapError(err error, format string, args ...interface{}) error {
	return ConfigMapError{
		GenericError: GenericError{fmt.Sprintf(format, args...), err},
	}
}

func IsConfigMapError(target error) bool {
	var e ConfigMapError
	return errors.As(target, &e)
}

// TokenError type
type TokenError struct {
	GenericError
}

func NewTokenError(err error, format string, args ...interface{}) error {
	return TokenError{
		GenericError: GenericError{fmt.Sprintf(format, args...), err},
	}
}

func IsTokenError(target error) bool {
	var e TokenError
	return errors.As(target, &e)
}

// SecretError type
type SecretError struct {
	GenericError
}

func NewSecretError(err error, format string, args ...interface{}) error {
	return SecretError{
		GenericError: GenericError{fmt.Sprintf(format, args...), err},
	}
}

func IsSecretError(target error) bool {
	var e SecretError
	return errors.As(target, &e)
}

// RetriableError type
type RetriableError struct {
	GenericError
}

func NewRetriableError(err error, format string, args ...interface{}) error {
	return RetriableError{
		GenericError: GenericError{fmt.Sprintf(format, args...), err},
	}
}

func IsRetriableError(target error) bool {
	var e RetriableError
	return errors.As(target, &e)
}

// NonRetriableError type
type NonRetriableError struct {
	GenericError
}

func NewNonRetriableError(err error, format string, args ...interface{}) error {
	return NonRetriableError{
		GenericError: GenericError{fmt.Sprintf(format, args...), err},
	}
}

func IsNonRetriableError(target error) bool {
	var e NonRetriableError
	return errors.As(target, &e)
}

// InputError wraps a standard error and provides a custom error type for input-related errors
type InputError struct {
	err error
}

func (i *InputError) Error() string {
	return i.err.Error()
}

func NewInputError(format string, args ...interface{}) *InputError {
	return &InputError{
		err: fmt.Errorf(format, args...),
	}
}

func IsInputError(err error) bool {
	var inputErr *InputError

	return errors.As(err, &inputErr)
}
