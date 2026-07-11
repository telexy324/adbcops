package skillframework

import (
	"context"

	"aiops-platform/backend/internal/model"
)

type actorContextKey struct{}

func ContextWithActor(ctx context.Context, actor *model.AppUser) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func ActorFromContext(ctx context.Context) *model.AppUser {
	actor, _ := ctx.Value(actorContextKey{}).(*model.AppUser)
	return actor
}
