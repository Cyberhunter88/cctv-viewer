export interface Recording {id:string;cameraId:string;view?:string;start:number;duration:number;codec:string;container?:string;kind?:'video'|'image'}
// Unclassified dual recordings remain available for diagnostics, but cannot play
// in either tile and must not fill gaps or become navigation targets.
export function playbackRecordings(records:Recording[],cameras:{id:string;viewMode?:string}[]):Recording[]{
 const modes=new Map(cameras.map(c=>[c.id,c.viewMode]));
 return records.filter(r=>r.kind!=='image'&&modes.has(r.cameraId)&&(modes.get(r.cameraId)!=='dual'||r.view==='wide'||r.view==='zoom'));
}
// Hidden zoom clips must not become timeline or navigation targets.
export function displayedRecordings(records:Recording[],cameras:{id:string;viewMode?:string;supportsZoom?:boolean}[],openZoom:ReadonlySet<string>):Recording[]{
 const modes=new Map(cameras.map(c=>[c.id,c]));
 return records.filter(r=>{const c=modes.get(r.cameraId);return c&&(c.viewMode!=='dual'||r.view==='wide'||r.view==='zoom'&&c.supportsZoom!==false&&openZoom.has(c.id))});
}
export function firstRecordingStart(records:Recording[],fallback:number):number{
 let start=Infinity;for(const r of records)start=Math.min(start,r.start);return start===Infinity?fallback:Math.max(fallback,start);
}
type CameraIndex={records:Recording[];maxEnd:number[]};
const indexes=new WeakMap<Recording[],Map<string,CameraIndex>>();
// Day arrays are immutable after loading. Cache sorted intervals once per response.
function index(records:Recording[]):Map<string,CameraIndex>{
 let cached=indexes.get(records);if(cached)return cached;cached=new Map();
 for(const r of records){if(r.kind==='image')continue;for(const key of [r.cameraId,...(r.view?[r.cameraId+"\0"+r.view]:[])]){let camera=cached.get(key);if(!camera){camera={records:[],maxEnd:[]};cached.set(key,camera)}camera.records.push(r)}}
 for(const camera of cached.values()){camera.records.sort((a,b)=>a.start-b.start||b.id.localeCompare(a.id));let end=-Infinity;for(const r of camera.records){end=Math.max(end,r.start+r.duration);camera.maxEnd.push(end)}}indexes.set(records,cached);return cached;
}
function upperBound(records:Recording[],time:number):number{let lo=0,hi=records.length;while(lo<hi){const mid=(lo+hi)>>>1;if(records[mid].start<=time)lo=mid+1;else hi=mid}return lo}
export function recordingAt(records:Recording[],camera:string,time:number,view?:string):Recording|undefined {
 const data=index(records).get(view?camera+"\0"+view:camera);if(!data)return;for(let i=upperBound(data.records,time)-1;i>=0&&data.maxEnd[i]>time;i--){const r=data.records[i];if(r.start+r.duration>time)return r}
}
export function nextRecording(records:Recording[],cameras:string[],time:number):number|undefined {
 let next=Infinity;const data=index(records);for(const id of cameras){const sorted=data.get(id)?.records;if(sorted){const r=sorted[upperBound(sorted,time)];if(r)next=Math.min(next,r.start)}}return next===Infinity?undefined:next;
}
export interface Gap{start:number;end:number}
// Uncovered intervals between two recordings of one camera, clipped to the window.
// Time before the first and after the last recording is not a gap: the camera may simply not have run.
export function gaps(records:Recording[],camera:string,from:number,to:number,minSeconds=60):Gap[]{
 const data=index(records).get(camera);if(!data)return[];
 const out:Gap[]=[];let covered=-Infinity;
 for(const r of data.records){
  if(covered!==-Infinity&&r.start-covered>=minSeconds){const start=Math.max(covered,from),end=Math.min(r.start,to);if(end>start)out.push({start,end})}
  covered=Math.max(covered,r.start+r.duration);
 }
 return out;
}
export function latestStartBetween(records:Recording[],camera:string,from:number,to:number):Recording|undefined{const data=index(records).get(camera);if(!data)return;const r=data.records[upperBound(data.records,to)-1];return r&&r.start>from?r:undefined}
export function clamp(value:number,min:number,max:number):number{return Math.max(min,Math.min(max,value))}
const dateFormatter=new Intl.DateTimeFormat('sv-SE',{timeZone:'Europe/Berlin',year:'numeric',month:'2-digit',day:'2-digit'});
const clockFormatter=new Intl.DateTimeFormat('de-DE',{timeZone:'Europe/Berlin',hour:'2-digit',minute:'2-digit',second:'2-digit'});
export function berlinDate(date:Date):string{return dateFormatter.format(date)}
export function clock(time:number):string{return clockFormatter.format(new Date(time*1000))}
/** Index of the camera row at a vertical timeline position, or -1 outside the recording bars. Matches the layout drawn by the timeline view. */
export function timelineRowAt(y:number,count:number):number{const index=Math.floor((y-39)/34);return index>=0&&index<count&&y-39-index*34<23?index:-1}
