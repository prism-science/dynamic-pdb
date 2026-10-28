package models

import (
	"time"

	"github.com/google/uuid"
)

type RoleKey string

const (
	RoleKeyAdmin    RoleKey = "admin"
	RoleKeyReviewer RoleKey = "reviewer"
)

type PermissionKey string

const (
	PermissionKeyRevisionsApprove PermissionKey = "revisions.approve"
	PermissionKeyRevisionsReject  PermissionKey = "revisions.reject"
	PermissionKeyRolesAssign      PermissionKey = "roles.assign"
	PermissionKeyRolesRevoke      PermissionKey = "roles.revoke"
	// PermissionKeyEntriesDelete is never stored. The sign-in response adds it for users who can
	// approve revisions while entry deletion is turned on, so the website knows to offer it.
	PermissionKeyEntriesDelete PermissionKey = "entries.delete"
)

type Role struct {
	ID        uuid.UUID
	Key       RoleKey
	Name      string
	CreatedAt time.Time
}

type Permission struct {
	ID          uuid.UUID
	Key         PermissionKey
	Name        string
	Description string
	CreatedAt   time.Time
}
