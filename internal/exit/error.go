/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package exit

import "fmt"

// Error is an error type that contains a process exit code. This is itended for situations where
// you want to call os.Exit only in one place, but also want some deeply nested functions to decide
// what should be the exit code.
type Error int

// Error is the implementation of the error interface.
func (e Error) Error() string {
	return fmt.Sprintf("%d", e)
}

// Code returns the exit code.
func (e Error) Code() int {
	return int(e)
}
