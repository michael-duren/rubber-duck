# Password change (and the exposed test account)

## Context

No way to change a password after signup. The production `mduren` account's
password appeared in deploy-session command history and should be rotated.

## Work

- `GET/POST /settings` (requireUser): current password + new password form,
  bcrypt verify then update `users.password_hash`.
- Invalidate the user's other sessions on change (delete from sessions
  where user_id and token_hash != current).
- One-off: rotate or delete the prod `mduren` user.

## Done when

Changing the password logs out other sessions and the old password stops
working.
