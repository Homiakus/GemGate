package gateway

import "context"

type downstreamContextKey struct{}

// tagDownstreamContext preserves the original client/request context as a value
// before http.Client applies its own provider timeout deadline. The transport
// can then distinguish a caller abort from a provider-side timeout.
func tagDownstreamContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, downstreamContextKey{}, ctx)
}

func downstreamRequestAborted(ctx context.Context) bool {
	original, ok := ctx.Value(downstreamContextKey{}).(context.Context)
	return ok && original.Err() != nil
}
