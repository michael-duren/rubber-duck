# User API tokens for CLI submission

## Context

`gc submit` needs to authenticate as a user from the terminal. The site
only has session cookies; agent api_keys are not user-scoped.

## Work

- Migration: `user_tokens` table mirroring `api_keys` (sha256 hash, name,
  revoked_at) plus `user_id` FK.
- `internal/auth` already has the token mint/hash helpers — reuse.
- Profile page: "Create CLI token" button (shown once) + revoke list.
- Auth middleware accepting `Authorization: Bearer gc_u_<hex>` on the
  submission endpoints (web POST or a new JSON endpoint — see issue 04).

## Done when

A token minted on the profile page authenticates a submission POST without
a session cookie; revoking it returns 401.
