import {test} from 'node:test';
import assert from 'node:assert/strict';
import {berlinEpoch,berlinLocal} from './management.ts';
import {recordingAt,nextRecording,type Recording} from './timeline.ts';
test('search inputs use Berlin even when the browser uses another timezone',()=>{assert.equal(berlinEpoch('2026-10-14T06:12:17'),Date.parse('2026-10-14T04:12:17Z')/1000);assert.equal(berlinLocal(Date.parse('2026-10-14T22:01:00Z')/1000),'2026-10-15T00:01:00');assert.equal(berlinEpoch('2026-01-01T06:00:00'),Date.parse('2026-01-01T05:00:00Z')/1000)});
test('ambiguous and nonexistent search times fail instead of silently shifting',()=>{assert.throws(()=>berlinEpoch('2026-10-25T02:30:00'));assert.throws(()=>berlinEpoch('2026-03-29T02:30:00'));assert.throws(()=>berlinEpoch('invalid'))});
test('image events neither play nor fill or skip video gaps',()=>{const records:Recording[]=[{id:'image',cameraId:'cam',start:100,duration:0,codec:'jpeg',kind:'image'},{id:'video',cameraId:'cam',start:200,duration:10,codec:'h264',kind:'video'}];assert.equal(recordingAt(records,'cam',100),undefined);assert.equal(nextRecording(records,['cam'],99),200)});
