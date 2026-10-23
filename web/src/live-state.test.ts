import test from 'node:test';
import assert from 'node:assert/strict';
import {liveSelection,liveBufferTarget} from './live-state.ts';
import {validView} from './view-state.ts';
test('live selection preserves empty choices, removes deleted cameras and adds new order entries',()=>{
 assert.deepEqual([...liveSelection({},['a','b']).selected],['a','b']);
 assert.deepEqual([...liveSelection({liveCameras:[]},['a','b']).selected],[]);
 const value=liveSelection({liveCameras:['gone','b'],liveOrder:['gone','b','b']},['a','b','c']);
 assert.deepEqual(value.order,['b','a','c']);assert.deepEqual([...value.selected],['b']);
});
test('live preferences remain independent from recordings and validate layouts',()=>{
 assert.deepEqual(validView({cameras:['a'],liveCameras:['b','gone'],liveOrder:['b','a'],liveLayout:'2x2'},['a','b']),{cameras:['a'],liveCameras:['b'],liveOrder:['b','a'],liveLayout:'2x2'});
 assert.deepEqual(validView({liveLayout:'99',liveCameras:[]},['a']),{liveCameras:[]});
 assert.equal(liveBufferTarget(.1),0);assert.equal(liveBufferTarget(10),9.75);
});
