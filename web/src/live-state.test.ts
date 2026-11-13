import test from 'node:test';
import assert from 'node:assert/strict';
import {liveSelection,liveBufferTarget,liveCatchUp} from './live-state.ts';
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
test('live catch-up speeds up gently, keeps its rate in the dead band and only jumps for large lag',()=>{
 assert.deepEqual(liveCatchUp(.1,1.12),{rate:1,seek:false});
 assert.deepEqual(liveCatchUp(.2,1),{rate:1,seek:false});assert.deepEqual(liveCatchUp(.2,1.05),{rate:1.05,seek:false});
 assert.deepEqual(liveCatchUp(.5,1),{rate:1.05,seek:false});assert.deepEqual(liveCatchUp(1,1),{rate:1.12,seek:false});
 assert.deepEqual(liveCatchUp(2,1),{rate:1.25,seek:false});assert.deepEqual(liveCatchUp(3,1.25),{rate:1,seek:true});
});
