// Optional playback trace for the diagnostics dialog. Recording continues while the dialog is closed.
type Context={api:<T>(url:string,options?:RequestInit)=>Promise<T>;notify:(message:string)=>void};
let trace:{at:number;event:string;camera?:string;detail?:string}[]=[],tracing=false,traceStart=0;
export function log(event:string,camera?:string,detail?:string){if(tracing&&trace.length<20000)trace.push({at:Math.round(performance.now()-traceStart),event,camera,detail});}
export function initDiagnostics(c:Context){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const on=(id:string,fn:()=>void|Promise<void>)=>$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>c.notify(e.message))});
 const diagnostics=$<HTMLDialogElement>('diagnostics-dialog');
 on('diagnostics-open',()=>diagnostics.showModal());on('diagnostics-close',()=>diagnostics.close());
 on('trace-stop',()=>{tracing=false;$<HTMLInputElement>('trace-enabled').checked=false;$<HTMLButtonElement>('trace-stop').disabled=true;$<HTMLButtonElement>('trace-download').disabled=trace.length===0;$('trace-status').textContent='Gestoppt · '+trace.length+' Einträge'});
 $<HTMLInputElement>('trace-enabled').onchange=()=>{tracing=$<HTMLInputElement>('trace-enabled').checked;if(tracing){trace=[];traceStart=performance.now();log('start')}else log('stop');$<HTMLButtonElement>('trace-stop').disabled=!tracing;$<HTMLButtonElement>('trace-download').disabled=tracing||trace.length===0;$('trace-status').textContent=tracing?'Aufzeichnung läuft (maximal 20.000 Einträge)':'Gestoppt'};
 on('trace-download',async()=>{const status=await c.api('/api/playback-status');const blob=new Blob([JSON.stringify({version:1,status,events:trace},null,2)],{type:'application/json'});const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download='wiedergabediagnose.json';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000)});
}
