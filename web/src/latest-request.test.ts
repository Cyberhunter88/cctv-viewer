import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createLatestRequest} from './latest-request.ts';

test('superseded and cancelled responses cannot update the current view', async () => {
  const requests = createLatestRequest();
  const first = requests.begin();
  let resolve!: (value: string) => void;
  const result = new Promise<string>(yes => {resolve = yes;});
  const applied: string[] = [];
  const completion = result.then(value => {if (first.current()) applied.push(value);});
  const second = requests.begin();
  assert.equal(first.signal.aborted, true);
  assert.equal(second.current(), true);
  resolve('late response'); await completion;
  assert.deepEqual(applied, []);
  requests.cancel();
  assert.equal(second.signal.aborted, true);
  assert.equal(second.current(), false);
  assert.equal(requests.begin().current(), true);
});
