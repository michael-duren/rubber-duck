# CSRF tokens on state-changing forms

## Context

M1 skipped CSRF protection, leaning on SameSite=Lax cookies. Lax blocks
cross-site POSTs from forms in most browsers, but explicit tokens are the
correct belt-and-braces for login/logout/submit/settings forms.

## Work

- Per-session CSRF token (derive from session token hash or store
  alongside the session row).
- Hidden input in every POST form (layout-level templ component); verify in
  a middleware wrapping the web mux POST routes.
- Exempt /api/v1 (bearer-key auth, no cookies).

## Done when

A POST without the token is rejected 403; all existing web tests updated
and green.
