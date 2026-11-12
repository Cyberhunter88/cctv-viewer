import {moveCamera} from './view-state';
import type {Camera,Scan} from './types';
interface Context {
 api:<T>(url:string,options?:RequestInit)=>Promise<T>;notify:(message:string)=>void;cameras:()=>Camera[];order:()=>string[];setOrder:(order:string[])=>void;
 saving:()=>boolean;date:()=>string;dialog:HTMLElement;form:HTMLFormElement;setScan:(scan:Scan)=>void;
 discardCamera:()=>boolean;canChange:()=>boolean;clearSettings:()=>void;editCamera:(camera:Camera)=>void;showCameraEditor:()=>void;resetCamera:()=>void;
 openLive:(id:string)=>void|Promise<void>;renderCameras:()=>void;saveView:()=>Promise<void>;refreshCameras:()=>Promise<void>;loadDay:()=>Promise<void>;
}
// Camera administration list with scan history, per-camera actions and the folder browser.
export function initCameraList(ctx:Context){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 function element<K extends keyof HTMLElementTagNameMap>(tag:K,className='',text=''):HTMLElementTagNameMap[K]{const e=document.createElement(tag);e.className=className;e.textContent=text;return e}
 const on=(id:string,fn:()=>void|Promise<void>)=>$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>ctx.notify(e.message))});
 const historySection=element('section','scan-history');historySection.innerHTML='<h4>Letzte Indexierungen</h4><div id="scan-history-list" aria-live="polite"></div>';
 ctx.dialog.querySelector('.camera-maintenance')?.append(historySection);
 type ScanHistoryRow={cameraId:string;cameraName:string;date:string;manual:boolean;finished:number;state:string;inspected:number;added:number;changed:number;skipped:number;warnings:string[]};
 async function loadScanHistory(){
  const rows=await ctx.api<ScanHistoryRow[]>('/api/index/history'),root=$('scan-history-list');root.replaceChildren();
  for(const row of rows){
   const item=element('div','scan-history-entry');
   const when=new Date(row.finished*1000).toLocaleString('de-DE');
   item.append(element('strong','',`${row.cameraName} · ${when} · ${row.state}`),element('span','',` ${row.inspected} geprüft · ${row.added} neu · ${row.changed} geändert · ${row.skipped} übersprungen`));
   for(const warning of row.warnings)item.append(element('p','warning',warning));
   if(row.manual&&['partial','failed','interrupted'].includes(row.state)){
    const retry=element('button','button secondary','Erneut scannen');retry.type='button';retry.onclick=async()=>{ctx.setScan(await ctx.api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({camera:row.cameraId,date:row.date,all:true})}));ctx.notify('Scan erneut gestartet.')};item.append(retry);
   }
   root.append(item);
  }
  if(!rows.length)root.textContent='Noch keine abgeschlossene Indexierung.';
 }
 let cameraLiveCapabilities:Record<string,{enabled:boolean}>={};
 async function loadCameraLiveStatus(){
  try{const info=await ctx.api<{cameras:Record<string,{enabled:boolean}>}>('/api/live');cameraLiveCapabilities=info.cameras;renderCameraList()}catch{/* Recording configuration remains usable without live status. */}
 }
 const cameraSearch=document.createElement('input');cameraSearch.type='search';cameraSearch.placeholder='Kameras suchen';cameraSearch.setAttribute('aria-label','Kameras suchen');
 function renderCameraList(){
  const list=$('camera-list');list.replaceChildren();$('camera-list-count').textContent=String(ctx.cameras().length);
  if(!ctx.cameras().length){const empty=element('p','camera-empty','Noch keine Kameras eingerichtet. Füge deine erste Aufnahmequelle hinzu.');list.append(empty)}
  const editing=(ctx.form.elements.namedItem('id') as HTMLInputElement).value;
  for(let i=0;i<ctx.order().length;i++){
   const c=ctx.cameras().find(c=>c.id===ctx.order()[i]);if(!c||cameraSearch.value&&!c.name.toLocaleLowerCase('de').includes(cameraSearch.value.toLocaleLowerCase('de')))continue;
   const entry=element('article','camera-entry'+(editing===c.id?' is-selected':''));
   const select=element('button','camera-edit'),name=element('strong','camera-name',c.name);
   select.type='button';select.setAttribute('aria-label',c.name);select.setAttribute('aria-pressed',String(editing===c.id));
   const status=element('span','camera-status'+(c.sourceStatus==='offline'?' is-offline':''),`${c.sourceStatus==='offline'?'Quelle offline':'Quelle erreichbar'} · ${c.indexedVideos||0} ${c.indexedVideos===1?'Video':'Videos'}`);
   if(cameraLiveCapabilities[c.id])status.textContent+=' · '+(cameraLiveCapabilities[c.id].enabled?'Live eingerichtet':'Live aus');
   if(c.lastScan)status.textContent+=' · Scan '+new Date(c.lastScan*1000).toLocaleString('de-DE',{dateStyle:'short',timeStyle:'short',timeZone:'Europe/Berlin'});
   status.textContent+=' · '+(c.lastRecording?'Letzte Aufnahme '+new Date(c.lastRecording*1000).toLocaleString('de-DE',{dateStyle:'short',timeStyle:'short',timeZone:'Europe/Berlin'}):'Noch keine Aufnahme im Index');
   select.append(name,status,element('span','camera-edit-label','Bearbeiten →'));
   select.onclick=()=>{if(ctx.saving()||!ctx.discardCamera()||!ctx.canChange())return;ctx.editCamera(c);ctx.showCameraEditor()};
   const actions=element('div','camera-entry-actions');
   const more=element('details','camera-more');const summary=element('summary','','Aktionen');more.append(summary);const moreActions=element('div','camera-more-actions');more.append(moreActions);actions.append(more);
   const addAction=(text:string,label:string,action:()=>void|Promise<void>,className='')=>{const b=element('button',className,text);b.type='button';b.title=label;b.setAttribute('aria-label',label+' · '+c.name);b.disabled=ctx.saving();b.onclick=()=>void Promise.resolve(action()).catch(e=>ctx.notify(e.message));if(['scan-camera','cleanup-camera','delete'].includes(className))moreActions.append(b);else actions.insertBefore(b,more);return b};
   addAction('Live','Livequelle einrichten',()=>ctx.openLive(c.id),'text-button');
   const up=addAction('↑','Nach oben verschieben',()=>reorder(i-1),'move-button');up.disabled=ctx.saving()||i===0;
   const down=addAction('↓','Nach unten verschieben',()=>reorder(i+1),'move-button');down.disabled=ctx.saving()||i===ctx.order().length-1;
   const cameraId=c.id;
   function reorder(target:number){ctx.setOrder(moveCamera(ctx.order(),cameraId,ctx.order()[target]));ctx.renderCameras();renderCameraList();ctx.saveView()}
   addAction('Scannen','Kamera scannen',async()=>{ctx.setScan(await ctx.api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:ctx.date(),camera:c.id,all:true})}))},'scan-camera');
   addAction('Bereinigen','Fehlende Indexeinträge bereinigen',async()=>{
    const preview=await ctx.api<{camera:string;count:number;digest:string}>('/api/index/cleanup?camera='+encodeURIComponent(c.id));
    if(!preview.count){ctx.notify('Keine fehlenden Indexeinträge.');return}
    if(!confirm(`${preview.count} fehlende Indexeinträge von „${c.name}“ entfernen? Originalaufnahmen bleiben erhalten. Bitte vorher prüfen, ob die Quelle vollständig gemountet ist.`))return;
    const result=await ctx.api<{removed:number}>('/api/index/cleanup',{method:'POST',body:JSON.stringify({camera:c.id,digest:preview.digest})});
    ctx.notify(`${result.removed} Indexeinträge entfernt.`);await ctx.refreshCameras();await ctx.loadDay();
   },'cleanup-camera');
   addAction('Entfernen','Kamera entfernen',async()=>{
    if(!confirm(`Kamera „${c.name}“ aus dem Viewer entfernen? Die Originalaufnahmen bleiben erhalten.`))return;
    if(editing===c.id&&(!ctx.discardCamera()||!ctx.canChange()))return;
    await ctx.api('/api/cameras/'+c.id,{method:'DELETE'});if(editing===c.id){ctx.resetCamera();ctx.clearSettings();ctx.dialog.classList.remove('camera-editing')}await ctx.refreshCameras();await ctx.loadDay();
   },'delete');
   entry.draggable=!ctx.saving();
   entry.ondragstart=e=>e.dataTransfer?.setData('text/plain',c.id);
   entry.ondragover=e=>e.preventDefault();
   entry.ondrop=e=>{e.preventDefault();if(ctx.saving())return;const source=e.dataTransfer?.getData('text/plain');if(!source||source===c.id||!ctx.order().includes(source))return;ctx.setOrder(moveCamera(ctx.order(),source,c.id));ctx.renderCameras();renderCameraList();ctx.saveView()};
   status.id='camera-source-'+c.id;select.setAttribute('aria-describedby',status.id);
   select.disabled=ctx.saving();entry.append(select,actions);list.append(entry);
  }
 }
 async function browseFolder(folder=''){const data=await ctx.api<{path:string;folders:string[];samples:string[];limited:boolean}>('/api/cameras/folders?path='+encodeURIComponent(folder));const root=$('folder-browser');root.hidden=false;root.replaceChildren();root.append(element('strong','',data.path==='.'?'Aufnahmeverzeichnis':data.path));if(data.path!=='.'){const back=element('button','text-button','↑ Übergeordneter Ordner');back.type='button';back.onclick=()=>void browseFolder(data.path.split('/').slice(0,-1).join('/')).catch(e=>ctx.notify(e.message));root.append(back);const choose=element('button','button secondary','Diesen Ordner verwenden');choose.type='button';choose.onclick=()=>{const folderField=ctx.form.elements.namedItem('folder') as HTMLInputElement,nameField=ctx.form.elements.namedItem('name') as HTMLInputElement,previous=folderField.value.split('/').pop();folderField.value=data.path;if(!nameField.value||nameField.value===previous)nameField.value=data.path.split('/').pop()||'';ctx.form.dispatchEvent(new Event('input',{bubbles:true}));root.hidden=true};root.append(choose)}for(const folder of data.folders){const b=element('button','text-button',folder.split('/').pop());b.type='button';b.onclick=()=>void browseFolder(folder).catch(e=>ctx.notify(e.message));root.append(b)}if(data.limited)root.append(element('small','','Suche begrenzt; bitte einen Unterordner wählen.'))}
 on('browse-folder',()=>browseFolder());
 // The search field is inserted once the camera editor has arranged the page.
 function mountSearch(){$('camera-list').before(cameraSearch);cameraSearch.addEventListener('input',renderCameraList);}
 return {renderCameraList,loadScanHistory,loadCameraLiveStatus,mountSearch};
}
