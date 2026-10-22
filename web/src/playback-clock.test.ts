import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createPlaybackClock} from './playback-clock.ts';

function fixture() {
  let time = 0, sequence = 0, seconds = 0, syncs = 0, renders = 0;
  const frames = new Map<number, FrameRequestCallback>();
  const clock = createPlaybackClock({
    now: () => time,
    requestFrame: callback => {frames.set(++sequence, callback); return sequence;},
    cancelFrame: id => {frames.delete(id);},
    advance: delta => {seconds += delta;},
    synchronize: () => {syncs++;},
    render: () => {renders++;},
  });
  function tick(at: number) {
    time = at;
    const callbacks = [...frames.values()]; frames.clear();
    callbacks.forEach(callback => callback(time));
  }
  return {clock, tick, frames, counts: () => ({seconds, syncs, renders})};
}

test('one frame loop advances continuously with bounded synchronization and rendering', () => {
  const f = fixture();
  f.clock.start(); f.clock.start();
  assert.equal(f.frames.size, 1);
  for (let time = 10; time <= 1000; time += 10) f.tick(time);
  assert.ok(Math.abs(f.counts().seconds - 1) < 1e-9);
  assert.equal(f.counts().syncs, 20);
  assert.equal(f.counts().renders, 10);
  f.clock.stop();
  assert.equal(f.frames.size, 0);
  f.tick(5000);
  assert.equal(f.counts().renders, 10);
  f.clock.start(); f.tick(5050);
  assert.ok(Math.abs(f.counts().seconds - 1.05) < 1e-9);
});

test('reset discards elapsed time before a seek; stop inside advance schedules no new frame', () => {
  const f = fixture();
  f.clock.start(); f.tick(40); f.clock.reset(); f.tick(60);
  assert.equal(f.counts().seconds, .06);
  assert.equal(f.counts().syncs, 0);
  let renders = 0;
  let callback!: FrameRequestCallback;
  const clock = createPlaybackClock({
    now: () => 0, requestFrame: next => {callback = next; return 1;}, cancelFrame: () => {},
    advance: () => clock.stop(), synchronize: () => {throw new Error('stopped');}, render: () => {renders++;},
  });
  clock.start(); callback(100);
  assert.equal(renders, 0);
});
