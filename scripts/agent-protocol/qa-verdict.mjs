// Deterministic QA verdict evaluator; no GitHub API or network access.
// Used by pull_request_target workflow, which checks out trusted default-branch code.
import { readFileSync } from 'node:fs';

const SHA = /^[0-9a-f]{40}$/i;
const PREFIX = '<!-- offbook-qa-verdict:v1 ';
const RESULT = new Set(['PASS', 'FAIL', 'BLOCKED']);

const verdictsIn = (body) => {
  if (typeof body !== 'string') return [];
  const result = [];
  for (const line of body.split(/\r?\n/)) {
    const text = line.trim();
    if (!text.startsWith(PREFIX) || !text.endsWith(' -->')) continue;
    try {
      result.push(JSON.parse(text.slice(PREFIX.length, -4)));
    } catch {
      result.push({ result: 'BLOCKED', invalid: true });
    }
  }
  return result;
};
const output = (state, description, more = {}) => ({ state, description, ...more });

export function evaluateQA(input) {
  const {
    headSha, prAuthor, trustedActors = '', comments = [], docsOnly = false,
  } = input ?? {};

  if (!SHA.test(headSha ?? '')) return output('error', 'Missing or malformed PR head SHA');
  if (docsOnly) return output('success', 'Docs/templates only; independent QA not required');

  const trusted = new Set(
    String(trustedActors).split(/[\s,]+/).map(s => s.toLowerCase()).filter(Boolean),
  );
  if (!trusted.size) return output('failure', 'BLOCKED: configure OFFBOOK_QA_ACTORS repo variable');
  const author = String(prAuthor ?? '').toLowerCase();
  if (!Array.isArray(comments)) return output('error', 'Comments must be an array');

  const verdicts = [];
  for (const comment of comments) {
    const actor = String(comment.user?.login ?? comment.actor ?? '').toLowerCase();
    if (!trusted.has(actor) || actor === author) continue;
    for (const body of verdictsIn(comment.body)) {
      if (body.head_sha !== headSha) continue;
      if (!RESULT.has(body.result)) continue;
      verdicts.push({
        ...body,
        actor,
        time: String(comment.created_at ?? ''),
        id: Number(comment.id ?? 0),
      });
    }
  }
  verdicts.sort((a, b) => a.time.localeCompare(b.time) || a.id - b.id);
  const latest = verdicts.at(-1);
  if (!latest) return output('failure', 'BLOCKED: no trusted QA verdict for current PR SHA');

  if (latest.result !== 'PASS') {
    return output('failure', `QA ${latest.result} at ${headSha.slice(0, 12)} by ${latest.actor}`);
  }
  const checks = latest.checks;
  let evidenceValid = false;
  try {
    const url = new URL(latest.run_url);
    evidenceValid = url.protocol === 'https:' && Boolean(url.hostname);
  } catch {
    // Missing or malformed evidence URL.
  }
  if (!evidenceValid || !Array.isArray(checks) ||
      checks.length === 0 || checks.length > 100 ||
      checks.some(v => typeof v !== 'string' || v.trim().length < 3)) {
    return output('failure', 'BLOCKED: QA PASS lacks HTTPS evidence or specific checked scenarios');
  }
  return output('success', `QA PASS for ${headSha.slice(0, 12)} by ${latest.actor}`, {
    qa_actor: latest.actor,
    run_url: latest.run_url,
    checks,
  });
}

if (process.argv[1]?.endsWith('qa-verdict.mjs')) {
  try {
    const input = JSON.parse(readFileSync(0, 'utf8'));
    process.stdout.write(JSON.stringify(evaluateQA(input)) + '\n');
  } catch (error) {
    process.stderr.write(`qa-verdict: ${error.message}\n`);
    process.exitCode = 2;
  }
}
