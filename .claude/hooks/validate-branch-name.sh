#!/bin/bash
INPUT=$(cat)
BRANCH=$(echo "$INPUT" | jq -r '.tool_input.command' | sed 's/git checkout -b //' | sed 's/ .*//')

if [[ $BRANCH =~ ^(feature|fix|chore|docs)/[0-9]+-[a-z0-9-]+$ ]]; then
  exit 0
else
  echo "Branch must follow: {type}/{issue-number}-{slug}" >&2
  echo "Examples: feature/12-plaid-link, fix/34-decimal-rounding" >&2
  exit 2
fi
