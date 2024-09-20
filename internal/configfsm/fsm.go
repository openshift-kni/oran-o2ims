package configfsm

import (
	"context"
	"fmt"
	"time"

	"github.com/qmuntal/stateless"
)

type Trigger string

const (
	// Transitions to Missing state
	StartToMissing           Trigger = "Start->Missing"
	ClusterNotReadyToMissing Trigger = "ClusterNotReady->Missing"
	InProgressToMissing      Trigger = "InProgress->Missing"
	CompletedToMissing       Trigger = "Completed->Missing"
	TimedOutToMissing        Trigger = "TimedOut->Missing"
	OutOfDateToMissing       Trigger = "OutOfDate->Missing"

	// Transitions to ClusterNotReady state
	MissingToClusterNotReady    Trigger = "Missing->ClusterNotReady"
	InProgressToClusterNotReady Trigger = "InProgress->ClusterNotReady"
	TimedOutToClusterNotReady   Trigger = "TimedOut->ClusterNotReady"
	CompletedToClusterNotReady  Trigger = "Completed->ClusterNotReady"
	OutOfDateToClusterNotReady  Trigger = "OutOfDate->ClusterNotReady"

	// Transitions to InProgress state
	ClusterNotReadyToInProgress Trigger = "ClusterNotReady->InProgress"
	OutOfDateToInProgress       Trigger = "OutOfDate->InProgress"
	CompletedToInProgress       Trigger = "Completed->InProgress"

	// Transitions to TimedOut state
	ClusterNotReadyToTimedOut Trigger = "ClusterNotReady->TimedOut"
	InProgressToTimedOut      Trigger = "InProgress->TimedOut"

	// Transitions to Completed state
	InProgressToCompleted Trigger = "InProgress->Completed"

	// Transitions to OutOfDate state
	InProgressToOutOfDate      Trigger = "InProgress->OutOfDate"
	TimedOutToOutOfDate        Trigger = "TimedOut->OutOfDate"
	ClusterNotReadyToOutOfDate Trigger = "ClusterNotReady->OutOfDate"
	CompletedToOutOfDate       Trigger = "Completed->OutOfDate"

	// Self transitions
	MissingToMissing                 Trigger = "Missing->Missing"
	ClusterNotReadyToClusterNotReady Trigger = "ClusterNotReady->ClusterNotReady"
	InProgressToInProgress           Trigger = "InProgress->InProgress"
	TimedOutToTimedOut               Trigger = "TimedOut->TimedOut"
	CompletedToCompleted             Trigger = "Completed->Completed"
	OutOfDateToOutOfDate             Trigger = "OutOfDate->OutOfDate"

	// States
	Missing         = "Missing"
	Start           = "Start"
	ClusterNotReady = "ClusterNotReady"
	InProgress      = "InProgress"
	TimedOut        = "TimedOut"
	Completed       = "Completed"
	OutOfDate       = "OutOfDate"
)

// Runs the state machine as much as it can by triggering all Self transitions
func RunFSM(ctx context.Context, fsm *stateless.StateMachine, fsmHelper FsmHelper) (aState any, err error) {
	aState, err = fsm.State(ctx)
	if err != nil {
		return aState, fmt.Errorf("could not get state, err: %w", err)
	}
	switch aState {
	case Start:
		err = fsm.Fire(StartToMissing, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	case Missing:
		err = fsm.Fire(MissingToMissing, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	case ClusterNotReady:
		err = fsm.Fire(ClusterNotReadyToClusterNotReady, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	case InProgress:
		err = fsm.Fire(InProgressToInProgress, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	case TimedOut:
		err = fsm.Fire(TimedOutToTimedOut, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	case Completed:
		err = fsm.Fire(CompletedToCompleted, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	case OutOfDate:
		err = fsm.Fire(OutOfDateToOutOfDate, fsmHelper)
		if err != nil {
			return aState, fmt.Errorf("could not Fire state %s, err: %w", aState, err)
		}
	}
	// Update the current state
	aState, err = fsm.State(ctx)
	if err != nil {
		return aState, fmt.Errorf("could not get state, err: %w", err)
	}
	return aState, nil
}

// Initialize the state machine algorithm
func InitFSM(state string) (fsm *stateless.StateMachine, err error) {
	fsm = stateless.NewStateMachine(state)
	fsm.Configure(Start).Permit(StartToMissing, Missing)
	fsm.Configure(ClusterNotReady).
		OnEntry(func(_ context.Context, args ...any) error {
			fsmHelper := args[0].(FsmHelper)
			fmt.Println("Entering ClusterNotReady")
			if fsmHelper.IsTimedOut() {
				return fsm.Fire(ClusterNotReadyToTimedOut, fsmHelper)
			}
			if fsmHelper.IsClusterReady() {
				if fsmHelper.IsNonCompliantPolicyInEnforce() || fsmHelper.IsAllPoliciesCompliant() {
					return fsm.Fire(ClusterNotReadyToInProgress, fsmHelper)
				} else {
					return fsm.Fire(ClusterNotReadyToOutOfDate, fsmHelper)
				}
			}
			return nil
		}).
		Permit(ClusterNotReadyToMissing, Missing).
		Permit(ClusterNotReadyToInProgress, InProgress).
		Permit(ClusterNotReadyToTimedOut, TimedOut).
		Permit(ClusterNotReadyToOutOfDate, OutOfDate).
		PermitReentry(ClusterNotReadyToClusterNotReady)
	fsm.Configure(InProgress).
		OnEntry(func(_ context.Context, args ...any) error {
			fsmHelper := args[0].(FsmHelper)
			fmt.Println("Entering InProgress")
			if fsmHelper.IsResetNonCompliantAt() {
				fsmHelper.SetResetNonCompliantAtNow()
			}

			if fsmHelper.IsTimedOut() {
				return fsm.Fire(InProgressToTimedOut, fsmHelper)
			}
			if fsmHelper.IsAllPoliciesCompliant() {
				return fsm.Fire(InProgressToCompleted, fsmHelper)
			}
			if !fsmHelper.IsPoliciesMatched() {
				return fsm.Fire(InProgressToMissing, fsmHelper)
			}
			if !fsmHelper.IsClusterReady() {
				return fsm.Fire(InProgressToClusterNotReady, fsmHelper)
			}
			if !fsmHelper.IsNonCompliantPolicyInEnforce() && !fsmHelper.IsAllPoliciesCompliant() {
				return fsm.Fire(InProgressToOutOfDate, fsmHelper)
			}
			return nil
		}).
		Permit(InProgressToMissing, Missing).
		Permit(InProgressToClusterNotReady, ClusterNotReady).
		Permit(InProgressToTimedOut, TimedOut).
		Permit(InProgressToOutOfDate, OutOfDate).
		Permit(InProgressToCompleted, Completed).
		PermitReentry(InProgressToInProgress)
	fsm.Configure(OutOfDate).
		OnEntry(func(_ context.Context, args ...any) error {
			fsmHelper := args[0].(FsmHelper)
			fmt.Println("Entering InProgress")
			fsmHelper.ResetNonCompliantAt()
			if !fsmHelper.IsClusterReady() {
				return fsm.Fire(OutOfDateToClusterNotReady, fsmHelper)
			}
			if fsmHelper.IsNonCompliantPolicyInEnforce() {
				return fsm.Fire(OutOfDateToInProgress, fsmHelper)
			}
			return nil
		}).
		Permit(OutOfDateToMissing, Missing).
		Permit(OutOfDateToClusterNotReady, ClusterNotReady).
		Permit(OutOfDateToInProgress, InProgress).
		PermitReentry(OutOfDateToOutOfDate)
	fsm.Configure(Completed).
		OnEntry(func(_ context.Context, args ...any) error {
			fsmHelper := args[0].(FsmHelper)
			fmt.Println("Entering Completed")
			fsmHelper.ResetNonCompliantAt()

			if !fsmHelper.IsPoliciesMatched() {
				return fsm.Fire(CompletedToMissing, fsmHelper)
			}
			if !fsmHelper.IsClusterReady() {
				return fsm.Fire(CompletedToClusterNotReady, fsmHelper)
			}
			if !fsmHelper.IsNonCompliantPolicyInEnforce() &&
				!fsmHelper.IsAllPoliciesCompliant() {
				return fsm.Fire(CompletedToOutOfDate, fsmHelper)
			}
			return nil
		}).
		Permit(CompletedToMissing, Missing).
		Permit(CompletedToClusterNotReady, ClusterNotReady).
		Permit(CompletedToInProgress, InProgress).
		Permit(CompletedToOutOfDate, OutOfDate).
		PermitReentry(CompletedToCompleted)

	fsm.Configure(Missing).
		OnEntry(func(_ context.Context, args ...any) error {
			fsmHelper := args[0].(FsmHelper)
			fmt.Println("Entering Missing")
			fsmHelper.ResetNonCompliantAt()
			if fsmHelper.IsPoliciesMatched() {
				return fsm.Fire(MissingToClusterNotReady, fsmHelper)
			}
			return nil
		}).
		Permit(MissingToClusterNotReady, ClusterNotReady).
		PermitReentry(MissingToMissing)

	fsm.Configure(TimedOut).
		OnEntry(func(_ context.Context, args ...any) error {
			fsmHelper := args[0].(FsmHelper)
			fmt.Println("Entering TimedOut")

			if !fsmHelper.IsPoliciesMatched() {
				return fsm.Fire(TimedOutToMissing, fsmHelper)
			}
			if !fsmHelper.IsNonCompliantPolicyInEnforce() &&
				!fsmHelper.IsAllPoliciesCompliant() {
				return fsm.Fire(TimedOutToOutOfDate, fsmHelper)
			}
			return nil
		}).
		Permit(TimedOutToClusterNotReady, ClusterNotReady).
		Permit(TimedOutToOutOfDate, OutOfDate).
		Permit(TimedOutToMissing, Missing).
		PermitReentry(TimedOutToTimedOut)

	return fsm, nil
}

// Helper Interface to collect all functions needed for state machine decisions
type FsmHelper interface {
	IsPoliciesMatched() bool
	ResetNonCompliantAt()
	IsResetNonCompliantAt() bool
	SetResetNonCompliantAtNow()
	IsTimedOut() bool
	IsClusterReady() bool
	IsAllPoliciesCompliant() bool
	IsNonCompliantPolicyInEnforce() bool
}

// Helper struct to store information needed for state machine decisions
type BaseFSMHelper struct {
	CurrentState                string
	ClusterReady                bool
	PoliciesMatched             bool
	AllPoliciesCompliant        bool
	NonCompliantAt              time.Time
	NonCompliantPolicyInEnforce bool
	ConfigTimeout               int
}

// Returns true if there are policies matched to the managed cluster, false otherwise
func (h *BaseFSMHelper) IsPoliciesMatched() bool {
	return h.PoliciesMatched
}

// Resets the NonCompliantAt field to zero
func (h *BaseFSMHelper) ResetNonCompliantAt() {
	h.NonCompliantAt = time.Time{}
}

// Returns true if NonCompliantAt is was resetm to zero, false otherwise
func (h *BaseFSMHelper) IsResetNonCompliantAt() bool {
	return h.NonCompliantAt == time.Time{}
}

// Sets the NonCompliantAt to the current time
func (h *BaseFSMHelper) SetResetNonCompliantAtNow() {
	println("Set NonCompliantAt to now")
	h.NonCompliantAt = time.Now()
}

// Returns true if the Managed cluster is ready for policy enforcement, false otherwise
func (h *BaseFSMHelper) IsClusterReady() bool {
	return h.ClusterReady
}

// Returns true if all policies are enforced and compliant, false otherwise
func (h *BaseFSMHelper) IsAllPoliciesCompliant() bool {
	return h.AllPoliciesCompliant
}

// Returns true if there is at least one non compliant policy in enforce, false otherwise
func (h *BaseFSMHelper) IsNonCompliantPolicyInEnforce() bool {
	return h.NonCompliantPolicyInEnforce
}
