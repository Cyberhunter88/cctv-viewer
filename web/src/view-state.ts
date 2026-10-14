import {clamp,recordingAt,latestStartBetween,type Recording} from './timeline.ts';
export const layouts=['auto','1x1','2x1','2x2','3x2','3x3','4x3','4x4','hotspot','custom'];
export interface ViewState {cameras:string[];order:string[];layout:string;quality:string;speed:string;preload:string;aspect:string;columns:number;rows:number;autoHotspot:boolean;collapsed:boolean;timelineHeight:number;hotspot?:string}
export function validView(value:unknown,ids:string[]):Partial<ViewState>{
 if(!value||typeof value!=='object')return {};
 const v=value as Partial<ViewState>,out:Partial<ViewState>={};
 for(const key of ['cameras','order'] as const)if(Array.isArray(v[key])&&v[key]!.every(id=>typeof id==='string'))out[key]=[...new Set(v[key]!.filter(id=>ids.includes(id)))];
 const legacy:Record<string,string>={'1':'1x1','4':'2x2','9':'3x3'};
 const layout=legacy[v.layout||'']||v.layout;if(layouts.includes(layout||''))out.layout=layout;
 const quality=v.quality==='compatible'?'balanced':v.quality==='auto'?'native':v.quality;
 if(['native','balanced','fast'].includes(quality||''))out.quality=quality;
 if(['0.25','0.5','1','2','4','8','16'].includes(v.speed||''))out.speed=v.speed;
 if(['metadata','auto'].includes(v.preload||''))out.preload=v.preload;
 if(['4/3','16/9','auto','fill'].includes(v.aspect||''))out.aspect=v.aspect;
 for(const key of ['columns','rows'] as const)if(Number.isInteger(v[key])&&v[key]!>=1&&v[key]!<=8)out[key]=v[key];
 for(const key of ['autoHotspot','collapsed'] as const)if(typeof v[key]==='boolean')out[key]=v[key];
 if(typeof v.hotspot==='string'&&ids.includes(v.hotspot))out.hotspot=v.hotspot;
 if(Number.isFinite(v.timelineHeight))out.timelineHeight=clamp(v.timelineHeight!,100,650);
 return out;
}
export function gridSize(layout:string,columns:number,rows:number,count:number,width:number):[number,number]{
 if(layout==='custom')return [clamp(Math.floor(columns)||2,1,8),clamp(Math.floor(rows)||2,1,8)];
 if(layout==='hotspot')return width<600?[1,3]:[3,2];
 if(layout!=='auto'){const [c,r]=layout.split('x').map(Number);if(c&&r)return [c,r]}
 const c=width<600?1:width<1000?2:Math.min(4,Math.max(1,Math.ceil(Math.sqrt(count))));return [c,Math.min(4,Math.max(1,Math.ceil(count/c)))];
}
export function hotspotCandidate(records:Recording[],ids:string[],time:number,fromExclusive?:number):string|undefined{
 let result:string|undefined,start=-Infinity;
 for(const id of ids){const r=fromExclusive===undefined?recordingAt(records,id,time):latestStartBetween(records,id,fromExclusive,time);if(r&&r.start>start){start=r.start;result=id}}
 return result;
}
export function recordingBoundary(records:Recording[],ids:string[],time:number,direction:number):number|undefined{
 const points=[...new Set(records.filter(r=>ids.includes(r.cameraId)).flatMap(r=>[r.start,r.start+r.duration]))].sort((a,b)=>a-b);
 return direction<0?points.filter(t=>t<time-.01).at(-1):points.find(t=>t>time+.01);
}
export function moveCamera(order:string[],source:string,target:string):string[]{const sourceIndex=order.indexOf(source),targetIndex=order.indexOf(target);if(sourceIndex<0||targetIndex<0||source===target)return [...order];const result=order.filter(id=>id!==source);result.splice(targetIndex,0,source);return result}
