import {initCameraSettings} from './camera-settings';
import {initLivePage} from './live-page';
import {initLiveAdmin} from './live-admin';
import {confirmAction} from './confirm-dialog';
import {notify,notifyError,initToast} from './toast';
import {requestJSON} from './api-client';
import {registerPage,navigate,startNavigation,currentPage} from './navigation';
import {PreferenceWriter} from './preferences';
import {Tile,setControlIcon,type TileContext} from './playback-tile';
import {createPlaybackClock} from './playback-clock';
import {createPoller} from './poller';
import {validView,gridSize,pageCapacity,hotspotCandidate,recordingBoundary,moveCamera,defaultView,type ViewState} from './view-state';
import {attachTimelineControls} from './timeline-controls';
import {recordingAt,nextRecording,playbackRecordings,displayedRecordings,firstRecordingStart,clamp,berlinDate,clock,timelineRowAt,keyboardSeekTarget,type Recording} from './timeline';
import {initSearchPage,type SearchPage,type SearchResult} from './search-page';
import {initCameraEditor} from './camera-editor';
import {createTimelineView} from './timeline-view';
import {initCameraList} from './camera-list';
import {initCameraBatch} from './camera-batch';
import {initPreferencesDialog} from './preferences-dialog';
import {collapseTile,expandedTile,toggleEnlarged} from './expanded-tile';
import {wrapChipWithMoveButtons,createReorderToggle} from './chip-order';
import {initShortcutHelp} from './shortcuts';
import {popoverMenu} from './popover-menu';
import type {Camera,ViewDetection,Scan,Day} from './types';
interface Session {preferenceKey:string;name:string;role:'admin'|'viewer';csrf:string;kind:'local'|'oidc'}
const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
let session:Session, cameras:Camera[]=[],records:Recording[]=[],playableRecords:Recording[]=[],displayedRecords:Recording[]=[],selected=new Set<string>(),from=0,to=0,cursor=0,viewStart=0,viewEnd=0,playing=false,scanRunning=false,loadGeneration=0,dayLoadPending=false;
const tiles=new Map<string,Tile>();
const openZoom=new Set<string>();
let audioTile:Tile|undefined;
const dateInput=$<HTMLInputElement>('date'),canvas=$<HTMLCanvasElement>('timeline'),overview=$<HTMLCanvasElement>('overview');
const timelineView=createTimelineView(canvas,overview);
const speed=()=>Number($<HTMLSelectElement>('speed').value);
let refreshRange=()=>{};
let reorderMode=false,page=0,order:string[]=[],hotspot:string|undefined,lastHotspotCursor:number|undefined;
// A completion observed during a day request may be newer than its recording snapshot.
let scanCompletion=0,loadedScanCompletion=0;
 const state:ViewState=defaultView();

let fullscreenGridPending=false;
async function toggleFullscreen(tile:HTMLElement){
 try{await toggleEnlarged(tile,tile.querySelector<HTMLElement>('.tile-expand')||undefined)}
 catch{notify('Vollbild konnte nicht geschlossen werden. Bitte erneut versuchen.')}
}
// Keeps the expand buttons of the overlay fallback in sync with the expanded tile.
document.addEventListener('tileexpandchange',()=>{
 for(const tile of tiles.values())tile.wake();
 for(const button of document.querySelectorAll<HTMLElement>('#video-grid .tile-expand')){
  const open=button.closest('.camera-tile')===expandedTile();
  button.setAttribute('aria-label',open?'Vergrößerte Ansicht schließen':'Ansicht vergrößern');
  setControlIcon(button,open?'collapse':'expand')
 }
});
document.addEventListener('fullscreenchange',()=>{
 if(!document.fullscreenElement&&fullscreenGridPending){fullscreenGridPending=false;renderGrid();update()}
 for(const tile of tiles.values()){
  const active=document.fullscreenElement===tile.container;
  const button=tile.container.querySelector('.tile-expand');
  if(button){
   button.setAttribute('aria-label',active?'Vollbild schließen':document.fullscreenEnabled?'Vollbild öffnen':'Ansicht vergrößern');
   setControlIcon(button,active?'collapse':'expand')
  }
  tile.applyAspect();
  tile.wake()
 }
});
// Without an explicit choice (.rail-auto) the stylesheet collapses the rail by media query,
// so the layout never lags behind a resize; the classes only carry an explicit choice.
function railCollapsed(){return state.railCollapsed??matchMedia('(max-width:1100px)').matches}
function applyRail(){
 const collapsed=railCollapsed(),root=document.documentElement.classList;
 root.toggle('rail-auto',state.railCollapsed==null);
 root.toggle('rail-collapsed',state.railCollapsed===true);
 $('rail-toggle').setAttribute('aria-expanded',String(!collapsed));
 $('rail-toggle').setAttribute('aria-label',collapsed?'Seitenleiste ausklappen':'Seitenleiste einklappen');
 scheduleResize();
}
on('rail-toggle',()=>{state.railCollapsed=!railCollapsed();applyRail();saveView()});
matchMedia('(max-width:1100px)').addEventListener('change',applyRail);
const accountMenu=$<HTMLDetailsElement>('account-menu');
accountMenu.querySelector('summary')!.addEventListener('click',e=>{if($('password-button').hidden)e.preventDefault()});
popoverMenu(accountMenu);
popoverMenu($<HTMLDetailsElement>('selection-menu'));
$('logout').addEventListener('click',()=>{accountMenu.open=false});

let preferenceWriter:PreferenceWriter|undefined;
function preferenceSnapshot():Record<string,unknown>{
 state.cameras=[...selected];state.order=order;state.hotspot=hotspot;
 for(const id of ['layout','quality','speed'] as const)state[id]=$<HTMLSelectElement>(id).value;
 // An empty hotspot clears the stored selection without JSON null semantics.
 return {...state,railCollapsed:state.railCollapsed??null,hotspot:hotspot||'',theme:state.theme||'system'};
}
function preferenceStatus(saving:boolean,error?:unknown){
 $('preference-status').textContent=saving?'Einstellungen werden gespeichert …':error?'Einstellungen nicht gespeichert. Bitte erneut versuchen.':'';
 $('preferences-retry').hidden=!error;
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

initToast();
// Skip link: moves focus past the navigation to the heading of the visible page.
$('skip-link').addEventListener('click',()=>{
 const main=[...document.querySelectorAll<HTMLElement>('main,[role=main]')].find(node=>!node.hidden&&node.offsetParent!==null);if(!main)return;
 const target=main.querySelector<HTMLElement>('h1,h2')||main;target.tabIndex=-1;target.focus();target.scrollIntoView({block:'start'});
});
// Every request carries the session's CSRF token; an expired sign-in reloads to the login.
function api<T>(url:string,options:RequestInit={}):Promise<T>{
 return requestJSON<T>(url,options,{csrf:session?.csrf,onUnauthorized:()=>{livePage.stop();pause();location.reload()}});
}
function on(id:string,fn:()=>void|Promise<void>){$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>notifyError(e.message))})}
function element<K extends keyof HTMLElementTagNameMap>(tag:K,className='',text=''):HTMLElementTagNameMap[K]{const e=document.createElement(tag);e.className=className;e.textContent=text;return e}
function ordered(){return order.map(id=>cameras.find(c=>c.id===id)).filter((c):c is Camera=>!!c&&selected.has(c.id))}
function grid(){
 return gridSize($<HTMLSelectElement>('layout').value,state.columns,state.rows,selected.size,$('video-grid').clientWidth||innerWidth,ordered().some(c=>c.viewMode==='dual'&&openZoom.has(c.id)))
}
function visiblePageCapacity(){const [cols,rows]=grid();return pageCapacity($<HTMLSelectElement>('layout').value,cols,rows)}
function visible():Camera[]{const all=ordered(),capacity=visiblePageCapacity();page=clamp(page,0,Math.max(0,Math.ceil(all.length/capacity)-1));return all.slice(page*capacity,(page+1)*capacity)}
function pause(){playing=false;playbackClock.stop();$('play').textContent='▶';$('play').setAttribute('aria-label','Abspielen');tiles.forEach(t=>{t.paused();t.setPlaying(false)});renderPosition()}
function play(){
 if(!playableRecords.length||!visible().length){
  notify('Für diese Auswahl sind keine Aufnahmen vorhanden.');
  return
 }
 if(!visible().some(c=>recordingAt(displayedRecords,c.id,cursor))){
  const next=nextRecording(displayedRecords,visible().map(c=>c.id),cursor);
  if(next===undefined){
   notify('Keine weiteren Aufnahmen an diesem Tag.');
   return
  }
  cursor=next
 }
 playing=true;
 $('play').textContent='Ⅱ';
 $('play').setAttribute('aria-label','Pause');
 tiles.forEach(t=>{
  t.retry();
  t.setPlaying(true)
 });
 update();
 if(!document.hidden&&currentPage()==='recordings')playbackClock.start()
}
function seek(time:number){playbackClock.reset();lastHotspotCursor=undefined;cursor=clamp(time,from,to-.001);update();updateSliders(true)}
const tileContext:TileContext={
 state,cursor:()=>cursor,playing:()=>playing,speed,quality:()=>$<HTMLSelectElement>('quality').value,
 recordings:()=>records,playableRecordings:()=>playableRecords,audioTile:()=>audioTile,setAudioTile:tile=>{audioTile=tile},
 selectHotspot:id=>{hotspot=id;state.autoHotspot=false;$<HTMLInputElement>('auto-hotspot').checked=false;renderGrid();saveView()},
 toggleFullscreen,notify,request:api,
 togglePlay:()=>{if(playing)pause();else play()},seekBy:seconds=>seek(cursor+seconds)
};
function synchronizeTiles(){
 if(!from)return;
 if(state.autoHotspot&&$<HTMLSelectElement>('layout').value==='hotspot'){
  const candidate=hotspotCandidate(displayedRecords,visible().map(c=>c.id),cursor,playing&&lastHotspotCursor!==undefined&&cursor>=lastHotspotCursor?lastHotspotCursor:undefined);
  if(candidate&&candidate!==hotspot){hotspot=candidate;renderGrid()}
 }
 lastHotspotCursor=cursor;const timestamp=clock(cursor);tiles.forEach(t=>t.update(timestamp));
}
// The timeline canvases are sliders. Their value is not updated while playing,
// otherwise screen readers would announce every second of a focused slider.
let sliderText='';
function updateSliders(force=false){
 if(!from||playing&&!force)return;
 const text=clock(cursor)+' Uhr';
 if(text===sliderText&&!force)return;
 sliderText=text;
 for(const slider of [canvas,overview]){
  slider.setAttribute('aria-valuemin',String(Math.floor(from)));
  slider.setAttribute('aria-valuemax',String(Math.ceil(to)));
  slider.setAttribute('aria-valuenow',String(Math.round(cursor)));
  slider.setAttribute('aria-valuetext',text)
 }
}
function renderPosition(){if(!from)return;const timestamp=clock(cursor);if($('clock').textContent!==timestamp)$('clock').textContent=timestamp;draw();refreshRange();updateSliders()}
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
   if(currentPage()==='admin/cameras'){await loadScanHistory();void loadStorage()}
  }
 },onError:error=>notifyError((error as Error).message)
});
let pollingReady=false;
function stopBackground(){pollingReady=false;scanPoller.stop();pause();tiles.forEach(t=>t.release())}
function refreshDisplayedRecords(){displayedRecords=displayedRecordings(playableRecords,cameras,openZoom)}
function renderGrid(){
 // Detaching the native fullscreen element ends fullscreen, including during viewport changes.
 if(document.fullscreenElement&&$('video-grid').contains(document.fullscreenElement)){fullscreenGridPending=true;return}
 const focused=document.activeElement instanceof HTMLElement&&$('video-grid').contains(document.activeElement)?document.activeElement:undefined;
 refreshDisplayedRecords();
 const active=visible();
 for(const[id,t]of tiles)if(!active.some(c=>c.id===t.camera.id&& (c.viewMode==='dual'?(t.view==='wide'||t.view==='zoom'&&openZoom.has(c.id)):!t.view))){
  t.destroy();
  tiles.delete(id)
 }
 const gridElement=$('video-grid'),[cols]=grid();
 gridElement.className='video-grid';
 gridElement.style.gridTemplateColumns=`repeat(${cols},minmax(0,1fr))`;
 gridElement.replaceChildren();
 const isHotspot=$<HTMLSelectElement>('layout').value==='hotspot';
 $('video-grid').classList.toggle('hotspot-layout',isHotspot);
 if(!active.some(c=>c.id===hotspot))hotspot=active[0]?.id;
 for(const c of active){
  const group=element('div','camera-group');
  group.id='camera-group-'+c.id;
  group.classList.toggle('dual',c.viewMode==='dual');
  group.classList.toggle('zoom-open',openZoom.has(c.id));
  group.classList.toggle('hotspot-primary',isHotspot&&c.id===hotspot&&cols>1&&grid()[1]>1);
  let zoomToggle:HTMLButtonElement|undefined;
  if(c.viewMode==='dual' && c.supportsZoom !== false){
   const toggle=element('button','zoom-toggle',openZoom.has(c.id)?'Zoom schließen':'Zoom anzeigen');
   toggle.type='button';
   toggle.id='zoom-toggle-'+c.id;
   toggle.setAttribute('aria-label',`${c.name}: ${toggle.textContent}`);
   toggle.setAttribute('aria-expanded',String(openZoom.has(c.id)));
   toggle.setAttribute('aria-controls',group.id);
   toggle.onclick=async()=>{
    if(document.fullscreenElement)await document.exitFullscreen();
    if(openZoom.has(c.id))openZoom.delete(c.id);else openZoom.add(c.id);
    page=Math.floor(ordered().findIndex(camera=>camera.id===c.id)/visiblePageCapacity());
    renderGrid();
    update();
    $(toggle.id).focus()
   };
   toggle.ondblclick=e=>e.stopPropagation();
   zoomToggle=toggle;
  }
  for(const view of c.viewMode==='dual'?(c.supportsZoom===false?['wide']:(openZoom.has(c.id)?['wide','zoom']:['wide'])):[undefined]){
   const key=c.id+':'+(view||'single');
   if(!tiles.has(key))tiles.set(key,new Tile(c,view,tileContext));
   const tile=tiles.get(key)!;
   tile.camera=c;
   tile.container.querySelector('.zoom-toggle')?.remove();
   if(view==='wide'&&zoomToggle)tile.audioButton.before(zoomToggle);
   group.append(tile.container);
   tile.applyAspect();
   tile.video.preload=state.preload as 'metadata'|'auto';
  }
  gridElement.append(group);
  if(isHotspot&&c.id===hotspot)gridElement.prepend(group);
 }
 if(cameras.length>0&&selected.size===0)gridElement.append(element('div','empty-filter-hint','Keine Kameras ausgewählt. Wähle oben mindestens eine Kamera aus, um die Aufnahmen anzuzeigen.'));
 const capacity=visiblePageCapacity();
 document.querySelector('.camera-page-controls')!.classList.toggle('single-page',selected.size<=capacity);
 $('camera-range').textContent=selected.size?`Kameras ${page*capacity+1}–${Math.min((page+1)*capacity,selected.size)} von ${selected.size}`:'Keine Kamera ausgewählt';
 const layout=$<HTMLSelectElement>('layout').value,requested=layout==='custom'?state.columns:Number(layout.split('x')[0]);
 $('grid-hint').textContent=Number.isFinite(requested)&&requested>cols?`Raster an Bildschirm angepasst: ${cols} Spalte${cols===1?'':'n'}`:'';
 $('page-display').textContent=`Seite ${page+1} von ${Math.max(1,Math.ceil(selected.size/capacity))}`;
 $<HTMLButtonElement>('page-prev').disabled=page===0;
 $<HTMLButtonElement>('page-next').disabled=(page+1)*capacity>=selected.size;
 $('custom-grid').hidden=$<HTMLSelectElement>('layout').value!=='custom';
 $('hotspot-control').hidden=!isHotspot;
 // Reattaching a tile blurs its controls even when the same node is reused.
 const restored=focused?.isConnected?focused:focused?.id?document.getElementById(focused.id):null;
 restored?.focus({preventScroll:true});
}
function renderCameras(){
 lastHotspotCursor=undefined;
 playableRecords=playbackRecordings(records,cameras);
 const filters=$('camera-filters');
 filters.replaceChildren();
 for(const id of order){
  const camera=cameras.find(c=>c.id===id);
  if(!camera)continue;
  const b=element('button','filter'+(selected.has(id)?' selected':''),camera.name);
  b.setAttribute('aria-pressed',String(selected.has(id)));
  b.draggable=true;
  b.ondragstart=e=>e.dataTransfer?.setData('text/plain',id);
  b.ondragover=e=>e.preventDefault();
  b.ondrop=e=>{
   e.preventDefault();
   const source=e.dataTransfer?.getData('text/plain');
   if(!source||!order.includes(source))return;
   order=moveCamera(order,source,id);
   renderCameras();
   saveView()
  };
  b.id='recording-filter-'+id;
  b.title='Reihenfolge mit Alt + Pfeil links/rechts ändern';
  b.setAttribute('aria-keyshortcuts','Alt+ArrowLeft Alt+ArrowRight');
  b.onkeydown=e=>{
   if(!e.altKey||!['ArrowLeft','ArrowRight'].includes(e.key))return;
   const target=order.indexOf(id)+(e.key==='ArrowLeft'?-1:1);
   if(target<0||target>=order.length)return;
   e.preventDefault();
   order=moveCamera(order,id,order[target]);
   renderCameras();
   document.getElementById('recording-filter-'+id)?.focus({preventScroll:true});
   saveView()
  };
  b.onclick=()=>{
   if(selected.has(id))selected.delete(id);else selected.add(id);
   page=0;
   renderCameras();
   saveView()
  };
  const index=order.indexOf(id);
  filters.append(reorderMode?wrapChipWithMoveButtons(b,camera.name,index,order.length,delta=>{
   const focusId=`${b.id}-move-${delta<0?'left':'right'}`;
   order=moveCamera(order,id,order[index+delta]);
   renderCameras();
   saveView();
   const next=document.getElementById(focusId) as HTMLButtonElement|null;
   (next&&!next.disabled?next:document.getElementById(b.id))?.focus({preventScroll:true})
  }):b)
 }
 renderGrid();
 $('empty').hidden=cameras.length!==0;
 $('timeline-section').hidden=cameras.length===0;
 $('playback-controls').hidden=cameras.length===0;
 $('add-first').hidden=session.role!=='admin';
 update();
}
function setScan(scan:Scan){
 scanRunning=scan.running;
 scanCompletion=scan.completion??scanCompletion;
 const labels:Record<string,string>={queued:'Wartet',running:'Wird geprüft',complete:'Abgeschlossen',partial:'Teilweise abgeschlossen',failed:'Fehlgeschlagen',cancelled:'Abgebrochen'};
 $('scan-status').textContent=scan.running?`Indexierung · ${scan.completed||0}/${scan.total||0} Kameras · ${scan.indexed} neue/geänderte Videos`:(scan.state==='partial'?'Indexierung teilweise abgeschlossen':scan.state==='cancelled'?'Indexierung abgebrochen':scan.state==='failed'?'Indexierung fehlgeschlagen':'Index bereit');
 $('index-dot').classList.toggle('busy',scan.running);
 for(const id of ['rescan','scan-all','scan-all-quick','rebuild-index'])$<HTMLButtonElement>(id).disabled=scan.running;
 const root=$('scan-cameras'),lines=(scan.cameras||[]).map(c=>`${c.name}: ${labels[c.state]||c.state} · ${c.inspected} geprüft · ${c.added} neu · ${c.changed} geändert · ${c.skipped} übersprungen`);
 // Rebuilding unchanged rows on every poll would reset the reading position of assistive technology.
 if(root.dataset.lines!==lines.join('\n')){root.dataset.lines=lines.join('\n');root.replaceChildren(...lines.map(line=>element('div','scan-camera-status',line)))}
 $('scan-queue').textContent=`${scan.queue?.length||0} Aufträge warten${scan.queue?.length?' · '+scan.queue.map(q=>`${cameras.find(c=>c.id===q.camera)?.name||'Alle Kameras'} (${q.date})`).join(', '):''}`;
 $('warnings').hidden=!scan.warnings?.length;
 $('warnings').textContent=(scan.warnings||[]).join('\n');
}
async function loadDay(reset=true,signal?:AbortSignal){
 // A scan refresh must not supersede an explicit date change or its reset.
 if(!reset&&dayLoadPending)return;
 const generation=reset?++loadGeneration:loadGeneration,date=dateInput.value,completionAtStart=scanCompletion;
 if(reset){dayLoadPending=true;pause();tiles.forEach(t=>t.release())}
 try{
  const data=await api<Day>('/api/day?date='+encodeURIComponent(date)+(reset?'':'&refresh=1'),{signal});
  if(signal?.aborted||generation!==loadGeneration||date!==dateInput.value)return;
  loadedScanCompletion=completionAtStart;records=data.recordings;playableRecords=playbackRecordings(records,cameras);from=data.from;to=data.to;setScan(data.scan);refreshDisplayedRecords();
  if(reset){const ids=new Set(visible().map(c=>c.id));cursor=firstRecordingStart(displayedRecords.filter(r=>ids.has(r.cameraId)),from);viewStart=from;viewEnd=to}
  $('recording-count').textContent=`${records.length} Aufnahmen · ${cameras.length} Kameras`;$('latest-recording').hidden=records.length>0||!cameras.length;update();refreshRange();
 }finally{if(reset&&generation===loadGeneration)dayLoadPending=false}
}
async function refreshCameras(){
 const previous=new Set(cameras.map(c=>c.id));
 cameras=await api<Camera[]>('/api/cameras');
 for(const id of openZoom)if(!cameras.some(c=>c.id===id&&c.viewMode==='dual'))openZoom.delete(id);
 order=[...order.filter(id=>cameras.some(c=>c.id===id)),...cameras.filter(c=>!order.includes(c.id)).map(c=>c.id)];
 for(const c of cameras)if(!previous.has(c.id))selected.add(c.id);
 for(const id of selected)if(!cameras.some(c=>c.id===id))selected.delete(id);
 renderCameras();
 renderCameraList();
 livePage.refresh()
}
// Preview frames are cached in ten-second steps; the start of a recording reuses its regular preview.
function timelinePreview(time:number,y:number){
 if(!state.showThumbnails)return undefined;
 const camera=visible()[timelineRowAt(y,visible().length)],record=camera&&recordingAt(displayedRecords,camera.id,time);
 if(!record||record.kind==='image')return undefined;
 const offset=Math.floor(clamp(time-record.start,0,Math.max(0,record.duration-.001))/10)*10;
 return '/api/recordings/'+encodeURIComponent(record.id)+'/thumbnail'+(offset?'?t='+offset:'');
}
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
const dialog=$('camera-dialog'),cameraForm=$<HTMLFormElement>('camera-form');
let cameraSaving=false;
const cameraList=initCameraList({
 api,notify,cameras:()=>cameras,order:()=>order,setOrder:next=>{order=next},saving:()=>cameraSaving,date:()=>dateInput.value,dialog,form:cameraForm,setScan,
 confirmDiscard:()=>confirmDiscard(),canChange:()=>cameraSettings.canChange(),clearSettings:()=>cameraSettings.clear(),editCamera:camera=>editCamera(camera),showCameraEditor:()=>showCameraEditor(),resetCamera:()=>resetCamera(),
 openLive:id=>liveAdmin.open(id),renderCameras:()=>renderCameras(),saveView:()=>saveView(),refreshCameras:()=>refreshCameras(),loadDay:()=>loadDay()
});
const {renderCameraList,loadScanHistory,loadCameraLiveStatus,loadStorage}=cameraList;
const {cameraInput,discardCamera,confirmDiscard,markCameraSaved,resetCamera,showCameraEditor,editCamera,leaveCameraEditor}=initCameraEditor(cameraForm,dialog,renderCameraList,()=>cameraSaving);
const cameraSettings=initCameraSettings(cameraForm,dialog,liveAdmin,()=>cameras[0]?.id||'',id=>{
 const camera=cameras.find(camera=>camera.id===id);if(!camera||cameraSaving||!discardCamera())return false;editCamera(camera);showCameraEditor();return true;
},()=>!cameraSaving&&discardCamera(),resetCamera);
cameraForm.addEventListener('cameraeditorclosed',()=>cameraSettings.clear());
cameraList.mountSearch();

resetCamera();
cameraForm.addEventListener('submit',async e=>{
 e.preventDefault();
 if(cameraSaving)return;
 cameraSaving=true;
 const submit=cameraForm.querySelector<HTMLButtonElement>('button[type=submit]')!;
 submit.disabled=true;
 renderCameraList();
 const controls=[...cameraForm.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLButtonElement>('input,select,button'),...['close-dialog','new-camera','batch-open'].map(id=>$<HTMLButtonElement>(id))];
 const disabled=controls.map(c=>c.disabled);
 const payload=cameraInput();
 controls.forEach(c=>c.disabled=true);
 try{
  $('camera-error').textContent='';
  const saved=await api<Camera&{detection?:ViewDetection}>('/api/cameras',{method:'POST',body:JSON.stringify(payload)});
  // The POST committed successfully even if refreshing recordings subsequently fails.
  (cameraForm.elements.namedItem('id') as HTMLInputElement).value=saved.id;
  markCameraSaved();
  await refreshCameras();
  editCamera(cameras.find(c=>c.id===saved.id)||saved);
  await loadDay();
  notify(saved.detection?`Kamera gespeichert. ${saved.detection.message}`:'Kamera gespeichert. Aufnahmen werden eingelesen.');
 }catch(e){$('camera-error').textContent=(e as Error).message}
 finally{controls.forEach((c,i)=>c.disabled=disabled[i]);cameraSaving=false;submit.disabled=false;renderCameraList()}
});
on('preview',async()=>{
 try{
  const result=await api<{examples:{file:string;time?:string;error?:string;view?:string}[];limited:boolean;detection?:ViewDetection;wideMarker:string;zoomMarker:string}>('/api/cameras/preview',{method:'POST',body:JSON.stringify(cameraInput())});
  $('preview-result').hidden=false;
  const status=result.detection?`${result.detection.message}\nKennungen: Weitwinkel ${result.wideMarker}, Zoom ${result.zoomMarker}\n\n`:'';
  $('preview-result').textContent=status+(result.examples.length?result.examples.map(v=>`${v.file}${v.view?" · "+({wide:"Weitwinkel",zoom:"Zoom",unknown:"Kennung unklar"}[v.view]||v.view):""}\n→ ${v.error||v.time}`).join('\n\n'):'Keine passenden Videodateien gefunden.');
  if(result.limited)$('preview-result').textContent+='\nSuche auf 10.000 Einträge begrenzt.'
 }catch(e){
  $('camera-error').textContent=(e as Error).message
 }
});

on('new-camera',async()=>{if(cameraSaving||!await confirmDiscard()||!cameraSettings.canChange())return;resetCamera();cameraSettings.clear();showCameraEditor()});
on('camera-back',()=>{if(cameraSettings.canChange())leaveCameraEditor()});
on('camera-cancel',()=>{if(cameraSettings.canChange())leaveCameraEditor()});
function closeCamera(){navigate('recordings')}
on('close-dialog',closeCamera);
registerPage('recordings',{element:$('recordings'),title:'Aufnahmen',enter:()=>scheduleResize()});
registerPage('admin/cameras',{element:dialog,title:'Kameras',admin:true,enter:()=>{
 void loadScanHistory();
 void loadCameraLiveStatus();
 void loadStorage();
 cameraSettings.showSection('list');
 cameraSettings.show('general')
},leave:next=>{
 if(cameraSaving)return false;
 if(next==='admin/cameras/live')return true;
 if(next==='admin/cameras/batch')return true;
 if(!discardCamera()||!cameraSettings.leave())return false;
 resetCamera();
 dialog.classList.remove('camera-editing');
 return true
}});
on('add-first',()=>{navigate('admin/cameras');resetCamera();showCameraEditor()});
on('mobile-nav-toggle',()=>{const expanded=document.documentElement.classList.toggle('mobile-nav-open');$('mobile-nav-toggle').setAttribute('aria-expanded',String(expanded))});
document.addEventListener('pageerror',e=>notify((e as CustomEvent<string>).detail));
on('play',()=>playing?pause():play());
on('skip-back',()=>{
 const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,-1);
 if(t!==undefined)seek(t)
});
on('skip-forward',()=>{
 const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,1);
 if(t!==undefined)seek(t)
});
on('zoom-in',()=>zoom(.5));
on('zoom-out',()=>zoom(2));
on('reset-zoom',()=>{
 viewStart=from;
 viewEnd=to;
 draw();
 refreshRange()
});
function changeDate(delta:number){const d=new Date(dateInput.value+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+delta);dateInput.value=d.toISOString().slice(0,10);return loadDay()}
on('prev-day',()=>changeDate(-1));
on('next-day',()=>changeDate(1));
on('today',()=>{
 dateInput.value=berlinDate(new Date());
 return loadDay()
});
dateInput.addEventListener('change',()=>void loadDay().catch(e=>notifyError(e.message)));
$<HTMLSelectElement>('layout').addEventListener('change',()=>{
 page=0;
 renderCameras();
 saveView()
});
$<HTMLSelectElement>('quality').addEventListener('change',()=>{
 tiles.forEach(t=>{
  t.release()
 });
 update();
 saveView()
});
$<HTMLSelectElement>('speed').addEventListener('change',()=>{
 update();
 saveView()
});
on('rescan',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value})}))});
on('logout',async()=>{
 if(document.querySelector('[data-dirty="true"]')&&!confirm('Ungespeicherte Änderungen verwerfen und abmelden?'))return;
 for(const draft of document.querySelectorAll<HTMLElement>('[data-dirty]'))draft.dataset.dirty='false';
 stopBackground();
 await api('/api/logout',{method:'POST'});
 location.reload()
});
window.addEventListener('pagehide',stopBackground);
window.addEventListener('pageshow',event=>{
 if(event.persisted&&session){
  pollingReady=true;
  if(!document.hidden)scanPoller.start()
 }
});
document.addEventListener('visibilitychange',()=>{
 if(document.hidden){
  scanPoller.stop();
  pause();
  tiles.forEach(t=>t.release())
 }else if(pollingReady)scanPoller.start()
});
const loginForm=$<HTMLFormElement>('login-form');
loginForm.addEventListener('submit',async e=>{
 e.preventDefault();
 const button=loginForm.querySelector('button')!;
 button.disabled=true;
 try{
  const data=new FormData(loginForm);
  const res=await fetch('/auth/local',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(data))});
  const body=await res.json();
  if(!res.ok){
   if(body.totpRequired){
    const code=loginForm.querySelector<HTMLInputElement>('[name=code]')!,first=$('login-code').hidden;
    $('login-code').hidden=false;
    code.disabled=false;
    code.required=true;
    code.value='';
    button.disabled=false;
    code.focus();
    if(first){
     $('login-error').textContent='';
     return
    }
   }
   throw new Error(body.error)
  }
  location.reload()
 }catch(e){
  $('login-error').textContent=(e as Error).message
 }finally{
  button.disabled=false
 }
});
let managementInitialized=false;
async function boot(){
 pollingReady=false;
 scanPoller.stop();
 playbackClock.stop();
 $('app').hidden=true;
 $('login').hidden=true;
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
 session=await response.json();
 $('password-button').hidden=false;
 $('password-button').textContent='Mein Konto';
 accountMenu.open=false;
 // Account, session and administration pages load as a separate chunk after sign-in;
 // it registers its pages before navigation starts.
 if(!managementInitialized){
  const {initManagement}=await import('./management');
  initManagement({api,getSession:()=>session,getCameras:()=>cameras,getCursor:()=>cursor,notify});
  managementInitialized=true
 }
 $('user-name').textContent=session.name;
 $('avatar').dataset.initials=session.name.slice(0,2).toUpperCase();
 document.querySelector('#account-menu summary')!.setAttribute('aria-label','Konto: '+session.name);
 document.querySelector('#account-menu summary')!.setAttribute('title',session.name);
 $('settings-button').hidden=session.role!=='admin';
 $('rescan').hidden=session.role!=='admin';
 dateInput.value=berlinDate(new Date());
 await refreshCameras();
 let stored:unknown;
 try{stored=JSON.parse(localStorage.getItem('cctv-view:'+session.preferenceKey)||'null')}catch{}
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

 delete state.railCollapsed;
 delete state.hotspot;
 Object.assign(state,defaultView(),saved);
 state.theme=saved.theme||'system';
 window.dispatchEvent(new CustomEvent('themecommit',{detail:state.theme}));
 applyRail();
 hotspot=saved.hotspot;
 if(saved.cameras)selected=new Set([...saved.cameras,...(saved.order?cameras.filter(c=>!saved.order!.includes(c.id)).map(c=>c.id):[])]);
 if(saved.order)order=[...saved.order,...order.filter(id=>!saved.order!.includes(id))];
 for(const id of ['preload','aspect'] as const)$<HTMLSelectElement>(id).value=state[id];
 $<HTMLInputElement>('grid-columns').value=String(state.columns);
 $<HTMLInputElement>('grid-rows').value=String(state.rows);
 $<HTMLInputElement>('auto-hotspot').checked=state.autoHotspot;
 applyPreferences();
 setTimelineHeight(state.timelineHeight);
 renderCameras();
 await loadDay();
 const initialSnapshot=preferenceSnapshot();
 // Remember the server's known cameras so the next edit also saves newly
 // discovered cameras and their order. Otherwise a deselected new camera
 // would be mistaken for a new addition again after every reload.
 preferenceWriter=new PreferenceWriter({...initialSnapshot,cameras:saved.cameras??initialSnapshot.cameras,order:saved.order??initialSnapshot.order},changes=>api('/api/preferences',{method:'PATCH',body:JSON.stringify(changes),keepalive:true}),preferenceStatus);
 try{localStorage.setItem('cctv-view:'+session.preferenceKey,JSON.stringify(preferenceSnapshot()))}catch{}
 refreshRange=attachTimelineControls(canvas,{bounds:()=>({from,to,cursor,viewStart,viewEnd}),preview:timelinePreview,seek,zoom,seekStep:()=>state.seekStep,pan:(start,end)=>{
  viewStart=start;
  viewEnd=end;
  draw();
  refreshRange()
 }});
 $('boot-error').hidden=true;
 $('app').hidden=false;
 startNavigation(()=>session.role==='admin',pause);
 pollingReady=true;
 if(!document.hidden)scanPoller.start();
}
function showBootError(error:unknown){$('boot-error-message').textContent=(error as Error).message||'Unbekannter Fehler';$('boot-error').hidden=false}
async function runBoot(){
 const button=$<HTMLButtonElement>('boot-retry');
 button.disabled=true;
 if(!$('boot-error').hidden)$('boot-error-message').textContent='Verbindung wird hergestellt …';
 try{
  await boot()
 }catch(error){
  showBootError(error)
 }finally{
  button.disabled=false
 }
}
$('boot-retry').addEventListener('click',()=>void runBoot());
void runBoot();

const {applyPreferences}=initPreferencesDialog({
 state,notify,scheduleResize:()=>scheduleResize(),setTimelineHeight:height=>setTimelineHeight(height),
 resetSelection:()=>{selected=new Set(cameras.map(c=>c.id));order=cameras.map(c=>c.id);hotspot=undefined;page=0},
 applyRail,releaseTiles:()=>tiles.forEach(t=>t.release()),renderCameras:()=>renderCameras(),saveView:()=>saveView(),
 snapshot:()=>({state:structuredClone(state),selected:[...selected],order:[...order],hotspot}),
 restore:value=>{
  const previous=value as {state:ViewState;selected:string[];order:string[];hotspot?:string};
  for(const key of Object.keys(state))delete (state as unknown as Record<string,unknown>)[key];
  Object.assign(state,structuredClone(previous.state));selected=new Set(previous.selected);order=[...previous.order];hotspot=previous.hotspot;
  for(const id of ['layout','quality','speed'] as const)$<HTMLSelectElement>(id).value=state[id];
  window.dispatchEvent(new CustomEvent('themecommit',{detail:state.theme||'system'}));
  setTimelineHeight(state.timelineHeight);applyRail();applyPreferences();tiles.forEach(t=>t.release());renderCameras();void saveView();
 },
 notifyAction:(message,label,run)=>notify(message,'info',{label,run})
});
$('select-none').after(createReorderToggle('reorder-cameras',()=>reorderMode,()=>{reorderMode=!reorderMode;$('reorder-hint').hidden=!reorderMode;renderCameras()}));
on('select-all',()=>{selected=new Set(cameras.map(c=>c.id));page=0;renderCameras();saveView()});
on('select-none',()=>{selected.clear();page=0;renderCameras();saveView()});
on('page-prev',()=>{page--;renderCameras()});
on('page-next',()=>{page++;renderCameras()});
const scanAll=async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value,all:true})}))};
on('scan-all',scanAll);
on('scan-all-quick',scanAll);
on('rebuild-index',async()=>{
 if(!await confirmAction({title:'Index neu aufbauen',message:'Der abgeleitete Index und alle Vorschaubilder werden gelöscht und neu eingelesen. Kameras und Originalvideos bleiben erhalten.',confirm:'Index neu aufbauen',danger:true}))return;
 pause();
 tiles.forEach(t=>t.release());
 await api('/api/index/rebuild',{method:'POST'});
 records=[];
 await loadDay();
 notify('Index geleert. Der geöffnete Tag wird erneut eingelesen.')
});
overview.onpointerdown=e=>{
 overview.setPointerCapture(e.pointerId);
 seek(from+clamp((e.clientX-overview.getBoundingClientRect().left)/overview.clientWidth,0,1)*(to-from));
 const span=viewEnd-viewStart;
 viewStart=clamp(cursor-span/2,from,to-span);
 viewEnd=viewStart+span;
 draw()
};
overview.onpointermove=e=>{
 if(e.buttons===1){
  const span=viewEnd-viewStart,start=clamp(from+(e.clientX-overview.getBoundingClientRect().left)/overview.clientWidth*(to-from)-span/2,from,to-span);
  viewStart=start;
  viewEnd=start+span;
  draw()
 }
};
overview.onkeydown=e=>{
 if(e.ctrlKey||e.metaKey||e.altKey)return;
 const target=keyboardSeekTarget(e.key,e.shiftKey,cursor,from,to,state.seekStep);
 if(target!==undefined){
  e.preventDefault();
  seek(target)
 }
};
function setTimelineHeight(height:number){
 state.timelineHeight=Math.round(clamp(height,100,650));
 $('timeline-viewport').style.height=state.timelineHeight+'px';
 resize.setAttribute('aria-valuenow',String(state.timelineHeight));
 resize.setAttribute('aria-valuetext',`${state.timelineHeight} Pixel`);
 draw()
}
let resizeStart=0,resizeHeight=0;
const resize=$('timeline-resize');
resize.onpointerdown=e=>{
 resize.setPointerCapture(e.pointerId);
 resizeStart=e.clientY;
 resizeHeight=state.timelineHeight
};
resize.onpointermove=e=>{
 if(!resize.hasPointerCapture(e.pointerId))return;
 setTimelineHeight(resizeHeight+e.clientY-resizeStart)
};
resize.onpointerup=()=>saveView();
resize.onkeydown=e=>{
 if(['ArrowUp','ArrowDown'].includes(e.key)){
  e.preventDefault();
  setTimelineHeight(state.timelineHeight+(e.key==='ArrowDown'?20:-20));
  saveView()
 }
};
let observedGridWidth=-1;
new ResizeObserver(([entry])=>{const width=entry.contentRect.width;if(width===observedGridWidth)return;observedGridWidth=width;scheduleResize(true)}).observe($('video-grid'));
async function openRecording(r:SearchResult){
 navigate('recordings');selected.add(r.cameraId);dateInput.value=berlinDate(new Date(r.start*1000));page=0;renderCameras();await loadDay();seek(r.start);
 const cameraIndex=ordered().findIndex(c=>c.id===r.cameraId);if(cameraIndex>=0)page=Math.floor(cameraIndex/visiblePageCapacity());renderCameras();saveView();
}
initSearchPage({cameras:()=>cameras,request:(url,signal)=>api<SearchPage>(url,{signal}),notify,open:openRecording});
// An empty day offers a direct jump to the newest indexed recording instead of a dead end.
on('latest-recording',async()=>{
 const button=$<HTMLButtonElement>('latest-recording');button.disabled=true;
 try{
  const data=await api<SearchPage>('/api/search?order=desc&count=0');
  if(data.results[0])await openRecording(data.results[0]);else notify('Im Index ist noch keine Aufnahme vorhanden.');
 }catch(e){notify((e as Error).message)}finally{button.disabled=false}
});

const shortcutHelp=initShortcutHelp();
on('shortcut-help-open',()=>shortcutHelp.open());
on('shortcut-help-button',()=>shortcutHelp.open());
// The tile a single-camera shortcut acts on: fullscreen, focused, hotspot, then the first one.
function shortcutTile(target?:HTMLElement){
 const element=document.fullscreenElement||expandedTile()||target?.closest('.camera-tile')||document.querySelector('.camera-group.hotspot-primary .camera-tile')||document.querySelector('#video-grid .camera-tile');
 return [...tiles.values()].find(tile=>tile.container===element);
}
function changeSpeed(step:number){
 const select=$<HTMLSelectElement>('speed'),index=clamp(select.selectedIndex+step,0,select.options.length-1);
 if(index===select.selectedIndex)return;
 select.selectedIndex=index;select.dispatchEvent(new Event('change',{bubbles:true}));notify('Geschwindigkeit '+select.options[index].text);
}
document.addEventListener('keydown',e=>{
 if(shortcutHelp.isOpen)return;
 if(e.key==='Escape'&&document.fullscreenElement)void document.exitFullscreen().catch(()=>notify('Vollbild konnte nicht geschlossen werden.'));
 // Closing an expanded tile consumes Escape, so the page does not also navigate away.
 if(e.key==='Escape'&&collapseTile()){e.preventDefault();return}
 const target=e.target instanceof HTMLElement?e.target:undefined;
 const tag=target?.tagName;
 if(tag==='INPUT'||tag==='SELECT'||tag==='TEXTAREA'||tag==='CANVAS'||target?.isContentEditable)return;
 // Inside an enlarged tile its buttons do not swallow the playback shortcuts; Enter still activates them.
 const enlarged=target?.closest<HTMLElement>('.camera-tile');const inViewer=!!enlarged&&(document.fullscreenElement===enlarged||enlarged.classList.contains('expanded'));
 if(target?.closest('button,a,summary,[role="button"],[role="link"]')&&!inViewer&&!(e.key.toLowerCase()==='f'&&target.closest('.camera-tile')))return;
 if(currentPage()!=='recordings'||e.defaultPrevented||e.ctrlKey||e.metaKey||e.altKey||!state.shortcuts||!session)return;
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
 }else if(e.key==='?'){
  e.preventDefault();shortcutHelp.open();
 }else if(e.key==='m'||e.key==='M'){
  e.preventDefault();shortcutTile(target)?.audioButton.click();
 }else if(e.key==='ArrowUp'||e.key==='ArrowDown'){
  e.preventDefault();changeSpeed(e.key==='ArrowUp'?1:-1);
 }else if(e.key==='n'||e.key==='N'||e.key==='p'||e.key==='P'){
  // Next or previous recording boundary, like the arrows beside play.
  e.preventDefault();const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,e.key.toLowerCase()==='n'?1:-1);if(t!==undefined)seek(t);
 }else if(e.key==='Home'||e.key==='End'){
  e.preventDefault();seek(e.key==='Home'?from:to-.001);
 }else if(e.key===','||e.key==='.'){
  // Single frames at the usual 25 fps of camera recordings; stepping always pauses first.
  e.preventDefault();if(playing)pause();seek(cursor+(e.key===','?-.04:.04));
 }
});

initCameraBatch({api,notify,cameras:()=>cameras,date:()=>dateInput.value,dialog,cameraInput,discardCamera,resetCamera,refreshCameras,loadDay:()=>loadDay()});

window.addEventListener('themechange',()=>{timelineView.invalidate();draw()});

document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!e.defaultPrevented&&currentPage()!=='recordings'){e.preventDefault();navigate('recordings')}});

