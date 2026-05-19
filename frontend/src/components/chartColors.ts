// Shared fallback palette for pie/donut charts. Server-returned slices can
// carry their own color (categories do); when they don't (e.g. investment
// asset-class buckets), components index into this list deterministically.
export const FALLBACK_PIE_COLORS = [
  '#6366F1',
  '#F59E0B',
  '#10B981',
  '#EC4899',
  '#3B82F6',
  '#EF4444',
  '#A855F7',
  '#64748B',
]
