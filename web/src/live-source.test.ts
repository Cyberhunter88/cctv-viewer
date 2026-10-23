import {test} from 'node:test';
import assert from 'node:assert/strict';
import {importLiveURL} from './live-source.ts';
test('Reolink URL import removes credentials and preserves stream parameters',()=>{
 const result=importLiveURL('https://camera.example.org/flv?port=1935&app=bcs&stream=channel0_sub.bcs&user=synthetic&password=a%26b%23c');
 assert.equal(result.username,'synthetic');assert.equal(result.password,'a&b#c');
 const url=new URL(result.url);assert.equal(url.searchParams.has('user'),false);assert.equal(url.searchParams.has('password'),false);assert.equal(url.searchParams.get('stream'),'channel0_sub.bcs');
});
test('empty imported password does not become an instruction to clear a password',()=>{
 const address=new URL('https://camera.example.org/flv');address.searchParams.set('password','');
 assert.equal(importLiveURL(address.href).password,'');
 assert.equal(importLiveURL('rtsp://camera.example.org/sub').url,'rtsp://camera.example.org/sub');
});
