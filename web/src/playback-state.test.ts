import {test} from 'node:test';
import assert from 'node:assert/strict';
import {classifyLoadIssue, nextWritingStep, stallTimeoutMs, WRITING_MAX_RETRIES} from './playback-state.ts';

test('classifies HTTP statuses of failed loads', () => {
  assert.equal(classifyLoadIssue(409, 'x').kind, 'writing');
  assert.equal(classifyLoadIssue(429, 'x').kind, 'busy');
  assert.equal(classifyLoadIssue(503, 'x').kind, 'busy');
  assert.equal(classifyLoadIssue(504, 'x').kind, 'timeout');
  assert.deepEqual(classifyLoadIssue(500, 'Fehler'), {kind: 'generic', message: 'Fehler'});
  assert.equal(classifyLoadIssue(undefined, 'Fehler').message, 'Fehler');
});

test('waits for a growing file until the retry limit', () => {
  const first = nextWritingStep(0);
  assert.equal(first.retry, true);
  assert.equal(first.attempt, 1);
  assert.match(first.message, /1\/24/);
  assert.equal(nextWritingStep(WRITING_MAX_RETRIES - 1).retry, true);
  const last = nextWritingStep(WRITING_MAX_RETRIES);
  assert.equal(last.retry, false);
  assert.match(last.message, /nicht fertig/);
});

test('stall tolerance grows with speed and stays bounded', () => {
  assert.equal(stallTimeoutMs(1), 10000);
  assert.ok(stallTimeoutMs(8) > stallTimeoutMs(2));
  assert.equal(stallTimeoutMs(1000), 30000);
});
