import {test} from 'node:test';
import assert from 'node:assert/strict';
import {PreferenceWriter} from './preferences.ts';

test('writes are serialized, unrelated fields are omitted and edits coalesce',async()=>{
 const writes:Record<string,unknown>[]=[],gates:(()=>void)[]=[];
 const writer=new PreferenceWriter({theme:'system',layout:'auto'},async changes=>{writes.push(changes);await new Promise<void>(resolve=>gates.push(resolve))},()=>{});
 const first=writer.save({theme:'dark',layout:'auto'});
 void writer.save({theme:'dark',layout:'2x2'});
 void writer.save({theme:'dark',layout:'3x2'});
 assert.deepEqual(writes,[{theme:'dark'}]);gates.shift()!();
 await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(writes,[{theme:'dark'},{layout:'3x2'}]);gates.shift()!();await first;
 await writer.save({theme:'dark',layout:'3x2'});assert.equal(writes.length,2);
});

test('failed fields remain pending and retry keeps the newest edits',async()=>{
 let failing=true;const writes:Record<string,unknown>[]=[],statuses:unknown[]=[];
 const writer=new PreferenceWriter({theme:'system',layout:'auto'},async changes=>{writes.push(changes);if(failing)throw new Error('synthetic unavailable')},(_saving,error)=>statuses.push(error));
 await assert.rejects(writer.save({theme:'dark',layout:'auto'}));
 failing=false;await writer.save({theme:'light',layout:'2x2'});
 assert.deepEqual(writes,[{theme:'dark'},{theme:'light',layout:'2x2'}]);assert.equal(statuses.at(-1),undefined);
});
