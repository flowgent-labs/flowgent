package handler

import (
	"context"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
)

func authenticatedUserID(ctx context.Context) string {
	if user, ok := auth.UserFromContext(ctx); ok {
		return user.UserID
	}
	return ""
}
