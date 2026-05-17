#!/usr/bin/env bash
#
# demo-revoke.sh — Remove a user from the user-svc demo gate.
#
# Strips their line from ~/.hvo-deploy/demo-users, then pushes the result
# to user-svc as ALLOWED_USERS. The user's existing JWT/refresh token keeps
# working until expiry — invalidate immediately by rotating JWT_SECRET if
# that matters.
#
# Usage:
#   scripts/demo-revoke.sh <email>

set -euo pipefail

email="${1:-}"
if [[ -z "$email" ]]; then
  echo "usage: $0 <email>" >&2
  exit 2
fi
email_lc=$(echo "$email" | tr '[:upper:]' '[:lower:]')

repo_root=$(cd "$(dirname "$0")/.." && pwd)
fly_config="$repo_root/services/user-svc/fly.toml"
state_file="$HOME/.hvo-deploy/demo-users"

if [[ ! -f "$state_file" ]]; then
  echo "no demo users file found at $state_file — nothing to revoke" >&2
  exit 0
fi

# Remove matching line.
tmp=$(mktemp)
grep -v "^${email_lc}:" "$state_file" > "$tmp" || true
mv "$tmp" "$state_file"
chmod 600 "$state_file"

remaining=$(grep -c . "$state_file" || true)

if [[ "$remaining" -eq 0 ]]; then
  # Last user removed — unset entirely, which fully closes the gate
  # (parseAllowedUsers treats empty as nil = no users at all = no one in).
  echo ">>> Last user removed — unsetting ALLOWED_USERS on Fly..."
  fly secrets unset --config "$fly_config" ALLOWED_USERS
else
  joined=$(tr '\n' ',' < "$state_file" | sed 's/,$//')
  echo ">>> Updating ALLOWED_USERS (${remaining} user(s) remaining)..."
  fly secrets set --config "$fly_config" ALLOWED_USERS="$joined"
fi

echo "✓ Revoked: ${email}"
