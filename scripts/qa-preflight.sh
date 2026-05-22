#!/usr/bin/env sh
set -eu

check_env_file() {
  file="$1"
  [ -f "$file" ] || return 0

  secret="$(awk -F= '
    /^[[:space:]]*#/ { next }
    /^[[:space:]]*SESSION_SECRET[[:space:]]*=/ {
      value=$0
      sub(/^[^=]*=/, "", value)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      print value
    }
  ' "$file" | tail -n 1)"

  case "$secret" in
    replace-with-*|change-me|changeme)
      cat >&2 <<EOF
QA preflight refused placeholder SESSION_SECRET in $file.

Generate a QA-only secret and set it in .env.qa or .env.qa.local:

  openssl rand -hex 32
EOF
      exit 2
      ;;
  esac
}

check_env_file .env.qa
check_env_file .env.qa.local
