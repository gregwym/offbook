#!/usr/bin/env bash
# Retention for Offbook backups: grandfather-father-son.
#
#   prune.sh <backup_dir> <project> <keep_daily> <keep_weekly>
#
# Keeps the <keep_daily> most-recent dumps unconditionally, PLUS the newest dump
# of each of the <keep_weekly> most-recent ISO weeks (older than the daily
# window). Everything else is deleted. This bounds disk use while preserving a
# longer weekly tail than a flat "keep N" would.
#
# Pure filesystem logic — no DB, no compose — so it is trivially testable in
# isolation. Deliberately avoids bash-4-only features (mapfile, associative
# arrays) so it runs on the Linux hosts AND on a macOS dev box (bash 3.2). ISO-
# week bucketing needs GNU `date -d`; without it, it degrades safely to keeping
# the newest (keep_daily + keep_weekly) dumps and warns.
set -euo pipefail

dir="${1:?backup dir required}"
project="${2:?project required}"
keep_daily="${3:?keep_daily required}"
keep_weekly="${4:?keep_weekly required}"

cd "$dir" 2>/dev/null || { echo "prune: $dir does not exist, nothing to do"; exit 0; }

# Newest first. Filenames embed a lexicographically-sortable timestamp
# (<project>-YYYYmmdd-HHMMSS.dump), so a reverse name sort is newest-first.
all=()
while IFS= read -r line; do
	[ -n "$line" ] && all+=("$line")
done < <(ls -1 "${project}"-*.dump 2>/dev/null | sort -r)

if [ "${#all[@]}" -eq 0 ]; then
	echo "prune: no dumps in $dir yet"
	exit 0
fi

# GNU date probe (BSD/macOS date lacks -d).
gnu_date=1
date -d '2020-01-01' +%G >/dev/null 2>&1 || gnu_date=0

# keep_list is a newline-delimited set of filenames to retain; membership is
# tested with grep -qxF (exact whole-line match — portable, no associative
# arrays). The daily and weekly passes cover disjoint files, so appending
# unconditionally never duplicates in a way that matters.
keep_list=""
mark_keep() { keep_list="${keep_list}${1}"$'\n'; }
is_kept()   { printf '%s' "$keep_list" | grep -qxF -- "$1"; }

# 1. Daily window: newest keep_daily unconditionally.
i=0
for f in "${all[@]}"; do
	[ "$i" -ge "$keep_daily" ] && break
	mark_keep "$f"
	i=$((i + 1))
done

if [ "$gnu_date" -eq 1 ]; then
	# 2. Weekly tail: among the OLDER dumps, keep the newest per ISO week for the
	#    most-recent keep_weekly distinct weeks.
	weeks_seen=0
	last_week=""
	idx=0
	for f in "${all[@]}"; do
		idx=$((idx + 1))
		[ "$idx" -le "$keep_daily" ] && continue
		[ "$weeks_seen" -ge "$keep_weekly" ] && break
		# Extract YYYYmmdd from <project>-YYYYmmdd-HHMMSS.dump, format as
		# YYYY-MM-DD so GNU date parses it unambiguously.
		stamp="${f#"${project}"-}"; day="${stamp%%-*}"
		iso="$(date -d "${day:0:4}-${day:4:2}-${day:6:2}" +%G-%V 2>/dev/null || true)"
		[ -z "$iso" ] && continue
		if [ "$iso" != "$last_week" ]; then
			mark_keep "$f"
			weeks_seen=$((weeks_seen + 1))
			last_week="$iso"
		fi
	done
else
	# Fallback: keep an extra keep_weekly newest dumps instead of week buckets.
	echo "prune: GNU 'date -d' unavailable; falling back to keep newest $((keep_daily + keep_weekly))" >&2
	i=0
	for f in "${all[@]}"; do
		[ "$i" -ge "$((keep_daily + keep_weekly))" ] && break
		mark_keep "$f"
		i=$((i + 1))
	done
fi

# 3. Delete anything not marked keep.
kept=0
deleted=0
for f in "${all[@]}"; do
	if is_kept "$f"; then
		kept=$((kept + 1))
	else
		rm -f "$f"
		echo "prune: removed $f"
		deleted=$((deleted + 1))
	fi
done
echo "prune: kept $kept, removed $deleted (daily=$keep_daily, weekly=$keep_weekly)"
