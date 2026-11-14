import {test} from 'node:test';
import assert from 'node:assert/strict';
import {searchRange} from './search-range.ts';

// 2026-11-14 20:30:15 in Berlin (UTC+1).
const now=Date.UTC(2026,10,14,19,30,15)/1000;
test('quick search ranges use Berlin wall-clock values',()=>{
 assert.deepEqual(searchRange('today',now),{from:'2026-11-14T00:00:00',to:''});
 assert.deepEqual(searchRange('24h',now),{from:'2026-11-13T20:30:15',to:''});
 assert.deepEqual(searchRange('7d',now),{from:'2026-11-07T20:30:15',to:''});
 assert.deepEqual(searchRange('all',now),{from:'',to:''});
});
test('ranges across the daylight saving change keep the elapsed time',()=>{
 // 2026-03-29 12:00 CEST is 24 hours after 2026-03-28 11:00 CET.
 assert.equal(searchRange('24h',Date.UTC(2026,2,29,10,0,0)/1000).from,'2026-03-28T11:00:00');
});
