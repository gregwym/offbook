// Validate a final independent Product Experience audit before closing Goal #423.
// No network access; GitHub Action supplies main SHA and issue comments.
import { readFileSync } from 'node:fs';

export const JOURNEYS = ['first-run', 'daily-review', 'monthly-budget', 'net-worth', 'recovery', 'mobile'];
export const DIMENSIONS = ['coverage', 'correctness', 'friction', 'visual', 'feedback', 'cross-platform', 'resilience', 'reference'];
const SHA = /^[a-f0-9]{40}$/i;
const MARKER = '<!-- offbook-maturity:v1 ';
const isHttps = value => {
  try {
    const u = new URL(value);
    return u.protocol === 'https:' && Boolean(u.hostname) && !['example.com', 'example.org', 'example.net'].includes(u.hostname);
  } catch { return false; }
};
export function checkMaturity({ mainSha, comments = [], trustedActors = '' } = {}) {
  if (!SHA.test(mainSha || '')) return { ok: false, reason: 'Invalid main SHA' };
  const allowed = new Set(String(trustedActors).split(/[\\s,]+/).map(s => s.toLowerCase()).filter(Boolean));
  if (!allowed.size) return { ok: false, reason: 'No authorized maturity QA commenter configured' };
  const entries = [];
  for (const item of comments) {
    if (!allowed.has(String(item.user?.login ?? '').toLowerCase())) continue;
    for (const line of String(item.body || '').split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed.startsWith(MARKER) || !trimmed.endsWith(' -->')) continue;
      try {
        entries.push({ ...JSON.parse(trimmed.slice(MARKER.length, -4)), commentId: item.id });
      } catch {
        entries.push({ result: 'INVALID', commentId: item.id });
      }
    }
  }
  const last = entries.at(-1);
  if (!last) return { ok: false, reason: 'No offbook-maturity:v1 QA report on Goal #423' };
  if (last.result !== 'MATURE') return { ok: false, reason: 'Latest product QA did not recommend MATURE' };
  if (last.head_sha !== mainSha) return { ok: false, reason: 'QA report is stale: it does not target current main' };
  if (!isHttps(last.qa_run_url)) return { ok: false, reason: 'Missing real HTTPS independent QA evidence URL' };
  if (!last.zero_open_blockers || !last.soak_verified || !last.backup_restore_verified) {
    return { ok: false, reason: 'Outstanding core blockers, reliability soak or backup restore not verified' };
  }
  if (!Array.isArray(last.journeys) || last.journeys.length !== JOURNEYS.length) {
    return { ok: false, reason: 'Product QA report must cover every Daily Driver v1 journey' };
  }
  const covered = new Set();
  for (const journey of last.journeys) {
    if (!JOURNEYS.includes(journey?.id) || covered.has(journey.id)) {
      return { ok: false, reason: 'Missing, duplicated or unknown journey in QA scorecard' };
    }
    covered.add(journey.id);
    if (!Array.isArray(journey.evidence) || journey.evidence.length === 0 || !journey.evidence.every(isHttps)) {
      return { ok: false, reason: 'Missing real HTTPS screenshot/trace evidence for ' + journey.id };
    }
    for (const dimension of DIMENSIONS) {
      if (!Number.isInteger(journey.scores?.[dimension]) || journey.scores[dimension] < 3 || journey.scores[dimension] > 4) {
        return { ok: false, reason: 'Below-mature or unreviewed ' + dimension + ' in ' + journey.id };
      }
    }
  }
  return { ok: true, reason: 'MATURE evidence covers current main and all six journeys', reportCommentId: last.commentId };
}
if (process.argv[1]?.endsWith('maturity-gate.mjs')) {
  try {
    process.stdout.write(JSON.stringify(checkMaturity(JSON.parse(readFileSync(0, 'utf8')))) + '\n');
  } catch (err) {
    process.stderr.write('maturity-gate: ' + err.message + '\n');
    process.exitCode = 2;
  }
}
