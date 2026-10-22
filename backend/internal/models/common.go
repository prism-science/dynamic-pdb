package models

import "fmt"

type RevisionState string

const (
	RevisionStatePending  RevisionState = "pending"
	RevisionStateInReview RevisionState = "in_review"
	RevisionStateActive   RevisionState = "active"
	RevisionStateRejected RevisionState = "rejected"
	RevisionStateArchived RevisionState = "archived"
)

func (state RevisionState) Validate() error {
	switch state {
	case RevisionStatePending,
		RevisionStateInReview,
		RevisionStateActive,
		RevisionStateRejected,
		RevisionStateArchived:
		return nil
	default:
		return fmt.Errorf("unknown revision state: %s", state)
	}
}
