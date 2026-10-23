export interface LivePreferences {liveCameras?:string[];liveOrder?:string[];liveLayout?:string}
export function liveSelection(value:LivePreferences,ids:string[]){
 const order=[...new Set([...(value.liveOrder||[]),...ids])].filter(id=>ids.includes(id));
 const selected=value.liveCameras===undefined?new Set(ids):new Set(value.liveCameras.filter(id=>ids.includes(id)));
 return {order,selected};
}
export function liveBufferTarget(end:number){return Math.max(0,end-.25)}
export const liveLayouts=['auto','1x1','2x2','3x3','4x4'];
