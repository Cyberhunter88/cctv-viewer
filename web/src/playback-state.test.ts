import {test} from 'node:test';
import assert from 'node:assert/strict';
import {classifyLoadIssue, nextWritingStep, stallTimeoutMs, streamQuality, WRITING_MAX_RETRIES} from './playback-state.ts';

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

import {prefetchTarget} from './playback-state.ts';
const clip = (id: string, start: number, extra = {}) => ({id, cameraId: 'a', start, duration: 60, ...extra});
test('prefetch picks the adjacent clip only near the end', () => {
  const list = [clip('one', 0), clip('two', 60), clip('other', 60, {cameraId: 'b'}), clip('later', 400)];
  assert.equal(prefetchTarget(list, list[0], 30, 1), undefined);
  assert.equal(prefetchTarget(list, list[0], 55, 1)?.id, 'two');
  assert.equal(prefetchTarget(list, list[0], 30, 8)?.id, 'two');
  assert.equal(prefetchTarget(list, list[1], 118, 1), undefined);
  assert.equal(prefetchTarget([clip('x', 0, {view: 'wide'}), clip('y', 60, {view: 'zoom'})], clip('x', 0, {view: 'wide'}), 58, 1), undefined);
});

test('original quality converts at full resolution only when the browser cannot decode it', () => {
  const none = () => '', hevc = (type: string) => type.includes('hvc1') ? 'probably' : '';
  assert.equal(streamQuality('native', 'h264', none, new Set()), 'native');
  assert.equal(streamQuality('native', 'hevc', hevc, new Set()), 'native');
  assert.equal(streamQuality('native', 'hevc', none, new Set()), 'full');
  assert.equal(streamQuality('native', 'h264', none, new Set(['h264'])), 'full');
  assert.equal(streamQuality('balanced', 'hevc', none, new Set()), 'balanced');
  assert.equal(streamQuality('fast', 'h264', none, new Set(['h264'])), 'fast');
});
