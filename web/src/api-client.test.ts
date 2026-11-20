import {test} from 'node:test';
import assert from 'node:assert/strict';
import {errorMessage} from './api-client.ts';

test('server errors keep their message and fall back to the status code', () => {
  assert.equal(errorMessage({error: 'Kamera fehlt'}, 404), 'Kamera fehlt');
  assert.equal(errorMessage({}, 503), 'Anfrage fehlgeschlagen (503)');
});

test('recording gaps are listed below the message, one range per line', () => {
  const message = errorMessage({error: 'Lücken', gaps: [[0, 60], [3600, 3661]]}, 409);
  const lines = message.split('\n');
  assert.equal(lines.length, 3);
  assert.equal(lines[0], 'Lücken');
  assert.match(lines[1], / – /);
});
