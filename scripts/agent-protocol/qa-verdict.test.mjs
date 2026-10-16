import test from 'node:test';
import assert from 'node:assert/strict';
import { evaluateQA } from './qa-verdict.mjs';

const shaA = 'a'.repeat(40);
const shaB = 'b'.repeat(40);
const verdict = (sha, result, checks = ['refund idempotency']) =>
  `<!-- offbook-qa-verdict:v1 ${JSON.stringify({
    head_sha: sha, result, run_url: 'https://example.com/runs/123', checks,
  })} -->`;
const comment = (login, body, id = 1) => ({
  id, user: { login }, created_at: new Date(1000 + id * 1000).toISOString(), body,
});
const params = {
  headSha: shaA, prAuthor: 'dev-bot', trustedActors: 'qa-bot,qa-reviewer',
};

test('trusted independent PASS for exact SHA succeeds', () => {
  assert.equal(evaluateQA({
    ...params, comments: [comment('qa-bot', verdict(shaA, 'PASS'))],
  }).state, 'success');
});
test('no QA actor configured fails closed', () => {
  assert.equal(evaluateQA({ ...params, trustedActors: '', comments: [] }).state, 'failure');
});
test('no verdict fails closed', () => {
  assert.equal(evaluateQA({ ...params, comments: [] }).state, 'failure');
});
test('untrusted or PR-author comments cannot grant PASS', () => {
  const comments = [comment('stranger', verdict(shaA, 'PASS')),
    comment('dev-bot', verdict(shaA, 'PASS'), 2)];
  assert.equal(evaluateQA({ ...params, comments }).state, 'failure');
});
test('new head SHA makes old QA PASS stale', () => {
  assert.equal(evaluateQA({
    ...params, headSha: shaB, comments: [comment('qa-bot', verdict(shaA, 'PASS'))],
  }).state, 'failure');
});
test('later trusted FAIL supersedes earlier PASS', () => {
  assert.equal(evaluateQA({ ...params, comments: [
    comment('qa-bot', verdict(shaA, 'PASS'), 1),
    comment('qa-reviewer', verdict(shaA, 'FAIL'), 2),
  ] }).state, 'failure');
});
test('later trusted PASS supersedes earlier FAIL for identical SHA', () => {
  assert.equal(evaluateQA({ ...params, comments: [
    comment('qa-bot', verdict(shaA, 'FAIL'), 1),
    comment('qa-reviewer', verdict(shaA, 'PASS'), 2),
  ] }).state, 'success');
});
test('PASS without a real evidence URL or checks fails closed', () => {
  const missing = `<!-- offbook-qa-verdict:v1 ${JSON.stringify({
    head_sha: shaA, result: 'PASS', run_url: 'http://example.org', checks: [],
  })} -->`;
  assert.equal(evaluateQA({ ...params, comments: [comment('qa-bot', missing)] }).state, 'failure');
});
test('unparseable verdict and malformed SHA cannot pass', () => {
  assert.equal(evaluateQA({ ...params, comments: [comment('qa-bot', '<!-- offbook-qa-verdict:v1 {bad} -->')] }).state, 'failure');
  assert.equal(evaluateQA({ ...params, headSha: 'bad', docsOnly: true }).state, 'error');
});
test('docs-only changes may skip independent QA', () => {
  assert.equal(evaluateQA({ ...params, trustedActors: '', docsOnly: true }).state, 'success');
});
test('matches verdicts only at standalone structured comment lines', () => {
  assert.equal(evaluateQA({ ...params, comments: [comment('qa-bot', 'backtick '+verdict(shaA, 'PASS'))] }).state, 'failure');
});
