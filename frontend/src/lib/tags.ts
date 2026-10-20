import type { TagCount } from '../types/transaction'

// Mirrors the backend limits in service/transaction_tags.go (#544).
export const MAX_TAG_LENGTH = 40
const MAX_TAGS = 10

export function cleanTag(raw: string): string {
  return raw.split(/\s+/).filter(Boolean).join(' ')
}

// mergeTags adds comma-separated tags from raw to current, mirroring the
// server's normalization: whitespace collapsed, blanks dropped, case-insensitive
// duplicates skipped, and a tag the user already uses keeps that spelling.
export function mergeTags(current: string[], raw: string, known: TagCount[]): { next: string[] | null; error: string | null } {
  let next = current
  for (const part of raw.split(',')) {
    const t = cleanTag(part)
    if (!t) continue
    if ([...t].length > MAX_TAG_LENGTH) return { next: null, error: `Keep each tag to ${MAX_TAG_LENGTH} characters or fewer.` }
    if (next.some((x) => x.toLowerCase() === t.toLowerCase())) continue
    if (next.length >= MAX_TAGS) return { next: null, error: `A transaction can have up to ${MAX_TAGS} tags.` }
    const k = known.find((s) => s.name.toLowerCase() === t.toLowerCase())
    next = [...next, k ? k.name : t]
  }
  return { next, error: null }
}

