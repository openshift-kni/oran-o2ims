/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"sort"
	"time"
)

const (
	// Generic Event states
	// InProgress indicates a in progress state
	InProgress = "InProgress"
	// InProgress indicates a completed state
	Completed = "Completed"
)

// Event is the generic representation of an event
type Event struct {
	ObjectID  string
	Timestamp time.Time
	State     string
}

// EventHistory contains a slice of events
type EventHistory struct {
	History []*Event
}

// IsTimedOut looks at the history an determines the overall timedout state implied
// by the events.
// returns true if for any ObjectID:
// - the overall state was InProgress from the initial time for more than timeout
// - the overall state was InProgress from the last transition to InProgress for more than timeout
// returns false if for any ObjectID:
// - the last transition is Completed
// - no events to process
func (h EventHistory) IsTimedOut(now time.Time, timeout time.Duration) bool {
	// If no Events are present, we can't timeout
	if len(h.History) == 0 {
		return false
	}
	// Sort events chronologically from old to new
	sort.Slice(h.History, func(i, j int) bool {
		return h.History[i].Timestamp.Before(h.History[j].Timestamp)
	})
	// currentState stores the running value of the State (InProgress or Completed) for each ObjectID
	currentState := map[string]string{}
	// Initialize the current state to InProgress
	for _, event := range h.History {
		currentState[event.ObjectID] = InProgress
	}
	// initialTime records the timestamp for the first Event among all events
	initialTime := h.History[0].Timestamp
	// lastCompleted records the last time the overall state for all event transitioned to Completed
	lastCompleted := time.Time{}
	// lastInProgress records the last time the overall state for all event transitioned to InProgress
	lastInProgress := time.Time{}
	// Process each event chronologically one by one and recalculate the
	// current state (InProgress or Completed)
	for _, event := range h.History {
		// If the state has not changed for this ObjectID, continue to the next event
		if currentState[event.ObjectID] == event.State {
			continue
		}
		// Update the current state for the ObjectID
		currentState[event.ObjectID] = event.State
		// Calculate the new overall state (InProgress or Completed) for all ObjectIDs
		overallState := Completed
		for _, state := range currentState {
			if state != Completed {
				overallState = InProgress
				break
			}
		}
		// Record last Completed overall state
		if overallState == Completed {
			lastCompleted = event.Timestamp
		}
		// Record last InProgress overall state
		if overallState == InProgress {
			lastInProgress = event.Timestamp
		}
	}
	if lastInProgress.After(lastCompleted) && !lastCompleted.IsZero() {
		// Overall state is not Completed start timing out after the lastInProgress
		return now.Sub(lastCompleted) > timeout
	}
	if lastCompleted.After(lastInProgress) && !lastCompleted.IsZero() {
		// Overall state is Completed
		return false
	}
	if lastCompleted.IsZero() && !initialTime.IsZero() {
		// Overall state is in progress from the beginning
		return now.Sub(initialTime) > timeout
	}
	// If no Events are present, we can't timeout
	return false
}
