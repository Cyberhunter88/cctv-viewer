export interface LivePreferences {liveCameras?:string[];liveOrder?:string[];liveLayout?:string}
export function liveSelection(value:LivePreferences,ids:string[]){
 const order=[...new Set([...(value.liveOrder||[]),...ids])].filter(id=>ids.includes(id));
 const selected=value.liveCameras===undefined?new Set(ids):new Set(value.liveCameras.filter(id=>ids.includes(id)));
 return {order,selected};
}
export function liveBufferTarget(end:number){return Math.max(0,end-.25)}
// Small lag is caught up by playing slightly faster; only a large lag jumps.
export function liveCatchUp(lag:number,rate:number){
 if(lag>2.5)return {rate:1,seek:true};
 if(lag>1.5)return {rate:1.25,seek:false};
 if(lag>.8)return {rate:1.12,seek:false};
 if(lag>.3)return {rate:1.05,seek:false};
 if(lag>.15)return {rate:rate>1?rate:1,seek:false};
 return {rate:1,seek:false};
}
export const liveLayouts=['auto','1x1','2x2','3x3','4x4'];
