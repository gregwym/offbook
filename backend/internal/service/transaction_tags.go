package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// Tag limits (#544).
const (
	MaxTagLength = 40
	MaxTags      = model.MaxTagsPerRow
)

// ErrInvalidTags is returned when a tag is too long or a row has too many.
var ErrInvalidTags = errors.New("invalid tags")

// NormalizeTags trims each tag, collapses inner whitespace, drops blanks and
// case-insensitive duplicates (first spelling wins), and enforces the
// per-tag length and per-row count limits. Never returns nil.
func NormalizeTags(in []string) (model.Tags, error) {
	out := model.Tags{}
	seen := map[string]bool{}
	for _, raw := range in {
		tag := strings.Join(strings.Fields(raw), " ")
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > MaxTagLength {
			return nil, fmt.Errorf("%w: %q is longer than %d characters", ErrInvalidTags, tag, MaxTagLength)
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	if len(out) > MaxTags {
		return nil, fmt.Errorf("%w: at most %d tags per transaction", ErrInvalidTags, MaxTags)
	}
	return out, nil
}

// canonicalTags rewrites each tag to the spelling the user already uses for
// it, so "vacation" and "Vacation" never split into two tags.
func (s *TransactionService) canonicalTags(ctx context.Context, userID int64, tags model.Tags) (model.Tags, error) {
	return canonicalizeTags(ctx, s.repo, userID, tags)
}

// canonicalizeTags is canonicalTags for any caller holding a transaction
// repository (rules reuse it for their add_tags, #546).
func canonicalizeTags(ctx context.Context, repo repository.TransactionRepository, userID int64, tags model.Tags) (model.Tags, error) {
	if len(tags) == 0 {
		return tags, nil
	}
	existing, err := repo.ListTags(ctx, userID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]string, len(existing))
	for _, t := range existing {
		if _, ok := byKey[strings.ToLower(t.Name)]; !ok {
			byKey[strings.ToLower(t.Name)] = t.Name
		}
	}
	for i, t := range tags {
		if c, ok := byKey[strings.ToLower(t)]; ok {
			tags[i] = c
		}
	}
	return tags, nil
}

// ListTags returns the user's tags with row counts (#544).
func (s *TransactionService) ListTags(ctx context.Context, userID int64) ([]repository.TagCount, error) {
	return s.repo.ListTags(ctx, userID)
}
