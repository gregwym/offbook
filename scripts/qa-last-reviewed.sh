#!/usr/bin/env sh
set -eu

./scripts/qa-assert-role.sh

owner="${GITHUB_REPOSITORY_OWNER:-gregwym}"
repo="${GITHUB_REPOSITORY_NAME:-offbook}"
discussion="${QA_LEDGER_DISCUSSION_NUMBER:-199}"

query='
query($owner:String!, $repo:String!, $number:Int!) {
  repository(owner:$owner, name:$repo) {
    discussion(number:$number) {
      comments(last:50) {
        nodes {
          body
          createdAt
        }
      }
    }
  }
}'

body="$(gh api graphql -f owner="$owner" -f repo="$repo" -F number="$discussion" -f query="$query" --jq '.data.repository.discussion.comments.nodes | reverse | .[].body' | awk '
  match($0, /Reviewed commit:[[:space:]]*`?[0-9a-f]{7,40}`?/) {
    line = substr($0, RSTART, RLENGTH)
    gsub(/Reviewed commit:[[:space:]]*`?/, "", line)
    gsub(/`/, "", line)
    print line
    exit
  }
')"

if [ -z "$body" ]; then
  exit 1
fi

printf '%s\n' "$body"
