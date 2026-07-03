package views

import "context"

type csrfKey struct{}

// WithCSRFToken attaches the request's CSRF token to the context so
// form-rendering templates can embed it without threading it through every
// view function's parameters.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfKey{}, token)
}

func csrfToken(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey{}).(string)
	return t
}
