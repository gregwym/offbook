import test from 'node:test';
import assert from 'node:assert/strict';
import { checkMaturity, JOURNEYS, DIMENSIONS } from './maturity-gate.mjs';
const main = 'a'.repeat(40);
const goodScore = () => Object.fromEntries(DIMENSIONS.map(key => [key, 3]));
const make = () => ({
  head_sha: main, result: 'MATURE',
  qa_run_url: 'https://github.com/gregwym/offbook/actions/runs/12345',
  zero_open_blockers: true, soak_verified: true, backup_restore_verified: true,
  journeys: JOURNEYS.map(id => ({
    id, scores: goodScore(),
    evidence: ['https://github.com/gregwym/offbook/issues/423#issuecomment-12345'],
  })),
});
const wrap = report => ({
  mainSha: main,
  trustedActors: 'gregwym',
  comments: [{ id: 9, user: {login: 'gregwym'}, body: '<!-- offbook-maturity:v1 ' + JSON.stringify(report) + ' -->' }],
});
test('complete evidence at exact current SHA passes', () => assert.equal(checkMaturity(wrap(make())).ok, true));
test('no evidence fails', () => assert.equal(checkMaturity({ mainSha: main, comments: [] }).ok, false));
test('stale SHA fails', () => { const r = make(); r.head_sha = 'b'.repeat(40); assert.equal(checkMaturity(wrap(r)).ok, false); });
test('missing core journey fails', () => { const r = make(); r.journeys.pop(); assert.equal(checkMaturity(wrap(r)).ok, false); });
test('missing visual evidence fails', () => { const r = make(); r.journeys[0].evidence = []; assert.equal(checkMaturity(wrap(r)).ok, false); });
test('rough but usable UX cannot graduate', () => { const r = make(); r.journeys[2].scores.friction = 2; assert.equal(checkMaturity(wrap(r)).ok, false); });
test('unreviewed dimension cannot graduate', () => { const r = make(); delete r.journeys[0].scores.reference; assert.equal(checkMaturity(wrap(r)).ok, false); });
test('unverified soak or backup prevents premature DONE', () => {
  for(const field of ['zero_open_blockers', 'soak_verified', 'backup_restore_verified']) {
    const r=make(); r[field]=false; assert.equal(checkMaturity(wrap(r)).ok,false);
  }
});
test('later negative report overrides earlier PASS', () => {
  const r=wrap(make()); r.comments.push({id: 10, user: { login: 'gregwym' }, body:'<!-- offbook-maturity:v1 {"result":"NOT MATURE"} -->'});
  assert.equal(checkMaturity(r).ok,false);
});
test('example.com placeholder is not valid evidence', () => {
  const r=make(); r.qa_run_url='https://example.com/fake'; assert.equal(checkMaturity(wrap(r)).ok,false);
});
test('non-MATURE result cannot pass', () => {const r=make();r.result='BLOCKED';assert.equal(checkMaturity(wrap(r)).ok,false)});

test('public issue commenter cannot forge final maturity', () => {
  const r = wrap(make()); r.comments[0].user.login = 'untrusted-public-commenter';
  assert.equal(checkMaturity(r).ok, false);
});
