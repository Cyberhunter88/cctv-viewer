import {initCameraSettings} from './camera-settings';
import {initSettingsLayout} from './settings-layout';
import {initLivePage} from './live-page';
import {initLiveAdmin} from './live-admin';
import {registerPage,navigate,startNavigation,currentPage} from './navigation';
import {PreferenceWriter} from './preferences';
import {prepareDialogs} from './dialogs';
import {Tile,setControlIcon,type TileContext} from './playback-tile';
import {createPlaybackClock} from './playback-clock';
import {createPoller} from './poller';
import {validView,gridSize,pageCapacity,hotspotCandidate,recordingBoundary,moveCamera,defaultView,type ViewState} from './view-state';
import {attachTimelineControls} from './timeline-controls';
import {initManagement} from './management';
import {recordingAt,nextRecording,playbackRecordings,displayedRecordings,firstRecordingStart,clamp,berlinDate,clock,type Recording} from './timeline';
import {initSearchPage,type SearchPage} from './search-page';
import {initCameraEditor} from './camera-editor';
import {createTimelineView} from './timeline-view';
interface Camera {viewMode?:string;wideMarker?:string;zoomMarker?:string;zoomOffset?:number;supportsZoom?:boolean;id:string;name:string;folder:string;pattern:string;timezone:string;offset:number;format?:string;prefix?:string;indexingMode?:string;directoryPattern?:string;sourceStatus?:string;lastScan?:number;indexedVideos?:number}
interface ViewDetection {status:'confirmed'|'swapped'|'unclear';message:string;pairs:number}
interface Session {preferenceKey:string;name:string;role:'admin'|'viewer';csrf:string;kind:'local'|'oidc'}
interface CameraScan {id:string;name:string;state:string;inspected:number;added:number;changed:number;skipped:number}
interface Scan {jobId?:string;state?:string;completion?:number;queue?:{id:string;date:string;camera:string;automatic:boolean}[];cameras?:CameraScan[];active?:string[];completed?:number;total?:number;running:boolean;date:string;camera:string;indexed:number;warnings:string[]}
interface Day {recordings:Recording[];from:number;to:number;scan:Scan}
prepareDialogs();
const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
let session:Session, cameras:Camera[]=[],records:Recording[]=[],playableRecords:Recording[]=[],displayedRecords:Recording[]=[],selected=new Set<string>(),from=0,to=0,cursor=0,viewStart=0,viewEnd=0,playing=false,scanRunning=false,loadGeneration=0,dayLoadPending=false;
let thumbGeneration=0;const thumbURLs:string[]=[];
const tiles=new Map<string,Tile>();
const openZoom=new Set<string>();
let audioTile:Tile|undefined;
const dateInput=$<HTMLInputElement>('date'),canvas=$<HTMLCanvasElement>('timeline'),overview=$<HTMLCanvasElement>('overview');
const timelineView=createTimelineView(canvas,overview);
const speed=()=>Number($<HTMLSelectElement>('speed').value);
let refreshRange=()=>{};let page=0,order:string[]=[],hotspot:string|undefined,lastHotspotCursor:number|undefined;
// A completion observed during a day request may be newer than its recording snapshot.
let scanCompletion=0,loadedScanCompletion=0;
let toastTimer:ReturnType<typeof setTimeout>|undefined;
 const state:ViewState=defaultView();
 let trace:{at:number;event:string;camera?:string;detail?:string}[]=[],tracing=false,traceStart=0;
 function log(event:string,camera?:string,detail?:string){if(tracing&&trace.length<20000)trace.push({at:Math.round(performance.now()-traceStart),event,camera,detail});}

let fullscreenGridPending=false;
async function toggleFullscreen(tile:HTMLElement){
 try{
  if(document.fullscreenElement){await document.exitFullscreen();return}
  if(document.fullscreenEnabled&&tile.requestFullscreen)await tile.requestFullscreen();
  else {tile.classList.toggle('expanded');const button=tile.querySelector('.tile-expand');if(button){button.setAttribute('aria-label',tile.classList.contains('expanded')?'Vergrößerte Ansicht schließen':'Ansicht vergrößern');setControlIcon(button,tile.classList.contains('expanded')?'collapse':'expand')}}
 }catch{notify('Vollbild konnte nicht geöffnet werden. Bitte erneut versuchen.')}
}
document.addEventListener('fullscreenchange',()=>{
 if(!document.fullscreenElement&&fullscreenGridPending){fullscreenGridPending=false;renderGrid();update()}
 for(const tile of tiles.values()){const active=document.fullscreenElement===tile.container;const button=tile.container.querySelector('.tile-expand');if(button){button.setAttribute('aria-label',active?'Vollbild schließen':document.fullscreenEnabled?'Vollbild öffnen':'Ansicht vergrößern');setControlIcon(button,active?'collapse':'expand')}tile.applyAspect()}
});
function applyRail(){
 const collapsed=state.railCollapsed??matchMedia('(max-width:1100px)').matches;
 document.documentElement.classList.toggle('rail-collapsed',collapsed);
 $('rail-toggle').setAttribute('aria-expanded',String(!collapsed));
 $('rail-toggle').setAttribute('aria-label',collapsed?'Seitenleiste ausklappen':'Seitenleiste einklappen');
 scheduleResize();
}
on('rail-toggle',()=>{state.railCollapsed=!(state.railCollapsed??matchMedia('(max-width:1100px)').matches);applyRail();saveView()});
for(const group of document.querySelectorAll<HTMLDetailsElement>('#rail-nav .nav-group')){
 group.querySelector('summary')?.addEventListener('click',event=>{
  if(!document.documentElement.classList.contains('rail-collapsed')||matchMedia('(max-width:700px)').matches)return;
  event.preventDefault();group.open=true;state.railCollapsed=false;applyRail();saveView();
  group.querySelector<HTMLAnchorElement>('.nav-submenu a')?.focus();
 });
}
matchMedia('(max-width:1100px)').addEventListener('change',applyRail);
const accountMenu=$<HTMLDetailsElement>('account-menu');
accountMenu.querySelector('summary')!.addEventListener('click',e=>{if($('password-button').hidden)e.preventDefault()});
function closeAccount(){if(!accountMenu.open)return;accountMenu.open=false;accountMenu.querySelector('summary')!.focus()}
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&accountMenu.open){e.preventDefault();closeAccount()}});
document.addEventListener('click',e=>{if(e.target instanceof Node&&!accountMenu.contains(e.target))accountMenu.open=false});
for(const id of ['password-button','logout'])$(id).addEventListener('click',()=>{accountMenu.open=false});

let preferenceWriter:PreferenceWriter|undefined;
function preferenceSnapshot():Record<string,unknown>{
 state.cameras=[...selected];state.order=order;state.hotspot=hotspot;
 for(const id of ['layout','quality','speed'] as const)state[id]=$<HTMLSelectElement>(id).value;
 // An empty hotspot clears the stored selection without JSON null semantics.
 return {...state,railCollapsed:state.railCollapsed??null,hotspot:hotspot||'',theme:state.theme||'system'};
}
function preferenceStatus(saving:boolean,error?:unknown){
 $('preference-status').textContent=saving?'Einstellungen werden gespeichert …':error?'Einstellungen nicht gespeichert. Bitte erneut versuchen.':'';
 $('preference-status').hidden=!saving&&!error;$('preferences-retry').hidden=!error;
 if(error)notify('Persönliche Einstellungen nicht gespeichert. Erneut versuchen unter Persönliche Einstellungen.');
}
function saveView():Promise<void>{
 if(!session||!preferenceWriter)return Promise.resolve();
 const snapshot=preferenceSnapshot();
 try{localStorage.setItem('cctv-view:'+session.preferenceKey,JSON.stringify(snapshot))}catch{}
 const pending=preferenceWriter.save(snapshot);
 // Automatic changes report failures through the persistent status and retry UI.
 void pending.catch(()=>{});return pending;
}
const livePage=initLivePage({api,cameras:()=>cameras,preferences:()=>state,save:saveView,admin:()=>session?.role==='admin',notify});
function cameraViewMode(id:string){
 const selected=(cameraForm.elements.namedItem('id') as HTMLInputElement).value;
 if(selected===id)return (cameraForm.elements.namedItem('viewMode') as HTMLSelectElement).value;
 return cameras.find(camera=>camera.id===id)?.viewMode||'single';
}
const liveAdmin=initLiveAdmin({api,cameras:()=>cameras,viewMode:cameraViewMode,notify,changed:()=>{livePage.refresh();void loadCameraLiveStatus()}});
on('preferences-retry',async()=>{try{await preferenceWriter?.flush()}catch{}});

function notify(message:string){$('toast').textContent=message;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,6000)}
async function api<T>(url:string,options:RequestInit={}):Promise<T>{
 const headers=new Headers(options.headers);if(options.body)headers.set('Content-Type','application/json');if(session)headers.set('X-CSRF-Token',session.csrf);
 const response=await fetch(url,{...options,headers});if(response.status===401){livePage.stop();pause();location.reload();throw new Error('Anmeldung abgelaufen')}
 const text=await response.text();let body:any={};try{body=text?JSON.parse(text):{}}catch{body={error:text||`Serverfehler (${response.status})`}}
 if(!response.ok)throw Object.assign(new Error((body.error||`Anfrage fehlgeschlagen (${response.status})`)+(body.gaps?'\n'+body.gaps.map((g:number[])=>clock(g[0])+' – '+clock(g[1])).join('\n'):'')),{indexing:body.indexing===true,status:response.status});return body as T;
}
function on(id:string,fn:()=>void|Promise<void>){$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>notify(e.message))})}
function element<K extends keyof HTMLElementTagNameMap>(tag:K,className='',text=''):HTMLElementTagNameMap[K]{const e=document.createElement(tag);e.className=className;e.textContent=text;return e}
function ordered(){return order.map(id=>cameras.find(c=>c.id===id)).filter((c):c is Camera=>!!c&&selected.has(c.id))}
function grid(){return gridSize($<HTMLSelectElement>('layout').value,state.columns,state.rows,selected.size,$('video-grid').clientWidth||innerWidth,ordered().some(c=>c.viewMode==='dual'&&openZoom.has(c.id)))}
function visiblePageCapacity(){const [cols,rows]=grid();return pageCapacity($<HTMLSelectElement>('layout').value,cols,rows)}
function visible():Camera[]{const all=ordered(),capacity=visiblePageCapacity();page=clamp(page,0,Math.max(0,Math.ceil(all.length/capacity)-1));return all.slice(page*capacity,(page+1)*capacity)}
function pause(){playing=false;playbackClock.stop();$('play').textContent='▶';$('play').setAttribute('aria-label','Abspielen');tiles.forEach(t=>t.paused());log('pause');renderPosition()}
function play(){if(!playableRecords.length||!visible().length){notify('Für diese Auswahl sind keine Aufnahmen vorhanden.');return}if(!visible().some(c=>recordingAt(displayedRecords,c.id,cursor))){const next=nextRecording(displayedRecords,visible().map(c=>c.id),cursor);if(next===undefined){notify('Keine weiteren Aufnahmen an diesem Tag.');return}cursor=next}log('play');playing=true;$('play').textContent='Ⅱ';$('play').setAttribute('aria-label','Pause');tiles.forEach(t=>t.retry());update();if(!document.hidden&&currentPage()==='recordings')playbackClock.start()}
function seek(time:number){playbackClock.reset();lastHotspotCursor=undefined;log('seek',undefined,String(time));cursor=clamp(time,from,to-.001);update()}
const tileContext:TileContext={
 state,cursor:()=>cursor,playing:()=>playing,speed,quality:()=>$<HTMLSelectElement>('quality').value,
 recordings:()=>records,playableRecordings:()=>playableRecords,audioTile:()=>audioTile,setAudioTile:tile=>{audioTile=tile},
 selectHotspot:id=>{hotspot=id;state.autoHotspot=false;$<HTMLInputElement>('auto-hotspot').checked=false;renderGrid();saveView()},
 toggleFullscreen,notify,log,request:api
};
function synchronizeTiles(){
 if(!from)return;
 if(state.autoHotspot&&$<HTMLSelectElement>('layout').value==='hotspot'){
  const candidate=hotspotCandidate(displayedRecords,visible().map(c=>c.id),cursor,playing&&lastHotspotCursor!==undefined&&cursor>=lastHotspotCursor?lastHotspotCursor:undefined);
  if(candidate&&candidate!==hotspot){hotspot=candidate;renderGrid()}
 }
 lastHotspotCursor=cursor;const timestamp=clock(cursor);tiles.forEach(t=>t.update(timestamp));
}
function renderPosition(){if(!from)return;const timestamp=clock(cursor);if($('clock').textContent!==timestamp)$('clock').textContent=timestamp;draw();refreshRange()}
function update(){synchronizeTiles();renderPosition()}
const playbackClock=createPlaybackClock({
 advance:seconds=>{
  const matching=[...tiles.values()].filter(t=>t.current());
  if(matching.length&&!matching.some(t=>t.failed||!t.loading&&t.video.readyState>=2))return;
  cursor+=seconds*speed();
  if(cursor>=to){cursor=to-.001;pause();return}
  if(!visible().some(c=>recordingAt(displayedRecords,c.id,cursor))){
   const next=nextRecording(displayedRecords,visible().map(c=>c.id),cursor);
   if(next===undefined){pause();return}cursor=next;
  }
  if(cursor>viewEnd&&viewEnd-viewStart<to-from){const width=viewEnd-viewStart;viewStart=clamp(cursor-width/4,from,to-width);viewEnd=viewStart+width}
 },
 synchronize:synchronizeTiles,render:renderPosition
});
const scanPoller=createPoller({
 request:async signal=>{
  const scan=await api<Scan>('/api/scan',{signal});if(signal.aborted)return;
  const wasRunning=scanRunning;setScan(scan);
  if((wasRunning&&!scan.running)||scanCompletion>loadedScanCompletion){
   await loadDay(false,signal);if(signal.aborted)return;
   if(currentPage()==='admin/cameras')await loadScanHistory();
  }
 },onError:error=>notify((error as Error).message)
});
let pollingReady=false;
function stopBackground(){pollingReady=false;scanPoller.stop();pause();tiles.forEach(t=>t.release())}
function refreshDisplayedRecords(){displayedRecords=displayedRecordings(playableRecords,cameras,openZoom)}
function renderGrid(){
 // Detaching the native fullscreen element ends fullscreen, including during viewport changes.
 if(document.fullscreenElement&&$('video-grid').contains(document.fullscreenElement)){fullscreenGridPending=true;return}
 const focused=document.activeElement instanceof HTMLElement&&$('video-grid').contains(document.activeElement)?document.activeElement:undefined;
 refreshDisplayedRecords();
 const active=visible();for(const[id,t]of tiles)if(!active.some(c=>c.id===t.camera.id&& (c.viewMode==='dual'?(t.view==='wide'||t.view==='zoom'&&openZoom.has(c.id)):!t.view))){t.destroy();tiles.delete(id)}
 const gridElement=$('video-grid'),[cols]=grid();gridElement.className='video-grid';gridElement.style.gridTemplateColumns=`repeat(${cols},minmax(0,1fr))`;
 gridElement.replaceChildren();
 const isHotspot=$<HTMLSelectElement>('layout').value==='hotspot';if(!active.some(c=>c.id===hotspot))hotspot=active[0]?.id;
 for(const c of active){
  const group=element('div','camera-group');group.id='camera-group-'+c.id;group.classList.toggle('dual',c.viewMode==='dual');group.classList.toggle('zoom-open',openZoom.has(c.id));group.classList.toggle('hotspot-primary',isHotspot&&c.id===hotspot&&cols>1&&grid()[1]>1);
  let zoomToggle:HTMLButtonElement|undefined;
  if(c.viewMode==='dual' && c.supportsZoom !== false){
   const toggle=element('button','zoom-toggle',openZoom.has(c.id)?'Zoom schließen':'Zoom anzeigen');toggle.type='button';toggle.id='zoom-toggle-'+c.id;toggle.setAttribute('aria-label',`${c.name}: ${toggle.textContent}`);toggle.setAttribute('aria-expanded',String(openZoom.has(c.id)));toggle.setAttribute('aria-controls',group.id);
   toggle.onclick=async()=>{if(document.fullscreenElement)await document.exitFullscreen();if(openZoom.has(c.id))openZoom.delete(c.id);else openZoom.add(c.id);page=Math.floor(ordered().findIndex(camera=>camera.id===c.id)/visiblePageCapacity());renderGrid();update();void thumbnails();$(toggle.id).focus()};toggle.ondblclick=e=>e.stopPropagation();zoomToggle=toggle;
  }
  for(const view of c.viewMode==='dual'?(c.supportsZoom===false?['wide']:(openZoom.has(c.id)?['wide','zoom']:['wide'])):[undefined]){
   const key=c.id+':'+(view||'single');if(!tiles.has(key))tiles.set(key,new Tile(c,view,tileContext));const tile=tiles.get(key)!;tile.camera=c;tile.container.querySelector('.zoom-toggle')?.remove();if(view==='wide'&&zoomToggle)tile.audioButton.before(zoomToggle);group.append(tile.container);tile.applyAspect();tile.video.preload=state.preload as 'metadata'|'auto';
  }
  gridElement.append(group);if(isHotspot&&c.id===hotspot)gridElement.prepend(group);
 }
 if(cameras.length>0&&selected.size===0)gridElement.append(element('div','empty-filter-hint','Keine Kameras ausgewählt. Wähle oben mindestens eine Kamera aus, um die Aufnahmen anzuzeigen.'));
 const capacity=visiblePageCapacity();document.querySelector('.camera-page-controls')!.classList.toggle('single-page',selected.size<=capacity);$('camera-range').textContent=selected.size?`Kameras ${page*capacity+1}–${Math.min((page+1)*capacity,selected.size)} von ${selected.size}`:'Keine Kamera ausgewählt';const layout=$<HTMLSelectElement>('layout').value,requested=layout==='custom'?state.columns:Number(layout.split('x')[0]);$('grid-hint').textContent=Number.isFinite(requested)&&requested>cols?`Raster an Bildschirm angepasst: ${cols} Spalte${cols===1?'':'n'}`:'';$('page-display').textContent=`Seite ${page+1} von ${Math.max(1,Math.ceil(selected.size/capacity))}`;$<HTMLButtonElement>('page-prev').disabled=page===0;$<HTMLButtonElement>('page-next').disabled=(page+1)*capacity>=selected.size;
 $('custom-grid').hidden=$<HTMLSelectElement>('layout').value!=='custom';$('hotspot-control').hidden=!isHotspot;
 // Reattaching a tile blurs its controls even when the same node is reused.
 const restored=focused?.isConnected?focused:focused?.id?document.getElementById(focused.id):null;
 restored?.focus({preventScroll:true});
}
function renderCameras(){
 lastHotspotCursor=undefined;playableRecords=playbackRecordings(records,cameras); const filters=$('camera-filters');filters.replaceChildren();for(const id of order){const camera=cameras.find(c=>c.id===id);if(!camera)continue;const b=element('button','filter'+(selected.has(id)?' selected':''),camera.name);b.setAttribute('aria-pressed',String(selected.has(id)));b.draggable=true;b.ondragstart=e=>e.dataTransfer?.setData('text/plain',id);b.ondragover=e=>e.preventDefault();b.ondrop=e=>{e.preventDefault();const source=e.dataTransfer?.getData('text/plain');if(!source||!order.includes(source))return;order=moveCamera(order,source,id);renderCameras();saveView()};b.onclick=()=>{if(selected.has(id))selected.delete(id);else selected.add(id);page=0;renderCameras();saveView()};filters.append(b)}
 renderGrid();$('empty').hidden=cameras.length!==0;$('timeline-section').hidden=cameras.length===0;$('playback-controls').hidden=cameras.length===0;$('add-first').hidden=session.role!=='admin';update();void thumbnails();
}
function setScan(scan:Scan){
 scanRunning=scan.running;scanCompletion=scan.completion??scanCompletion;
 const labels:Record<string,string>={queued:'Wartet',running:'Wird geprüft',complete:'Abgeschlossen',partial:'Teilweise abgeschlossen',failed:'Fehlgeschlagen',cancelled:'Abgebrochen'};
 $('scan-status').textContent=scan.running?`Indexierung · ${scan.completed||0}/${scan.total||0} Kameras · ${scan.indexed} neue/geänderte Videos`:(scan.state==='partial'?'Indexierung teilweise abgeschlossen':scan.state==='cancelled'?'Indexierung abgebrochen':scan.state==='failed'?'Indexierung fehlgeschlagen':'Index bereit');
 $('index-dot').classList.toggle('busy',scan.running);for(const id of ['rescan','scan-all','rebuild-index'])$<HTMLButtonElement>(id).disabled=scan.running;
 const root=$('scan-cameras');root.replaceChildren();for(const c of scan.cameras||[])root.append(element('div','scan-camera-status',`${c.name}: ${labels[c.state]||c.state} · ${c.inspected} geprüft · ${c.added} neu · ${c.changed} geändert · ${c.skipped} übersprungen`));
 $('scan-queue').textContent=`${scan.queue?.length||0} Aufträge warten${scan.queue?.length?' · '+scan.queue.map(q=>`${cameras.find(c=>c.id===q.camera)?.name||'Alle Kameras'} (${q.date})`).join(', '):''}`;
 $('warnings').hidden=!scan.warnings?.length;$('warnings').textContent=(scan.warnings||[]).join('\n');
}
async function loadDay(reset=true,signal?:AbortSignal){
 // A scan refresh must not supersede an explicit date change or its reset.
 if(!reset&&dayLoadPending)return;
 const generation=reset?++loadGeneration:loadGeneration,date=dateInput.value,completionAtStart=scanCompletion;
 if(reset){dayLoadPending=true;pause();tiles.forEach(t=>t.release())}
 try{
  const data=await api<Day>('/api/day?date='+encodeURIComponent(date),{signal});
  if(signal?.aborted||generation!==loadGeneration||date!==dateInput.value)return;
  loadedScanCompletion=completionAtStart;records=data.recordings;playableRecords=playbackRecordings(records,cameras);from=data.from;to=data.to;setScan(data.scan);refreshDisplayedRecords();
  if(reset){const ids=new Set(visible().map(c=>c.id));cursor=firstRecordingStart(displayedRecords.filter(r=>ids.has(r.cameraId)),from);viewStart=from;viewEnd=to}
  $('recording-count').textContent=`${records.length} Aufnahmen · ${cameras.length} Kameras`;update();void thumbnails();refreshRange();
 }finally{if(reset&&generation===loadGeneration)dayLoadPending=false}
}
async function refreshCameras(){const previous=new Set(cameras.map(c=>c.id));cameras=await api<Camera[]>('/api/cameras');for(const id of openZoom)if(!cameras.some(c=>c.id===id&&c.viewMode==='dual'))openZoom.delete(id);order=[...order.filter(id=>cameras.some(c=>c.id===id)),...cameras.filter(c=>!order.includes(c.id)).map(c=>c.id)];for(const c of cameras)if(!previous.has(c.id))selected.add(c.id);for(const id of selected)if(!cameras.some(c=>c.id===id))selected.delete(id);renderCameras();renderCameraList();livePage.refresh()}
function draw(){timelineView.draw({from,to,cursor,viewStart,viewEnd,cameras:visible(),records:displayedRecords,height:state.timelineHeight})}
function zoom(factor:number,anchor=cursor){const span=clamp((viewEnd-viewStart)*factor,300,to-from);viewStart=clamp(anchor-span/2,from,to-span);viewEnd=viewStart+span;draw();refreshRange()}
// Resize callbacks only schedule work: changing an observed layout inside the
// callback causes undelivered notifications in WebKit, especially for paired tiles.
let resizeFrame=0,gridResizePending=false;
function scheduleResize(gridChanged=false){
 gridResizePending ||= gridChanged;if(resizeFrame)return;
 resizeFrame=requestAnimationFrame(()=>{resizeFrame=0;const rebuild=gridResizePending;gridResizePending=false;if(!session||!from)return;if(rebuild)renderGrid();draw();refreshRange()});
}
new ResizeObserver(()=>scheduleResize()).observe(canvas.parentElement!);
async function thumbnails(){const generation=++thumbGeneration;for(const url of thumbURLs)URL.revokeObjectURL(url);thumbURLs.length=0;const root=$('thumbnails');root.replaceChildren();if(!state.showThumbnails)return;const ids=visible().map(c=>c.id),items=displayedRecords.filter(r=>ids.includes(r.cameraId)).sort((a,b)=>a.start-b.start);const sample=items.filter((_,i)=>i%Math.max(1,Math.ceil(items.length/8))===0).slice(0,8);
 const pairs=sample.map(record=>{const button=element('button','thumb'),img=element('img'),label=element('span','',clock(record.start));img.alt='Vorschau '+clock(record.start);button.title=clock(record.start);button.append(img,label);button.onclick=()=>seek(record.start);root.append(button);return{record,img}});
 await Promise.allSettled(pairs.map(async({record,img})=>{try{const response=await fetch('/api/recordings/'+record.id+'/thumbnail');if(!response.ok)return;const blob=await response.blob();if(generation!==thumbGeneration)return;const url=URL.createObjectURL(blob);thumbURLs.push(url);img.src=url}catch{}}));
}
const dialog=$('camera-dialog'),cameraForm=$<HTMLFormElement>('camera-form');
const historySection=element('section','scan-history');historySection.innerHTML='<h4>Letzte Indexierungen</h4><div id="scan-history-list" aria-live="polite"></div>';
dialog.querySelector('.camera-maintenance')?.append(historySection);
type ScanHistoryRow={cameraId:string;cameraName:string;date:string;manual:boolean;finished:number;state:string;inspected:number;added:number;changed:number;skipped:number;warnings:string[]};
async function loadScanHistory(){
 const rows=await api<ScanHistoryRow[]>('/api/index/history'),root=$('scan-history-list');root.replaceChildren();
 for(const row of rows){
  const item=element('div','scan-history-entry');
  const when=new Date(row.finished*1000).toLocaleString('de-DE');
  item.append(element('strong','',`${row.cameraName} · ${when} · ${row.state}`),element('span','',` ${row.inspected} geprüft · ${row.added} neu · ${row.changed} geändert · ${row.skipped} übersprungen`));
  for(const warning of row.warnings)item.append(element('p','warning',warning));
  if(row.manual&&['partial','failed','interrupted'].includes(row.state)){
   const retry=element('button','button secondary','Erneut scannen');retry.type='button';retry.onclick=async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({camera:row.cameraId,date:row.date,all:true})}));notify('Scan erneut gestartet.')};item.append(retry);
  }
  root.append(item);
 }
 if(!rows.length)root.textContent='Noch keine abgeschlossene Indexierung.';
}
let cameraSaving=false;
let cameraLiveCapabilities:Record<string,{enabled:boolean}>={};
async function loadCameraLiveStatus(){
 try{const info=await api<{cameras:Record<string,{enabled:boolean}>}>('/api/live');cameraLiveCapabilities=info.cameras;renderCameraList()}catch{/* Recording configuration remains usable without live status. */}
}
const {cameraInput,discardCamera,markCameraSaved,resetCamera,showCameraEditor,editCamera,leaveCameraEditor}=initCameraEditor(cameraForm,dialog,renderCameraList,()=>cameraSaving);
const cameraSettings=initCameraSettings(cameraForm,dialog,liveAdmin,()=>cameras[0]?.id||'',id=>{
 const camera=cameras.find(camera=>camera.id===id);if(!camera||cameraSaving||!discardCamera())return false;editCamera(camera);showCameraEditor();return true;
},()=>!cameraSaving&&discardCamera(),resetCamera);
cameraForm.addEventListener('cameraeditorclosed',()=>cameraSettings.clear());
const cameraSearch=document.createElement('input');cameraSearch.type='search';cameraSearch.placeholder='Kameras suchen';cameraSearch.setAttribute('aria-label','Kameras suchen');$('camera-list').before(cameraSearch);cameraSearch.addEventListener('input',renderCameraList);

function renderCameraList(){
 const list=$('camera-list');list.replaceChildren();$('camera-list-count').textContent=String(cameras.length);
 if(!cameras.length){const empty=element('p','camera-empty','Noch keine Kameras eingerichtet. Füge deine erste Aufnahmequelle hinzu.');list.append(empty)}
 const editing=(cameraForm.elements.namedItem('id') as HTMLInputElement).value;
 for(let i=0;i<order.length;i++){
  const c=cameras.find(c=>c.id===order[i]);if(!c||cameraSearch.value&&!c.name.toLocaleLowerCase('de').includes(cameraSearch.value.toLocaleLowerCase('de')))continue;
  const entry=element('article','camera-entry'+(editing===c.id?' is-selected':''));
  const select=element('button','camera-edit'),name=element('strong','camera-name',c.name);
  select.type='button';select.setAttribute('aria-label',c.name);select.setAttribute('aria-pressed',String(editing===c.id));
  const status=element('span','camera-status'+(c.sourceStatus==='offline'?' is-offline':''),`${c.sourceStatus==='offline'?'Quelle offline':'Quelle erreichbar'} · ${c.indexedVideos||0} ${c.indexedVideos===1?'Video':'Videos'}`);
  if(cameraLiveCapabilities[c.id])status.textContent+=' · '+(cameraLiveCapabilities[c.id].enabled?'Live aktiv':'Live aus');
  select.append(name,status,element('span','camera-edit-label','Bearbeiten →'));
  select.onclick=()=>{if(cameraSaving||!discardCamera()||!cameraSettings.canChange())return;editCamera(c);showCameraEditor()};
  const actions=element('div','camera-entry-actions');
  const more=element('details','camera-more');const summary=element('summary','','Aktionen');more.append(summary);const moreActions=element('div','camera-more-actions');more.append(moreActions);actions.append(more);
  const addAction=(text:string,label:string,action:()=>void|Promise<void>,className='')=>{const b=element('button',className,text);b.type='button';b.title=label;b.setAttribute('aria-label',label+' · '+c.name);b.disabled=cameraSaving;b.onclick=()=>void Promise.resolve(action()).catch(e=>notify(e.message));if(['scan-camera','cleanup-camera','delete'].includes(className))moreActions.append(b);else actions.insertBefore(b,more);return b};
  addAction('Live','Livequelle einrichten',()=>liveAdmin.open(c.id),'text-button');
  const up=addAction('↑','Nach oben verschieben',()=>reorder(i-1),'move-button');up.disabled=cameraSaving||i===0;
  const down=addAction('↓','Nach unten verschieben',()=>reorder(i+1),'move-button');down.disabled=cameraSaving||i===order.length-1;
  const cameraId=c.id;
  function reorder(target:number){order=moveCamera(order,cameraId,order[target]);renderCameras();renderCameraList();saveView()}
  addAction('Scannen','Kamera scannen',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value,camera:c.id,all:true})}))},'scan-camera');
  addAction('Bereinigen','Fehlende Indexeinträge bereinigen',async()=>{
   const preview=await api<{camera:string;count:number;digest:string}>('/api/index/cleanup?camera='+encodeURIComponent(c.id));
   if(!preview.count){notify('Keine fehlenden Indexeinträge.');return}
   if(!confirm(`${preview.count} fehlende Indexeinträge von „${c.name}“ entfernen? Originalaufnahmen bleiben erhalten. Bitte vorher prüfen, ob die Quelle vollständig gemountet ist.`))return;
   const result=await api<{removed:number}>('/api/index/cleanup',{method:'POST',body:JSON.stringify({camera:c.id,digest:preview.digest})});
   notify(`${result.removed} Indexeinträge entfernt.`);await refreshCameras();await loadDay();
  },'cleanup-camera');
  addAction('Entfernen','Kamera entfernen',async()=>{
   if(!confirm(`Kamera „${c.name}“ aus dem Viewer entfernen? Die Originalaufnahmen bleiben erhalten.`))return;
   if(editing===c.id&&(!discardCamera()||!cameraSettings.canChange()))return;
   await api('/api/cameras/'+c.id,{method:'DELETE'});if(editing===c.id){resetCamera();cameraSettings.clear();dialog.classList.remove('camera-editing')}await refreshCameras();await loadDay();
  },'delete');
  entry.draggable=!cameraSaving;
  entry.ondragstart=e=>e.dataTransfer?.setData('text/plain',c.id);
  entry.ondragover=e=>e.preventDefault();
  entry.ondrop=e=>{e.preventDefault();if(cameraSaving)return;const source=e.dataTransfer?.getData('text/plain');if(!source||source===c.id||!order.includes(source))return;order=moveCamera(order,source,c.id);renderCameras();renderCameraList();saveView()};
  status.id='camera-source-'+c.id;select.setAttribute('aria-describedby',status.id);
  select.disabled=cameraSaving;entry.append(select,actions);list.append(entry);
 }
}
resetCamera();
cameraForm.addEventListener('submit',async e=>{
 e.preventDefault();if(cameraSaving)return;cameraSaving=true;const submit=cameraForm.querySelector<HTMLButtonElement>('button[type=submit]')!;submit.disabled=true;renderCameraList();
 const controls=[...cameraForm.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLButtonElement>('input,select,button'),...['close-dialog','new-camera','batch-open'].map(id=>$<HTMLButtonElement>(id))];const disabled=controls.map(c=>c.disabled);
 const payload=cameraInput();controls.forEach(c=>c.disabled=true);
 try{
  $('camera-error').textContent='';const saved=await api<Camera&{detection?:ViewDetection}>('/api/cameras',{method:'POST',body:JSON.stringify(payload)});
  // The POST committed successfully even if refreshing recordings subsequently fails.
  (cameraForm.elements.namedItem('id') as HTMLInputElement).value=saved.id;markCameraSaved();
  await refreshCameras();editCamera(cameras.find(c=>c.id===saved.id)||saved);await loadDay();notify(saved.detection?`Kamera gespeichert. ${saved.detection.message}`:'Kamera gespeichert. Aufnahmen werden eingelesen.');
 }catch(e){$('camera-error').textContent=(e as Error).message}
 finally{controls.forEach((c,i)=>c.disabled=disabled[i]);cameraSaving=false;submit.disabled=false;renderCameraList()}
});
on('preview',async()=>{try{const result=await api<{examples:{file:string;time?:string;error?:string;view?:string}[];limited:boolean;detection?:ViewDetection;wideMarker:string;zoomMarker:string}>('/api/cameras/preview',{method:'POST',body:JSON.stringify(cameraInput())});$('preview-result').hidden=false;const status=result.detection?`${result.detection.message}\nKennungen: Weitwinkel ${result.wideMarker}, Zoom ${result.zoomMarker}\n\n`:'';$('preview-result').textContent=status+(result.examples.length?result.examples.map(v=>`${v.file}${v.view?" · "+({wide:"Weitwinkel",zoom:"Zoom",unknown:"Kennung unklar"}[v.view]||v.view):""}\n→ ${v.error||v.time}`).join('\n\n'):'Keine passenden Videodateien gefunden.');if(result.limited)$('preview-result').textContent+='\nSuche auf 10.000 Einträge begrenzt.'}catch(e){$('camera-error').textContent=(e as Error).message}});
async function browseFolder(folder=''){const data=await api<{path:string;folders:string[];samples:string[];limited:boolean}>('/api/cameras/folders?path='+encodeURIComponent(folder));const root=$('folder-browser');root.hidden=false;root.replaceChildren();root.append(element('strong','',data.path==='.'?'Aufnahmeverzeichnis':data.path));if(data.path!=='.'){const back=element('button','text-button','↑ Übergeordneter Ordner');back.type='button';back.onclick=()=>void browseFolder(data.path.split('/').slice(0,-1).join('/')).catch(e=>notify(e.message));root.append(back);const choose=element('button','button secondary','Diesen Ordner verwenden');choose.type='button';choose.onclick=()=>{const folderField=cameraForm.elements.namedItem('folder') as HTMLInputElement,nameField=cameraForm.elements.namedItem('name') as HTMLInputElement,previous=folderField.value.split('/').pop();folderField.value=data.path;if(!nameField.value||nameField.value===previous)nameField.value=data.path.split('/').pop()||'';cameraForm.dispatchEvent(new Event('input',{bubbles:true}));root.hidden=true};root.append(choose)}for(const folder of data.folders){const b=element('button','text-button',folder.split('/').pop());b.type='button';b.onclick=()=>void browseFolder(folder).catch(e=>notify(e.message));root.append(b)}if(data.limited)root.append(element('small','','Suche begrenzt; bitte einen Unterordner wählen.'))}
on('browse-folder',()=>browseFolder());

on('new-camera',()=>{if(cameraSaving||!discardCamera()||!cameraSettings.canChange())return;resetCamera();cameraSettings.clear();showCameraEditor()});
on('camera-back',()=>{if(cameraSettings.canChange())leaveCameraEditor()});on('camera-cancel',()=>{if(cameraSettings.canChange())leaveCameraEditor()});
function closeCamera(){navigate('recordings')}
on('close-dialog',closeCamera);
registerPage('recordings',{element:$('recordings'),title:'Aufnahmen',enter:()=>scheduleResize()});
registerPage('admin/cameras',{element:dialog,title:'Kameras',admin:true,enter:()=>{void loadScanHistory();void loadCameraLiveStatus();cameraSettings.show('general')},leave:next=>{if(cameraSaving)return false;if(next==='admin/cameras/live')return true;if(next==='admin/cameras/batch')return true;if(!discardCamera()||!cameraSettings.leave())return false;resetCamera();dialog.classList.remove('camera-editing');return true}});
on('add-first',()=>{navigate('admin/cameras');resetCamera();showCameraEditor()});
on('mobile-nav-toggle',()=>{const expanded=document.documentElement.classList.toggle('mobile-nav-open');$('mobile-nav-toggle').setAttribute('aria-expanded',String(expanded))});
document.addEventListener('pageerror',e=>notify((e as CustomEvent<string>).detail));
on('play',()=>playing?pause():play());on('skip-back',()=>{const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,-1);if(t!==undefined)seek(t)});on('skip-forward',()=>{const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,1);if(t!==undefined)seek(t)});on('zoom-in',()=>zoom(.5));on('zoom-out',()=>zoom(2));on('reset-zoom',()=>{viewStart=from;viewEnd=to;draw();refreshRange()});
function changeDate(delta:number){const d=new Date(dateInput.value+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+delta);dateInput.value=d.toISOString().slice(0,10);return loadDay()}
on('prev-day',()=>changeDate(-1));on('next-day',()=>changeDate(1));on('today',()=>{dateInput.value=berlinDate(new Date());return loadDay()});dateInput.addEventListener('change',()=>void loadDay().catch(e=>notify(e.message)));
$<HTMLSelectElement>('layout').addEventListener('change',()=>{page=0;renderCameras();saveView()});$<HTMLSelectElement>('quality').addEventListener('change',()=>{tiles.forEach(t=>{t.release()});update();saveView()});$<HTMLSelectElement>('speed').addEventListener('change',()=>{update();saveView()});
on('rescan',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value})}))});
on('logout',async()=>{if(document.querySelector('[data-dirty="true"]')&&!confirm('Ungespeicherte Änderungen verwerfen und abmelden?'))return;for(const draft of document.querySelectorAll<HTMLElement>('[data-dirty]'))draft.dataset.dirty='false';stopBackground();await api('/api/logout',{method:'POST'});location.reload()});
window.addEventListener('pagehide',stopBackground);window.addEventListener('pageshow',event=>{if(event.persisted&&session){pollingReady=true;if(!document.hidden)scanPoller.start()}});document.addEventListener('visibilitychange',()=>{if(document.hidden){scanPoller.stop();pause();tiles.forEach(t=>t.release())}else if(pollingReady)scanPoller.start()});
const loginForm=$<HTMLFormElement>('login-form');loginForm.addEventListener('submit',async e=>{e.preventDefault();const button=loginForm.querySelector('button')!;button.disabled=true;try{const data=new FormData(loginForm);const res=await fetch('/auth/local',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(data))});const body=await res.json();if(!res.ok)throw new Error(body.error);location.reload()}catch(e){$('login-error').textContent=(e as Error).message}finally{button.disabled=false}});
let managementInitialized=false;
async function boot(){
 pollingReady=false;scanPoller.stop();playbackClock.stop();
 $('app').hidden=true;$('login').hidden=true;
 const response=await fetch('/api/session');
 if(response.status===401){
  const methods=await api<{local:boolean;oidc:boolean}>('/auth/methods');
  loginForm.hidden=!methods.local;$('oidc-login').hidden=!methods.oidc;$('login-divider').hidden=!(methods.local&&methods.oidc);$('boot-error').hidden=true;$('login').hidden=false;
  // Focus synchronously after revealing the form. Native autofocus on a hidden
  // input can run later in WebKit and steal focus from password entry.
  if(methods.local)loginForm.querySelector<HTMLInputElement>('[name=username]')!.focus();
  return;
 }
 if(!response.ok)throw new Error(`Sitzung konnte nicht geladen werden (${response.status})`);
 session=await response.json();$('password-button').hidden=false;$('password-button').textContent='Mein Konto';accountMenu.open=false;
 if(!managementInitialized){initManagement({api,getSession:()=>session,getCameras:()=>cameras,getCursor:()=>cursor,notify});prepareDialogs();initSettingsLayout();managementInitialized=true}
 $('user-name').textContent=session.name;$('avatar').textContent=session.name.slice(0,2).toUpperCase();document.querySelector('#account-menu summary')!.setAttribute('aria-label','Konto: '+session.name);document.querySelector('#account-menu summary')!.setAttribute('title',session.name);$('settings-button').hidden=session.role!=='admin';$('rescan').hidden=session.role!=='admin';dateInput.value=berlinDate(new Date());await refreshCameras();
 let stored:unknown;try{stored=JSON.parse(localStorage.getItem('cctv-view:'+session.preferenceKey)||'null')}catch{}
 let remote=await api<unknown>('/api/preferences');
 if(remote===null){
  const legacy=validView(stored,cameras.map(c=>c.id));
  if(legacy.originalQualityVersion!==1){legacy.quality='native';legacy.originalQualityVersion=1}
  let theme='system';try{theme=localStorage.getItem('cctv-theme')||'system'}catch{}
  const initial={...defaultView(),cameras:[...selected],order:[...order],...legacy,theme:legacy.theme||(['system','light','dark'].includes(theme)?theme:'system')};
  try{await api('/api/preferences',{method:'PATCH',headers:{'If-None-Match':'*'},body:JSON.stringify(initial)});remote=initial}
  catch(error){if((error as {status?:number}).status!==412)throw error;remote=await api('/api/preferences')}
 }
 const saved=validView(remote,cameras.map(c=>c.id));
 for(const id of ['layout','quality','speed'] as const)$<HTMLSelectElement>(id).value=saved[id]||defaultView()[id];

 delete state.railCollapsed;delete state.hotspot;
 Object.assign(state,defaultView(),saved);state.theme=saved.theme||'system';window.dispatchEvent(new CustomEvent('themecommit',{detail:state.theme}));applyRail();hotspot=saved.hotspot;if(saved.cameras)selected=new Set([...saved.cameras,...(saved.order?cameras.filter(c=>!saved.order!.includes(c.id)).map(c=>c.id):[])]);if(saved.order)order=[...saved.order,...order.filter(id=>!saved.order!.includes(id))];for(const id of ['preload','aspect'] as const)$<HTMLSelectElement>(id).value=state[id];$<HTMLInputElement>('grid-columns').value=String(state.columns);$<HTMLInputElement>('grid-rows').value=String(state.rows);$<HTMLInputElement>('auto-hotspot').checked=state.autoHotspot;applyPreferences();setTimelineHeight(state.timelineHeight);renderCameras();await loadDay();
 const initialSnapshot=preferenceSnapshot();
 // Remember the server's known cameras so the next edit also saves newly
 // discovered cameras and their order. Otherwise a deselected new camera
 // would be mistaken for a new addition again after every reload.
 preferenceWriter=new PreferenceWriter({...initialSnapshot,cameras:saved.cameras??initialSnapshot.cameras,order:saved.order??initialSnapshot.order},changes=>api('/api/preferences',{method:'PATCH',body:JSON.stringify(changes),keepalive:true}),preferenceStatus);
 try{localStorage.setItem('cctv-view:'+session.preferenceKey,JSON.stringify(preferenceSnapshot()))}catch{}
 refreshRange=attachTimelineControls(canvas,{bounds:()=>({from,to,cursor,viewStart,viewEnd}),seek,zoom,shortcuts:()=>state.shortcuts,seekStep:()=>state.seekStep,pan:(start,end)=>{viewStart=start;viewEnd=end;draw();refreshRange()}});
 $('boot-error').hidden=true;$('app').hidden=false;startNavigation(()=>session.role==='admin',pause);pollingReady=true;if(!document.hidden)scanPoller.start();
}
function showBootError(error:unknown){$('boot-error-message').textContent=(error as Error).message||'Unbekannter Fehler';$('boot-error').hidden=false}
async function runBoot(){const button=$<HTMLButtonElement>('boot-retry');button.disabled=true;if(!$('boot-error').hidden)$('boot-error-message').textContent='Verbindung wird hergestellt …';try{await boot()}catch(error){showBootError(error)}finally{button.disabled=false}}
$('boot-retry').addEventListener('click',()=>void runBoot());
void runBoot();

const preferences=$('preferences-dialog'),preferencesForm=$<HTMLFormElement>('preferences-form');
let preferencesBaseline='',resetPreferencesPending=false,preferencesSaving=false;
const preferenceChecks=['showNames','showTimestamps','showCodec','showOverview','showThumbnails','shortcuts'] as const;
function preferencesSnapshot(){return JSON.stringify(Array.from(preferencesForm.querySelectorAll<HTMLInputElement|HTMLSelectElement>('input,select')).map(e=>[e.id,e.type==='checkbox'?(e as HTMLInputElement).checked:e.value]))}
function updatePreferencesSave(){const dirty=resetPreferencesPending||preferencesSnapshot()!==preferencesBaseline;$<HTMLButtonElement>('preferences-save').disabled=!dirty;preferencesForm.dataset.dirty=String(dirty)}
function syncPreferences(value:ViewState=state){
 for(const key of preferenceChecks)$<HTMLInputElement>('pref-'+key).checked=value[key];
 $<HTMLSelectElement>('pref-density').value=value.density;$<HTMLSelectElement>('pref-seekStep').value=String(value.seekStep);$<HTMLInputElement>('pref-timelineHeight').value=String(value.timelineHeight);
 for(const id of ['preload','aspect'] as const)$<HTMLSelectElement>(id).value=value[id];
 $<HTMLInputElement>('grid-columns').value=String(value.columns);$<HTMLInputElement>('grid-rows').value=String(value.rows);$<HTMLInputElement>('auto-hotspot').checked=value.autoHotspot;
 $<HTMLSelectElement>('theme').value=$<HTMLSelectElement>('login-theme').value;
}
function applyPreferences(){
 document.documentElement.dataset.density=state.density;
 for(const key of ['showNames','showTimestamps','showCodec'] as const)document.body.classList.toggle('hide-'+key,!state[key]);
 $('overview').hidden=!state.showOverview;$('thumbnails').hidden=!state.showThumbnails;if(preferences.hidden)syncPreferences();scheduleResize();
}
function openPreferences(){if(preferencesForm.dataset.draftActive==='true')return;preferencesForm.dataset.draftActive='true';syncPreferences();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();preferencesForm.dataset.dirty='false'}
function leavePreferences(){if(preferencesSaving)return false;if((resetPreferencesPending||preferencesSnapshot()!==preferencesBaseline)&&!confirm('Ungespeicherte Einstellungen verwerfen?'))return false;preferencesForm.dataset.draftActive='false';syncPreferences();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();return true}
function closePreferences(){navigate('recordings')}
for(const tab of ['appearance','playback'])registerPage('settings/'+tab,{element:preferences,group:'preferences',title:tab==='appearance'?'Darstellung':'Wiedergabe',leave:leavePreferences,enter:()=>{openPreferences();$('preferences-title').textContent=tab==='appearance'?'Darstellung':'Wiedergabe';for(const section of preferences.querySelectorAll<HTMLElement>('[data-preference-page]'))section.hidden=section.dataset.preferencePage!==tab}});
const diagnostics=$<HTMLDialogElement>('diagnostics-dialog');
on('diagnostics-open',()=>diagnostics.showModal());on('diagnostics-close',()=>diagnostics.close());
on('preferences-close',closePreferences);on('preferences-cancel',closePreferences);
preferences.addEventListener('cancel',e=>{e.preventDefault();closePreferences()});
preferencesForm.addEventListener('input',updatePreferencesSave);preferencesForm.addEventListener('change',updatePreferencesSave);
preferencesForm.noValidate=true;
preferencesForm.addEventListener('submit',async e=>{
 e.preventDefault();const invalid=preferencesForm.querySelector<HTMLInputElement>(':invalid');if(invalid){const tab=invalid.closest<HTMLElement>('[data-preference-page]')?.dataset.preferencePage;if(tab)navigate('settings/'+tab);preferencesForm.reportValidity();return;}
 const theme=$<HTMLSelectElement>('theme').value;
 if(resetPreferencesPending){Object.assign(state,defaultView());delete state.railCollapsed;selected=new Set(cameras.map(c=>c.id));order=cameras.map(c=>c.id);hotspot=undefined;page=0;for(const id of ['layout','quality','speed'] as const)$<HTMLSelectElement>(id).value=state[id]}
 for(const key of preferenceChecks)state[key]=$<HTMLInputElement>('pref-'+key).checked;
 state.density=$<HTMLSelectElement>('pref-density').value as ViewState['density'];state.seekStep=Number($<HTMLSelectElement>('pref-seekStep').value);
 for(const id of ['preload','aspect'] as const)state[id]=$<HTMLSelectElement>(id).value;
 state.columns=Number($<HTMLInputElement>('grid-columns').value);state.rows=Number($<HTMLInputElement>('grid-rows').value);state.autoHotspot=$<HTMLInputElement>('auto-hotspot').checked;
 setTimelineHeight(Number($<HTMLInputElement>('pref-timelineHeight').value));
 state.theme=theme as ViewState['theme'];window.dispatchEvent(new CustomEvent('themecommit',{detail:theme}));
 applyRail();applyPreferences();if(resetPreferencesPending)tiles.forEach(t=>t.release());renderCameras();const saving=saveView();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();preferencesSaving=true;try{await saving;preferencesSaving=false;if(preferencesSnapshot()===preferencesBaseline){preferencesForm.dataset.dirty='false';navigate('recordings');notify('Persönliche Einstellungen gespeichert.')}else notify('Einstellungen gespeichert. Weitere Änderungen sind noch nicht gespeichert.')}catch{}finally{preferencesSaving=false}
});
on('preferences-reset',()=>{
 if(!confirm('Persönliche Einstellungen und Kameraauswahl auf die Standardwerte zurücksetzen? Erst Speichern übernimmt die Änderung.'))return;
 syncPreferences(defaultView());$<HTMLSelectElement>('theme').value='system';resetPreferencesPending=true;updatePreferencesSave();
});
on('select-all',()=>{selected=new Set(cameras.map(c=>c.id));page=0;renderCameras();saveView()});
on('select-none',()=>{selected.clear();page=0;renderCameras();saveView()});
on('page-prev',()=>{page--;renderCameras()});on('page-next',()=>{page++;renderCameras()});
on('scan-all',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value,all:true})}))});
on('rebuild-index',async()=>{if(!confirm('Den abgeleiteten Index und Vorschaubilder neu aufbauen? Kameras und Originalvideos bleiben erhalten.'))return;pause();tiles.forEach(t=>t.release());await api('/api/index/rebuild',{method:'POST'});records=[];await loadDay();notify('Index geleert. Der geöffnete Tag wird erneut eingelesen.')});
overview.onpointerdown=e=>{overview.setPointerCapture(e.pointerId);seek(from+clamp((e.clientX-overview.getBoundingClientRect().left)/overview.clientWidth,0,1)*(to-from));const span=viewEnd-viewStart;viewStart=clamp(cursor-span/2,from,to-span);viewEnd=viewStart+span;draw()};
overview.onpointermove=e=>{if(e.buttons===1){const span=viewEnd-viewStart,start=clamp(from+(e.clientX-overview.getBoundingClientRect().left)/overview.clientWidth*(to-from)-span/2,from,to-span);viewStart=start;viewEnd=start+span;draw()}};
overview.onkeydown=e=>{if(state.shortcuts&&['ArrowLeft','ArrowRight'].includes(e.key)){e.preventDefault();seek(cursor+(e.key==='ArrowRight'?1:-1)*(e.shiftKey?60:state.seekStep))}};
function setTimelineHeight(height:number){state.timelineHeight=Math.round(clamp(height,100,650));$('timeline-viewport').style.height=state.timelineHeight+'px';$<HTMLInputElement>('pref-timelineHeight').value=String(state.timelineHeight);resize.setAttribute('aria-valuenow',String(state.timelineHeight));resize.setAttribute('aria-valuetext',`${state.timelineHeight} Pixel`);draw()}
let resizeStart=0,resizeHeight=0;const resize=$('timeline-resize');resize.onpointerdown=e=>{resize.setPointerCapture(e.pointerId);resizeStart=e.clientY;resizeHeight=state.timelineHeight};resize.onpointermove=e=>{if(!resize.hasPointerCapture(e.pointerId))return;setTimelineHeight(resizeHeight+e.clientY-resizeStart)};resize.onpointerup=()=>saveView();resize.onkeydown=e=>{if(['ArrowUp','ArrowDown'].includes(e.key)){e.preventDefault();setTimelineHeight(state.timelineHeight+(e.key==='ArrowDown'?20:-20));saveView()}};
let observedGridWidth=-1;
new ResizeObserver(([entry])=>{const width=entry.contentRect.width;if(width===observedGridWidth)return;observedGridWidth=width;scheduleResize(true)}).observe($('video-grid'));
initSearchPage({
 cameras:()=>cameras,request:(url,signal)=>api<SearchPage>(url,{signal}),notify,
 open:async r=>{
  navigate('recordings');selected.add(r.cameraId);dateInput.value=berlinDate(new Date(r.start*1000));page=0;renderCameras();await loadDay();seek(r.start);
  const cameraIndex=ordered().findIndex(c=>c.id===r.cameraId);if(cameraIndex>=0)page=Math.floor(cameraIndex/visiblePageCapacity());renderCameras();saveView();
 }
});
on('trace-stop',()=>{tracing=false;$<HTMLInputElement>('trace-enabled').checked=false;$<HTMLButtonElement>('trace-stop').disabled=true;$<HTMLButtonElement>('trace-download').disabled=trace.length===0;$('trace-status').textContent='Gestoppt · '+trace.length+' Einträge'});
$<HTMLInputElement>('trace-enabled').onchange=()=>{tracing=$<HTMLInputElement>('trace-enabled').checked;if(tracing){trace=[];traceStart=performance.now();log('start')}else log('stop');$<HTMLButtonElement>('trace-stop').disabled=!tracing;$<HTMLButtonElement>('trace-download').disabled=tracing||trace.length===0;$('trace-status').textContent=tracing?'Aufzeichnung läuft (maximal 20.000 Einträge)':'Gestoppt'};
on('trace-download',async()=>{const status=await api('/api/playback-status');const blob=new Blob([JSON.stringify({version:1,status,events:trace},null,2)],{type:'application/json'});const url=URL.createObjectURL(blob),a=element('a');a.href=url;a.download='wiedergabediagnose.json';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000)});

document.addEventListener('keydown',e=>{
 if(e.key==='Escape'&&document.fullscreenElement)void document.exitFullscreen().catch(()=>notify('Vollbild konnte nicht geschlossen werden.'));
 if(e.key==='Escape'&&!document.querySelector('dialog[open]'))document.querySelectorAll('.camera-tile.expanded').forEach(t=>{t.classList.remove('expanded');const button=t.querySelector('.tile-expand');if(button){button.setAttribute('aria-label','Ansicht vergrößern');setControlIcon(button,'expand')}});
 const target=e.target instanceof HTMLElement?e.target:undefined;
 const tag=target?.tagName;
 if(tag==='INPUT'||tag==='SELECT'||tag==='TEXTAREA'||tag==='CANVAS'||target?.isContentEditable)return;
 if(target?.closest('button,a,summary,[role="button"],[role="link"]')&&!(e.key.toLowerCase()==='f'&&target.closest('.camera-tile')))return;
 if(currentPage()!=='recordings'||e.defaultPrevented||e.ctrlKey||e.metaKey||e.altKey||document.querySelector('dialog[open]')||!state.shortcuts||!session)return;
 if(e.key===' '||e.key==='Spacebar'||e.code==='Space'||e.key==='k'||e.key==='K'){
  e.preventDefault();
  if(playing)pause();else play();
 }else if(e.key==='ArrowLeft'){
  e.preventDefault();
  seek(cursor-(e.shiftKey?60:state.seekStep));
 }else if(e.key==='ArrowRight'){
  e.preventDefault();
  seek(cursor+(e.shiftKey?60:state.seekStep));
 }else if(e.key==='j'||e.key==='J'){
  e.preventDefault();
  seek(cursor-10);
 }else if(e.key==='l'||e.key==='L'){
  e.preventDefault();
  seek(cursor+10);
 }else if(e.key==='f'||e.key==='F'){
  e.preventDefault();
  const primary=document.fullscreenElement||target?.closest('.camera-tile')||document.querySelector('.camera-group.hotspot-primary .camera-tile')||document.querySelector('.camera-tile');
  if(primary instanceof HTMLElement)void toggleFullscreen(primary);
 }
});

const batchSelection=new Map<string,string>();
function renderBatchSelection(){$('batch-dialog').dataset.dirty=String(batchSelection.size>0);const root=$('batch-selected');root.replaceChildren();for(const [folder,name] of batchSelection){const row=element('label','batch-row',folder),input=element('input');input.value=name;input.maxLength=100;input.setAttribute('aria-label','Anzeigename für '+folder);input.oninput=()=>batchSelection.set(folder,input.value);const remove=element('button','text-button','Entfernen');remove.type='button';remove.onclick=()=>{batchSelection.delete(folder);renderBatchSelection();void browseBatch(batchFolder)};row.append(input,remove);root.append(row)}$<HTMLButtonElement>('batch-save').disabled=batchSelection.size===0}
let batchFolder='';
async function browseBatch(folder=''){batchFolder=folder;const data=await api<{path:string;folders:string[];limited:boolean}>('/api/cameras/folders?path='+encodeURIComponent(folder));const root=$('batch-browser');root.replaceChildren();root.append(element('strong','',data.path==='.'?'Aufnahmeverzeichnis':data.path));if(data.path!=='.'){const back=element('button','text-button','↑ Übergeordneter Ordner');back.type='button';back.onclick=()=>void browseBatch(data.path.split('/').slice(0,-1).join('/')).catch(e=>notify(e.message));root.append(back)}for(const path of data.folders){const row=element('div','batch-row'),check=element('input');check.type='checkbox';check.checked=batchSelection.has(path);check.disabled=cameras.some(c=>c.folder===path);check.setAttribute('aria-label','Kameraordner '+path+' auswählen');check.onchange=()=>{if(check.checked)batchSelection.set(path,path.split('/').pop()||path);else batchSelection.delete(path);renderBatchSelection()};const open=element('button','text-button',path.split('/').pop());open.type='button';open.onclick=()=>void browseBatch(path).catch(e=>notify(e.message));row.append(check,open);if(check.disabled)row.append(element('small','','Bereits eingerichtet'));root.append(row)}if(data.limited)root.append(element('small','','Suche begrenzt; bitte einen Unterordner öffnen.'))}
let batchSaving=false;
registerPage('admin/cameras/batch',{element:$('batch-dialog'),title:'Mehrere Kameras hinzufügen',admin:true,enter:async()=>{batchSelection.clear();$('batch-error').textContent='';renderBatchSelection();try{await browseBatch()}catch(e){$('batch-error').textContent=(e as Error).message}},leave:next=>{if(batchSaving)return false;if(batchSelection.size&&!confirm('Ausgewählte Kameraordner verwerfen?'))return false;if(next!=='admin/cameras'&&!discardCamera())return false;batchSelection.clear();$('batch-dialog').dataset.dirty='false';if(next!=='admin/cameras'){resetCamera();dialog.classList.remove('camera-editing')}return true}});
on('batch-open',()=>navigate('admin/cameras/batch'));
on('batch-close',()=>navigate('admin/cameras'));
on('batch-save',async()=>{if(batchSaving)return;batchSaving=true;const button=$<HTMLButtonElement>('batch-save');button.disabled=true;try{const settings=cameraInput();const added=await api<(Camera&{detection?:ViewDetection})[]>('/api/cameras/batch',{method:'POST',body:JSON.stringify({date:dateInput.value,cameras:[...batchSelection].map(([folder,name])=>({...settings,id:'',folder,name}))})});batchSelection.clear();$('batch-dialog').dataset.dirty='false';batchSaving=false;resetCamera();dialog.classList.remove('camera-editing');navigate('recordings');await refreshCameras();await loadDay();const dual=added.filter(c=>c.viewMode==='dual').length,swapped=added.filter(c=>c.detection?.status==='swapped').length,unclear=added.filter(c=>c.detection?.status==='unclear').length;notify(dual?`Kameras hinzugefügt. ${swapped} Zuordnungen korrigiert${unclear?`, ${unclear} nicht sicher bestimmbar`:''}.`:'Kameras hinzugefügt. Bis zu drei Kameras werden gleichzeitig indexiert.')}catch(e){$('batch-error').textContent=(e as Error).message}finally{batchSaving=false;button.disabled=false}});

window.addEventListener('themechange',()=>{timelineView.invalidate();draw()});

document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!e.defaultPrevented&&!(e.target instanceof Element&&e.target.closest('dialog'))&&!document.querySelector('dialog[open]')&&currentPage()!=='recordings'){e.preventDefault();navigate('recordings')}});

initSettingsLayout();
