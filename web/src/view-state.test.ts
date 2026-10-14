import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readTimeLink,validView,selectionEdge,shouldFallback} from './view-state.ts';
test('saved views reject invalid settings and remove missing cameras',()=>{
 assert.deepEqual(validView({cameras:['gone','a'],layout:'9',quality:'compatible',speed:'4'},['a']),{cameras:['a'],layout:'9',quality:'compatible',speed:'4'});
 assert.deepEqual(validView({cameras:[],layout:'100',quality:'bad',speed:'0'},['a']),{cameras:[]});
 assert.deepEqual(validView(null,[]),{});
});
test('valid time link overrides saved cameras; invalid dates and cross-day times are ignored',()=>{
 const time=Date.parse('2026-10-14T06:30:00Z')/1000;
 assert.deepEqual(readTimeLink('?date=2026-10-14&time='+time+'&cameras=a,gone', ['a']),{date:'2026-10-14',time,cameras:['a']});
 for(const date of ['2026-02-30','2026-03-99','bad','1969-12-31'])assert.equal(readTimeLink('?date='+date,[]).date,undefined);
 assert.equal(readTimeLink('?date=2026-10-13&time='+time,[]).time,undefined);
 assert.deepEqual(readTimeLink('?time=NaN&cameras=gone',['a']),{});
 const stored=validView({cameras:['b'],layout:'1'},['a','b']);assert.deepEqual({...stored,...readTimeLink('?cameras=a',['a','b'])},{cameras:['a'],layout:'1'});
});
test('range handles preserve order and 30-minute maximum at both ends',()=>{
 assert.deepEqual(selectionEdge('start',0,100,2000,0,86400),[200,2000]);
 assert.deepEqual(selectionEdge('end',9999,100,200,0,86400),[100,1900]);
 assert.deepEqual(selectionEdge('start',500,100,200,0,86400),[199,200]);
 assert.deepEqual(selectionEdge('end',100,100,200,0,86400),[100,101]);
 assert.deepEqual(selectionEdge('end',9999,100,200,0,400),[100,400]);
});
test('only an automatic original decoder/format failure permits one fallback',()=>{
 for(const code of [3,4])assert.equal(shouldFallback('auto',code,false,false),true);
 for(const code of [1,2])assert.equal(shouldFallback('auto',code,false,false),false);
 assert.equal(shouldFallback('native',3,false,false),false);
 assert.equal(shouldFallback('compatible',3,false,false),false);
 assert.equal(shouldFallback('auto',3,true,false),false);
 assert.equal(shouldFallback('auto',3,false,true),false);
});
