#!/bin/sh
set -eu

manager="$(node -p "require('./web/package.json').packageManager")"
attempt=1
until corepack install --global "$manager" && pnpm --dir web --version; do
  if [ "$attempt" -ge 3 ]; then
    echo "could not download the pinned package manager after $attempt attempts" >&2
    exit 1
  fi
  sleep "$((attempt * 5))"
  attempt="$((attempt + 1))"
done
