import {clamp,recordingAt,latestStartBetween,type Recording} from './timeline.ts';
export const layouts=['auto','1x1','2x1','2x2','3x2','3x3','4x3','4x4','hotspot','custom'];
export interface ViewState {cameras:string[];order:string[];layout:string;quality:string;speed:string;preload:string;aspect:string;columns:number;rows:number;autoHotspot:boolean;collapsed:boolean;timelineHeight:number;hotspot?:string;density:'comfortable'|'compact';showNames:boolean;showTimestamps:boolean;showCodec:boolean;showOverview:boolean;showThumbnails:boolean;shortcuts:boolean;seekStep:number}
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
 for(const key of ['autoHotspot','collapsed','showNames','showTimestamps','showCodec','showOverview','showThumbnails','shortcuts'] as const)if(typeof v[key]==='boolean')out[key]=v[key];
 if(typeof v.hotspot==='string'&&ids.includes(v.hotspot))out.hotspot=v.hotspot;
 if(v.density==='comfortable'||v.density==='compact')out.density=v.density;
 if([5,10,30].includes(v.seekStep!))out.seekStep=v.seekStep;
 if(Number.isFinite(v.timelineHeight))out.timelineHeight=clamp(v.timelineHeight!,100,650);
 return out;
}
export function gridSize(layout:string,columns:number,rows:number,count:number,width:number,dual=false):[number,number]{
 const narrow=width<600,minimum=dual?488:260;
 const maxColumns=Math.max(1,Math.floor((width+12)/(minimum+12)));
 let c:number,r:number;
 if(layout==='custom'){c=clamp(Math.floor(columns)||2,1,8);r=clamp(Math.floor(rows)||2,1,8)}
 else if(layout==='hotspot'){c=3;r=2}
 else if(layout!=='auto'&&/^\d+x\d+$/.test(layout)){[c,r]=layout.split('x').map(Number)}
 else{c=Math.min(4,Math.max(1,Math.ceil(Math.sqrt(count))),maxColumns);r=Math.min(4,Math.max(1,Math.ceil(count/c)))}
 return [Math.min(c!,maxColumns),narrow?Math.min(r!,2):r!];
}
export function pageCapacity(layout:string,columns:number,rows:number):number{
 return Math.max(1,columns*rows-(layout==='hotspot'&&columns>1&&rows>1?3:0));
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

export function defaultView():ViewState{return {cameras:[],order:[],layout:'auto',quality:'native',speed:'1',preload:'metadata',aspect:'auto',columns:2,rows:2,autoHotspot:false,collapsed:true,timelineHeight:240,density:'comfortable',showNames:true,showTimestamps:true,showCodec:true,showOverview:true,showThumbnails:true,shortcuts:true,seekStep:5}}
