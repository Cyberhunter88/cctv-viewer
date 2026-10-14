import {test} from 'node:test';
import assert from 'node:assert/strict';
import {recordingAt,nextRecording,clamp,berlinDate,clock,type Recording} from './timeline.ts';
const records:Recording[]=[{id:'a',cameraId:'1',start:100,duration:60,codec:'h264'},{id:'b',cameraId:'1',start:150,duration:30,codec:'h264'},{id:'c',cameraId:'2',start:200,duration:50,codec:'hevc'}];
test('intervals are half-open and overlaps use latest clip',()=>{assert.equal(recordingAt(records,'1',99),undefined);assert.equal(recordingAt(records,'1',100)?.id,'a');assert.equal(recordingAt(records,'1',150)?.id,'b');assert.equal(recordingAt(records,'1',180),undefined)});
test('gap skipping respects visible cameras',()=>{assert.equal(nextRecording(records,['1'],160),undefined);assert.equal(nextRecording(records,['2'],160),200);assert.equal(nextRecording(records,['1'],99),100)});
test('Berlin formatting does not use browser timezone',()=>{assert.equal(berlinDate(new Date('2026-10-14T23:30:00Z')),'2026-10-15');assert.equal(clock(Date.parse('2026-10-14T06:30:00Z')/1000),'08:30:00');assert.equal(clamp(-5,0,10),0)});
