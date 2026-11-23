package auth

import "context"

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID                                                       int64
	Username                                                     string
	IsOwner, IsAdmin, AutoApprove, MustChangePassword, ViaAPIKey bool
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
