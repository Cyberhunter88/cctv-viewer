import test from 'node:test';
import assert from 'node:assert/strict';
import {formatBytes,dailyAverage} from './storage.ts';

test('byte sizes use binary units with one decimal place',()=>{
 assert.equal(formatBytes(0),'0 B');
 assert.equal(formatBytes(-5),'0 B');
 assert.equal(formatBytes(Number.NaN),'0 B');
 assert.equal(formatBytes(1023),'1.023 B');
 assert.equal(formatBytes(1024),'1 KiB');
 assert.equal(formatBytes(1536),'1,5 KiB');
 assert.equal(formatBytes(5*1024**3),'5 GiB');
 assert.equal(formatBytes(3.25*1024**4),'3,3 TiB');
 assert.equal(formatBytes(4096*1024**4),'4.096 TiB');
});

test('daily average ignores days without recordings',()=>{
 assert.equal(dailyAverage([]),0);
 assert.equal(dailyAverage([{date:'2026-01-01',videos:0,bytes:0}]),0);
 assert.equal(dailyAverage([{date:'2026-01-01',videos:2,bytes:300},{date:'2026-01-02',videos:0,bytes:0},{date:'2026-01-03',videos:1,bytes:100}]),200);
});
