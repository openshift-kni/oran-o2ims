/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"regexp"
)

const (
	LabelPrefixResources = "resources.oran.openshift.io/"
	LabelResourcePoolID  = LabelPrefixResources + "resourcePoolId"
	LabelSiteID          = LabelPrefixResources + "siteId"

	LabelPrefixResourceSelector = "resourceselector.oran.openshift.io/"

	LabelPrefixInterfaces = "interfacelabel.oran.openshift.io/"
)

// The following regex pattern is used to find interface labels
var REPatternInterfaceLabel = regexp.MustCompile(`^` + LabelPrefixInterfaces + `(.*)`)

// The following regex pattern is used to check resourceselector label pattern
var REPatternResourceSelectorLabel = regexp.MustCompile(`^` + LabelPrefixResourceSelector)
