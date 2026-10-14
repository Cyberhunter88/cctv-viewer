export interface Recording {id:string;cameraId:string;start:number;duration:number;codec:string;container?:string}
export function recordingAt(records:Recording[],camera:string,time:number):Recording|undefined {
 // Prefer the most recently started clip when intervals overlap.
 return records.filter(r=>r.cameraId===camera&&r.start<=time&&r.start+r.duration>time).sort((a,b)=>b.start-a.start||a.id.localeCompare(b.id))[0];
}
export function nextRecording(records:Recording[],cameras:string[],time:number):number|undefined {
 return records.filter(r=>cameras.includes(r.cameraId)&&r.start>time).sort((a,b)=>a.start-b.start)[0]?.start;
}
export function clamp(value:number,min:number,max:number):number{return Math.max(min,Math.min(max,value))}
export function berlinDate(date:Date):string{return new Intl.DateTimeFormat('sv-SE',{timeZone:'Europe/Berlin',year:'numeric',month:'2-digit',day:'2-digit'}).format(date)}
export function clock(time:number):string{return new Intl.DateTimeFormat('de-DE',{timeZone:'Europe/Berlin',hour:'2-digit',minute:'2-digit',second:'2-digit'}).format(new Date(time*1000))}
