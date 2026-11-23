package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	domainmodels "dynamic-pdb/backend/internal/models"
)

// Review flags are open schema_migration_review rows shown on a public record (researcher answer
// 7). Everyone sees whether a record is flagged for review; only reviewers and admins see the
// issues and may filter by flags. Signing in grants neither role: a role is a user_roles row,
// and nothing in the API writes one.

// reviewFlagRoles are the roles that see review flag issues and may use the flagged filter.
var reviewFlagRoles = []domainmodels.RoleKey{domainmodels.RoleKeyReviewer, domainmodels.RoleKeyAdmin}

// errFlaggedFilterForbidden is a search with the flagged filter from a caller who may not use it.
var errFlaggedFilterForbidden = errors.New("the flagged filter is for reviewers and admins")

// seesReviewFlags reports whether the caller may see review flag issues and use the flagged
// filter: a signed-in user with the reviewer or admin role. An anonymous caller costs no query.
func (s *Server) seesReviewFlags(ctx context.Context) (bool, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return false, nil
	}
	return s.userSeesReviewFlags(ctx, user.ID)
}

// seesReviewFlagsOrPublic is seesReviewFlags for a response that can fall back to what the
// public sees. A failed role lookup is logged and answers false: the caller loses the issues,
// never gets issues it may not see, and the rest of the response still works.
func (s *Server) seesReviewFlagsOrPublic(ctx context.Context) bool {
	sees, err := s.seesReviewFlags(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "review flag access check failed; answering as for the public", "err", err)
		return false
	}
	return sees
}

// userSeesReviewFlags reports whether the user holds the reviewer or admin role.
func (s *Server) userSeesReviewFlags(ctx context.Context, userID uuid.UUID) (bool, error) {
	allowed, err := s.authorizer.HasAnyRole(ctx, userID, reviewFlagRoles...)
	if err != nil {
		return false, fmt.Errorf("check review flag access of user %s: %w", userID, err)
	}
	return allowed, nil
}

// usesFlaggedFilter reports whether the search filters by review flags.
func usesFlaggedFilter(search *parsedModelSearch) bool {
	return search != nil && search.filters.Flagged != nil
}

// checkFlaggedFilter refuses a search that uses the flagged filter when its caller may not see
// review flags. GET /v1/models and the searches of POST /v1/selections both call it on what
// modelSearchFromParams read.
func checkFlaggedFilter(search *parsedModelSearch, seesReviewFlags bool) error {
	if !usesFlaggedFilter(search) || seesReviewFlags {
		return nil
	}
	return errFlaggedFilterForbidden
}

// reviewFlagFields is what a response tells its caller about a record's open flags: whether
// there are any, for everyone, and the flags themselves only for reviewers and admins.
func reviewFlagFields(flags []domainmodels.ReviewFlag, seesReviewFlags bool) (bool, *[]ReviewFlag) {
	flagged := len(flags) > 0
	if !seesReviewFlags {
		return flagged, nil
	}
	return flagged, ptr(reviewFlagsFromModels(flags))
}

func reviewFlagsFromModels(flags []domainmodels.ReviewFlag) []ReviewFlag {
	items := make([]ReviewFlag, 0, len(flags))
	for _, flag := range flags {
		items = append(items, ReviewFlag{
			ObjectType: flag.ObjectType,
			ObjectId:   flag.ObjectID,
			Issue:      flag.Issue,
			FlaggedAt:  flag.FlaggedAt,
		})
	}
	return items
}

// varyByCaller marks a response whose body depends on who is asking, so a cache never hands a
// reviewer's answer to anyone else.
func varyByCaller(w http.ResponseWriter) {
	w.Header().Add("Vary", "Authorization")
}
