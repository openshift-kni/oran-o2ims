/*
Copyright (c) 2024 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package v1alpha1

const (
	// FulfilledCondition is the condition that indicates that an request has been completely
	// and successfully completed.
	FulfilledCondition = "Fulfilled"

	// FailedConditions is a condition that indicates that an order cound't be completely
	// fulfilled.
	FailedCondition = "Failed"
)

type ConditionType string

// The following constants define the different types of conditions that will be set
const (
	Provisioned ConditionType = "Provisioned"
	Unknown     ConditionType = "Unknown" // indicates the condition has not been evaluated
)

type ConditionReason string

// The following constants define the different reasons that conditions will be set for
const (
	InProgress     ConditionReason = "InProgress"
	Completed      ConditionReason = "Completed"
	Unprovisioned  ConditionReason = "Unprovisioned"
	Failed         ConditionReason = "Failed"
	NotInitialized ConditionReason = "NotInitialized"
	TimedOut       ConditionReason = "TimedOut"
)
