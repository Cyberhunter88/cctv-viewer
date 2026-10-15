import {test} from 'node:test';
import assert from 'node:assert/strict';
import {validView,gridSize,pageCapacity,hotspotCandidate,recordingBoundary,moveCamera} from './view-state.ts';
const records=[{id:'a',cameraId:'a',start:100,duration:50,codec:'h264'},{id:'b',cameraId:'b',start:120,duration:20,codec:'h264'}];
test('migrates old view preferences and sanitizes unknown cameras',()=>{assert.deepEqual(validView({cameras:['gone','a','a'],layout:'9',quality:'compatible',speed:'4'},['a']),{cameras:['a'],layout:'3x3',quality:'balanced',speed:'4'});assert.deepEqual(validView({cameras:[],layout:'100',quality:'bad',speed:'0',rows:99},['a']),{cameras:[]});assert.deepEqual(validView(null,[]),{})});
test('fixed, custom and narrow automatic grids preserve capacity',()=>{assert.deepEqual(gridSize('3x2',2,2,20,1200),[3,2]);assert.deepEqual(gridSize('custom',8,8,100,1200),[4,8]);assert.deepEqual(gridSize('auto',2,2,8,390),[1,2]);assert.deepEqual(gridSize('hotspot',2,2,6,1200),[3,2])});
test('hotspot chooses latest active start and camera order resolves ties',()=>{assert.equal(hotspotCandidate(records,['a','b'],119),'a');assert.equal(hotspotCandidate(records,['a','b'],120),'b');assert.equal(hotspotCandidate(records,['a','b'],150),undefined);assert.equal(hotspotCandidate([...records,{...records[1],id:'c',cameraId:'c'}],['c','b'],125),'c')});
test('boundary navigation includes ends, respects selection and avoids staying put',()=>{assert.equal(recordingBoundary(records,['a','b'],120,1),140);assert.equal(recordingBoundary(records,['a','b'],120,-1),100);assert.equal(recordingBoundary(records,['a'],151,-1),150);assert.equal(recordingBoundary(records,['b'],140,1),undefined)});
test('playing hotspot follows new starts, including short clips crossed between frames',()=>{assert.equal(hotspotCandidate(records,['a','b'],145,110),'b');assert.equal(hotspotCandidate(records,['a','b'],146,145),undefined)});
test('camera dragging moves forward after target and backward before target',()=>{assert.deepEqual(moveCamera(['a','b','c'],'a','c'),['b','c','a']);assert.deepEqual(moveCamera(['a','b','c'],'c','a'),['c','a','b']);assert.deepEqual(moveCamera(['a','b'],'gone','b'),['a','b'])});

test('six cameras retain readable capacity on phones and dual grids',()=>{
 assert.deepEqual(gridSize('auto',2,2,6,1732,true),[3,2]);
 assert.deepEqual(gridSize('auto',2,2,6,1304,true),[2,3]);
 assert.deepEqual(gridSize('3x2',2,2,6,320,true),[1,2]);
 assert.deepEqual(gridSize('custom',8,8,6,320),[1,2]);
 assert.equal(pageCapacity('hotspot',3,2),3);
 assert.equal(pageCapacity('hotspot',2,2),1);
 assert.equal(pageCapacity('hotspot',1,2),2);
 assert.deepEqual(validView({quality:'fast',aspect:'4/3'},[]),{quality:'fast',aspect:'4/3'});
});
