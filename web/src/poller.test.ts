import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createPoller} from './poller.ts';

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => {resolve = yes; reject = no;});
  return {promise, resolve, reject};
}
const settled = () => new Promise<void>(resolve => setImmediate(resolve));

function fixture() {
  const pending: {signal: AbortSignal; result: ReturnType<typeof deferred>}[] = [];
  const timers = new Map<number, {callback: () => void; delay: number}>();
  const errors: unknown[] = [];
  let sequence = 0;
  const poller = createPoller({
    request: signal => {const result = deferred(); pending.push({signal, result}); return result.promise;},
    onError: error => errors.push(error),
    schedule: (callback, delay) => {const id = ++sequence; timers.set(id, {callback, delay}); return id as unknown as ReturnType<typeof setTimeout>;},
    cancel: id => {timers.delete(id as unknown as number);},
  });
  const next = () => {
    assert.equal(timers.size, 1);
    const [id, timer] = [...timers][0];
    timers.delete(id);
    timer.callback();
    return timer.delay;
  };
  return {poller, pending, timers, errors, next};
}

test('polls never overlap and restart waits for an aborted predecessor to settle', async () => {
  const f = fixture();
  f.poller.start(); f.poller.start();
  assert.equal(f.pending.length, 1);
  assert.equal(f.timers.size, 0);
  f.poller.stop();
  assert.equal(f.pending[0].signal.aborted, true);
  f.poller.start();
  assert.equal(f.pending.length, 1);
  f.pending[0].result.resolve(); await settled();
  assert.equal(f.next(), 0);
  assert.equal(f.pending.length, 2);
  f.pending[1].result.resolve(); await settled();
  assert.equal(f.next(), 4000);
  f.poller.stop();
  f.pending[2].result.reject(new Error('cancelled')); await settled();
  assert.equal(f.timers.size, 0);
  assert.deepEqual(f.errors, []);
});

test('errors back off to 30 seconds, deduplicate messages and reset after success', async () => {
  const f = fixture();
  f.poller.start();
  for (const [i, delay] of [8000, 16000, 30000, 30000].entries()) {
    f.pending[i].result.reject(new Error('offline')); await settled();
    assert.equal(f.next(), delay);
  }
  assert.equal(f.errors.length, 1);
  f.pending[4].result.reject(new Error('different')); await settled();
  assert.equal(f.errors.length, 2);
  assert.equal(f.next(), 30000);
  f.pending[5].result.resolve(); await settled();
  assert.equal(f.next(), 4000);
  f.pending[6].result.reject(new Error('offline')); await settled();
  assert.equal(f.errors.length, 3);
  assert.equal([...f.timers.values()][0].delay, 8000);
  f.poller.stop();
  assert.equal(f.timers.size, 0);
});
