import {test} from 'node:test';
import assert from 'node:assert/strict';
import {recordingAt,nextRecording,playbackRecordings,firstRecordingStart,clamp,berlinDate,clock,type Recording} from './timeline.ts';
const records:Recording[]=[{id:'a',cameraId:'1',start:100,duration:60,codec:'h264'},{id:'b',cameraId:'1',start:150,duration:30,codec:'h264'},{id:'c',cameraId:'2',start:200,duration:50,codec:'hevc'}];
test('intervals are half-open and overlaps use latest clip',()=>{assert.equal(recordingAt(records,'1',99),undefined);assert.equal(recordingAt(records,'1',100)?.id,'a');assert.equal(recordingAt(records,'1',150)?.id,'b');assert.equal(recordingAt(records,'1',180),undefined)});
test('gap skipping respects visible cameras',()=>{assert.equal(nextRecording(records,['1'],160),undefined);assert.equal(nextRecording(records,['2'],160),200);assert.equal(nextRecording(records,['1'],99),100)});
test('Berlin formatting does not use browser timezone',()=>{assert.equal(berlinDate(new Date('2026-10-14T23:30:00Z')),'2026-10-15');assert.equal(clock(Date.parse('2026-10-14T06:30:00Z')/1000),'08:30:00');assert.equal(clamp(-5,0,10),0)});
test('nested overlaps remain correct with interval lookup cache',()=>{const items:Recording[]=[{id:'long',cameraId:'a',start:0,duration:100,codec:'h264'},{id:'short',cameraId:'a',start:50,duration:1,codec:'h264'}];assert.equal(recordingAt(items,'a',50.5)?.id,'short');assert.equal(recordingAt(items,'a',52)?.id,'long');assert.equal(recordingAt(items,'a',100),undefined)});

test('dual views use independent overlapping intervals and missing zoom stays empty',()=>{
 const items:Recording[]=[{id:'wide',cameraId:'a',view:'wide',start:100,duration:60,codec:'h264'},{id:'zoom',cameraId:'a',view:'zoom',start:102,duration:20,codec:'h264'},{id:'unclear',cameraId:'a',view:'unknown',start:103,duration:60,codec:'h264'}];
 assert.equal(recordingAt(items,'a',101,'wide')?.id,'wide');assert.equal(recordingAt(items,'a',101,'zoom'),undefined);
 assert.equal(recordingAt(items,'a',104,'wide')?.id,'wide');assert.equal(recordingAt(items,'a',104,'zoom')?.id,'zoom');
 assert.equal(recordingAt(items,'a',122,'zoom'),undefined);assert.equal(recordingAt(items,'a',122,'wide')?.id,'wide');
});

test('dual navigation skips unclassified clips and keeps a zoom-only interval playable',()=>{
 const items:Recording[]=[
  {id:'wide',cameraId:'pair',view:'wide',start:100,duration:5,codec:'h264'},
  {id:'unknown',cameraId:'pair',view:'unknown',start:105,duration:100,codec:'h264'},
  {id:'zoom',cameraId:'pair',view:'zoom',start:120,duration:10,codec:'h264'},
  {id:'single',cameraId:'single',start:140,duration:10,codec:'h264'},
 ];
 const playable=playbackRecordings(items,[{id:'pair',viewMode:'dual'},{id:'single'}]);
 assert.deepEqual(playable.map(r=>r.id),['wide','zoom','single']);
 assert.equal(recordingAt(playable,'pair',110),undefined);
 assert.equal(nextRecording(playable,['pair'],105),120);
 assert.equal(recordingAt(playable,'pair',121,'wide'),undefined);
 assert.equal(recordingAt(playable,'pair',121,'zoom')?.id,'zoom');
 assert.equal(recordingAt(items,'pair',110,'unknown')?.id,'unknown');
 assert.equal(firstRecordingStart(playable,0),100);
 assert.deepEqual(playbackRecordings(items,[{id:'pair',viewMode:'single'}]).map(r=>r.id),['wide','unknown','zoom']);
});

test('large paired archives select their first start without argument expansion',()=>{
 const items=Array.from({length:200000},(_,i)=>({id:String(i),cameraId:'pair',view:i%2?'zoom':'wide',start:300000-i,duration:1,codec:'h264'}));
 assert.equal(firstRecordingStart(items,0),100001);
 assert.equal(firstRecordingStart(items,200000),200000);
 assert.equal(firstRecordingStart([],42),42);
});


test('Reolink Testkamera-B perspectives share the clock despite unequal and changing clip boundaries',()=>{
 const items:Recording[]=[
  {id:'Reolink Testkamera-B_00_20261015061313.mp4',cameraId:'camera-b',view:'wide',start:100,duration:10,codec:'h264'},
  {id:'Reolink Testkamera-B_01_20261015061312.mp4',cameraId:'camera-b',view:'zoom',start:99,duration:8,codec:'h264'},
  {id:'next-wide',cameraId:'camera-b',view:'wide',start:110,duration:10,codec:'h264'},
  {id:'next-zoom',cameraId:'camera-b',view:'zoom',start:107,duration:9,codec:'h264'},
 ];
 assert.equal(recordingAt(items,'camera-b',99,'wide'),undefined);
 const wide=recordingAt(items,'camera-b',100,'wide')!,zoom=recordingAt(items,'camera-b',100,'zoom')!;
 assert.equal(100-wide.start,0);assert.equal(100-zoom.start,1);
 assert.equal(recordingAt(items,'camera-b',108,'wide')?.id,wide.id);
 assert.equal(recordingAt(items,'camera-b',108,'zoom')?.id,'next-zoom');
 assert.equal(recordingAt(items,'camera-b',110,'wide')?.id,'next-wide');
 assert.equal(recordingAt(items,'camera-b',110,'zoom')?.id,'next-zoom');
 assert.equal(recordingAt(items,'camera-b',117,'zoom'),undefined);
 assert.equal(recordingAt(items,'camera-b',117,'wide')?.id,'next-wide');
});
