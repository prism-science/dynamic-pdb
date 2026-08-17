package models

type RevisionState string

const (
	RevisionStatePending  RevisionState = "pending"
	RevisionStateInReview RevisionState = "in_review"
	RevisionStateActive   RevisionState = "active"
	RevisionStateRejected RevisionState = "rejected"
	RevisionStateDeleted  RevisionState = "deleted"
)
