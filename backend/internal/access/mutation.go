package access

import (
	"context"
	"database/sql"
)

type mutationKey struct{}
type Mutation struct {
	Actor string
	Check func(*sql.Tx, string) error
}

func WithMutation(ctx context.Context, m Mutation) context.Context {
	return context.WithValue(ctx, mutationKey{}, m)
}
func CheckMutation(ctx context.Context, tx *sql.Tx, resource string) error {
	m, _ := ctx.Value(mutationKey{}).(Mutation)
	if m.Check != nil {
		return m.Check(tx, resource)
	}
	return nil
}
func Actor(ctx context.Context, owner string) string {
	m, _ := ctx.Value(mutationKey{}).(Mutation)
	if m.Actor != "" {
		return m.Actor
	}
	return owner
}
