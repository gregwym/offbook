#!/usr/bin/env sh
set -eu

./scripts/qa-assert-role.sh

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/qa-append-ledger.sh <markdown-file>" >&2
  exit 2
fi

owner="${GITHUB_REPOSITORY_OWNER:-gregwym}"
repo="${GITHUB_REPOSITORY_NAME:-offbook}"
discussion="${QA_LEDGER_DISCUSSION_NUMBER:-199}"

discussion_id="$(gh api graphql \
  -f owner="$owner" \
  -f repo="$repo" \
  -F number="$discussion" \
  -f query='query($owner:String!, $repo:String!, $number:Int!) { repository(owner:$owner, name:$repo) { discussion(number:$number) { id } } }' \
  --jq '.data.repository.discussion.id')"

gh api graphql \
  -f discussionId="$discussion_id" \
  -f body="$(cat "$1")" \
  -f query='mutation($discussionId:ID!, $body:String!) { addDiscussionComment(input:{discussionId:$discussionId, body:$body}) { comment { url } } }' \
  --jq '.data.addDiscussionComment.comment.url'
