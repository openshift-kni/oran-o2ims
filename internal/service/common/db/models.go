/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package db

type Model interface {
	PrimaryKey() string
	TableName() string
	OnConflict() string
}
