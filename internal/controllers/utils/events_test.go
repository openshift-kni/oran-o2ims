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
	"testing"
	"time"
)

func TestEventHistory_IsTimedOut(t *testing.T) {
	type fields struct {
		History []*Event
	}
	type args struct {
		now     time.Time
		timeout time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "no event - timedout false ",
			fields: fields{
				History: []*Event{},
			},
			args: args{
				now:     time.Date(2024, time.October, 8, 12, 0, 2, 0, time.UTC),
				timeout: time.Second,
			},
			want: false,
		},
		{
			name: "timed out - no overall state change during history",
			fields: fields{
				History: []*Event{
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
				},
			},
			args: args{
				now:     time.Date(2024, time.October, 8, 12, 0, 2, 0, time.UTC), // 2s have passed
				timeout: time.Second,
			},
			want: true,
		},
		{
			name: "timed out - reversed event time order",
			fields: fields{
				History: []*Event{
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 5, 0, time.UTC), State: InProgress},
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
				},
			},
			args: args{
				now:     time.Date(2024, time.October, 8, 12, 0, 7, 0, time.UTC), // 2s have passed
				timeout: time.Second,
			},
			want: true,
		},
		{
			name: "timed out - one object is completed, overall timeout after 3 s",
			fields: fields{
				History: []*Event{
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 1, 0, time.UTC), State: Completed},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 2, 0, time.UTC), State: InProgress},
				},
			},
			args: args{
				now:     time.Date(2024, time.October, 8, 12, 0, 3, 0, time.UTC), // 2s have passed
				timeout: time.Second,
			},
			want: true,
		},
		{
			name: "timed out - both objects completed after 2 seconds",
			fields: fields{
				History: []*Event{
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: InProgress},
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 1, 0, time.UTC), State: Completed},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 2, 0, time.UTC), State: Completed},
				},
			},
			args: args{
				now:     time.Date(2024, time.October, 8, 12, 0, 3, 0, time.UTC), // 2s have passed
				timeout: time.Second,
			},
			want: false,
		},
		{
			name: "timed out - 5 events becomes completed between 5 and 6 seconds and returns to non-compliant. The timeout is reset after transitioning to completed state",
			fields: fields{
				History: []*Event{
					// Event 1
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: Completed},
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 1, 0, time.UTC), State: InProgress},
					{ObjectID: "event1", Timestamp: time.Date(2024, time.October, 8, 12, 0, 2, 0, time.UTC), State: Completed},
					// Event 2
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: Completed},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 1, 0, time.UTC), State: InProgress},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 2, 0, time.UTC), State: InProgress},
					{ObjectID: "event2", Timestamp: time.Date(2024, time.October, 8, 12, 0, 3, 0, time.UTC), State: Completed},
					// Event 3
					{ObjectID: "event3", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: Completed},
					{ObjectID: "event3", Timestamp: time.Date(2024, time.October, 8, 12, 0, 1, 0, time.UTC), State: InProgress},
					{ObjectID: "event3", Timestamp: time.Date(2024, time.October, 8, 12, 0, 3, 0, time.UTC), State: InProgress},
					{ObjectID: "event3", Timestamp: time.Date(2024, time.October, 8, 12, 0, 4, 0, time.UTC), State: Completed},
					// Event 4
					{ObjectID: "event4", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: Completed},
					{ObjectID: "event4", Timestamp: time.Date(2024, time.October, 8, 12, 0, 4, 0, time.UTC), State: InProgress},
					{ObjectID: "event4", Timestamp: time.Date(2024, time.October, 8, 12, 0, 5, 0, time.UTC), State: Completed},
					// Event5
					{ObjectID: "event5", Timestamp: time.Date(2024, time.October, 8, 12, 0, 0, 0, time.UTC), State: Completed},
					{ObjectID: "event5", Timestamp: time.Date(2024, time.October, 8, 12, 0, 6, 0, time.UTC), State: InProgress},
					{ObjectID: "event5", Timestamp: time.Date(2024, time.October, 8, 12, 0, 8, 0, time.UTC), State: Completed},
				},
			},
			args: args{
				now:     time.Date(2024, time.October, 8, 12, 0, 3, 0, time.UTC), // 2s have passed
				timeout: time.Second,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &EventHistory{
				History: tt.fields.History,
			}
			if got := h.IsTimedOut(tt.args.now, tt.args.timeout); got != tt.want {
				t.Errorf("EventHistory.IsTimedOut() = %v, want %v", got, tt.want)
			}
		})
	}
}
