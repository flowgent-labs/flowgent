package handler

import (
	"context"

	"github.com/flowgent-labs/flowgent/api/pkg/authz"
)

func authenticatedUserID(ctx context.Context) string {
	return authz.PrincipalIDFromContext(ctx)
}
