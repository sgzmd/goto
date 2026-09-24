package web

type contextKey string

const (
	UserEmailContextKey contextKey = "user_email"
	CSRFTokenContextKey contextKey = "csrf_token"
)
