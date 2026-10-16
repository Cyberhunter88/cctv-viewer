import {prepareDialogs} from './dialogs';
import Hls from 'hls.js';
import {validView,gridSize,pageCapacity,hotspotCandidate,recordingBoundary,moveCamera,defaultView,type ViewState} from './view-state';
import {attachTimelineControls} from './timeline-controls';
import {initManagement,berlinEpoch} from './management';
import {recordingAt,nextRecording,playbackRecordings,displayedRecordings,firstRecordingStart,clamp,berlinDate,clock,type Recording} from './timeline';
interface Camera {viewMode?:string;wideMarker?:string;zoomMarker?:string;zoomOffset?:number;supportsZoom?:boolean;id:string;name:string;folder:string;pattern:string;timezone:string;offset:number;format?:string;prefix?:string;indexingMode?:string;directoryPattern?:string;sourceStatus?:string;lastScan?:number;indexedVideos?:number}
interface ViewDetection {status:'confirmed'|'swapped'|'unclear';message:string;pairs:number}
interface Session {preferenceKey:string;name:string;role:'admin'|'viewer';csrf:string;kind:'local'|'oidc'}
interface CameraScan {id:string;name:string;state:string;inspected:number;added:number;changed:number;skipped:number}
interface Scan {jobId?:string;state?:string;completion?:number;queue?:{id:string;date:string;camera:string;automatic:boolean}[];cameras?:CameraScan[];active?:string[];completed?:number;total?:number;running:boolean;date:string;camera:string;indexed:number;warnings:string[]}
interface Day {recordings:Recording[];from:number;to:number;scan:Scan}
interface SearchPage {results:{id:string;cameraId:string;cameraName:string;filename:string;start:number;duration:number}[];total:number;offset:number;limit:number;hasMore:boolean}
prepareDialogs();
const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
let session:Session, cameras:Camera[]=[],records:Recording[]=[],playableRecords:Recording[]=[],displayedRecords:Recording[]=[],selected=new Set<string>(),from=0,to=0,cursor=0,viewStart=0,viewEnd=0,playing=false,lastFrame=0,scanRunning=false,loadGeneration=0;
let thumbGeneration=0;const thumbURLs:string[]=[];
const tiles=new Map<string,Tile>();
const openZoom=new Set<string>();
let audioTile:Tile|undefined;
const dateInput=$<HTMLInputElement>('date'),canvas=$<HTMLCanvasElement>('timeline');
const timelineLayer=document.createElement('canvas');let timelineKey='',timelineRecords:Recording[]|undefined;
const speed=()=>Number($<HTMLSelectElement>('speed').value);
let refreshRange=()=>{};let page=0,order:string[]=[],hotspot:string|undefined,lastHotspotCursor:number|undefined;
let scanCompletion=0;
let toastTimer:ReturnType<typeof setTimeout>|undefined;
 const state:ViewState=defaultView();
 let trace:{at:number;event:string;camera?:string;detail?:string}[]=[],tracing=false,traceStart=0;
 function log(event:string,camera?:string,detail?:string){if(tracing&&trace.length<20000)trace.push({at:Math.round(performance.now()-traceStart),event,camera,detail});}

async function toggleFullscreen(tile:HTMLElement){
 try{
  if(document.fullscreenElement){await document.exitFullscreen();return}
  if(document.fullscreenEnabled&&tile.requestFullscreen)await tile.requestFullscreen();
  else {tile.classList.toggle('expanded');tile.querySelector('.tile-expand')?.setAttribute('aria-label',tile.classList.contains('expanded')?'Vergrößerte Ansicht schließen':'Ansicht vergrößern')}
 }catch{notify('Vollbild konnte nicht geöffnet werden. Bitte erneut versuchen.')}
}
document.addEventListener('fullscreenchange',()=>{
 for(const tile of tiles.values()){const active=document.fullscreenElement===tile.container;const button=tile.container.querySelector('.tile-expand');if(button){button.setAttribute('aria-label',active?'Vollbild schließen':document.fullscreenEnabled?'Vollbild öffnen':'Ansicht vergrößern');button.textContent=active?'⤡':'⤢'}tile.applyAspect()}
});
function applyRail(){
 const collapsed=state.railCollapsed??matchMedia('(max-width:1100px)').matches;
 document.documentElement.classList.toggle('rail-collapsed',collapsed);
 $('rail-toggle').setAttribute('aria-expanded',String(!collapsed));
 $('rail-toggle').setAttribute('aria-label',collapsed?'Seitenleiste ausklappen':'Seitenleiste einklappen');
 scheduleResize();
}
on('rail-toggle',()=>{state.railCollapsed=!(state.railCollapsed??matchMedia('(max-width:1100px)').matches);applyRail();saveView()});
matchMedia('(max-width:1100px)').addEventListener('change',applyRail);
const accountMenu=$<HTMLDetailsElement>('account-menu');
accountMenu.querySelector('summary')!.addEventListener('click',e=>{if($('password-button').hidden)e.preventDefault()});
function closeAccount(){if(!accountMenu.open)return;accountMenu.open=false;accountMenu.querySelector('summary')!.focus()}
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&accountMenu.open){e.preventDefault();closeAccount()}});
document.addEventListener('click',e=>{if(e.target instanceof Node&&!accountMenu.contains(e.target))accountMenu.open=false});
for(const id of ['password-button','logout'])$(id).addEventListener('click',()=>{accountMenu.open=false});

function saveView(){if(!session)return;state.cameras=[...selected];state.order=order;state.hotspot=hotspot;for(const id of ['layout','quality','speed'] as const)state[id]=$<HTMLSelectElement>(id).value;try{localStorage.setItem('cctv-view:'+session.preferenceKey,JSON.stringify(state))}catch{}}
function notify(message:string){$('toast').textContent=message;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,6000)}
async function api<T>(url:string,options:RequestInit={}):Promise<T>{
 const headers=new Headers(options.headers);if(options.body)headers.set('Content-Type','application/json');if(session)headers.set('X-CSRF-Token',session.csrf);
 const response=await fetch(url,{...options,headers});if(response.status===401){pause();location.reload();throw new Error('Anmeldung abgelaufen')}
 const text=await response.text();let body:any={};try{body=text?JSON.parse(text):{}}catch{body={error:text||`Serverfehler (${response.status})`}}
 if(!response.ok)throw Object.assign(new Error((body.error||`Anfrage fehlgeschlagen (${response.status})`)+(body.gaps?'\n'+body.gaps.map((g:number[])=>clock(g[0])+' – '+clock(g[1])).join('\n'):'')),{indexing:body.indexing===true});return body as T;
}
function on(id:string,fn:()=>void|Promise<void>){$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>notify(e.message))})}
function element<K extends keyof HTMLElementTagNameMap>(tag:K,className='',text=''):HTMLElementTagNameMap[K]{const e=document.createElement(tag);e.className=className;e.textContent=text;return e}
function ordered(){return order.map(id=>cameras.find(c=>c.id===id)).filter((c):c is Camera=>!!c&&selected.has(c.id))}
function grid(){return gridSize($<HTMLSelectElement>('layout').value,state.columns,state.rows,selected.size,$('video-grid').clientWidth||innerWidth,ordered().some(c=>c.viewMode==='dual'&&openZoom.has(c.id)))}
function visiblePageCapacity(){const [cols,rows]=grid();return pageCapacity($<HTMLSelectElement>('layout').value,cols,rows)}
function visible():Camera[]{const all=ordered(),capacity=visiblePageCapacity();page=clamp(page,0,Math.max(0,Math.ceil(all.length/capacity)-1));return all.slice(page*capacity,(page+1)*capacity)}
function setMessage(tile:Tile,message:string,error=false){if(tile.messageState===message&&tile.messageError===error)return;tile.messageState=message;tile.messageError=error;tile.message.replaceChildren();if(message){tile.message.append(element('span','symbol',error?'⚠':'▧'),element('span','',message))}tile.message.hidden=!message;tile.message.classList.toggle('error',error);if(error){const retry=element('button','tile-retry','Erneut versuchen');retry.type='button';retry.onclick=()=>tile.retry();tile.message.append(retry)}}
class Tile {
 audioButton=element('button','tile-audio');
 frameCallback?:number;messageState?:string;messageError=false;playPending=false;video=element('video');container=element('div','camera-tile');message=element('div','tile-message');timestamp=element('span');codec=element('small');stream?:string;record?:Recording;loading=false;failed=false;epoch=0;offset=0;factor=1;abort?:AbortController;hls?:Hls;loadedSpeed=1;attempts=0;recoveryStart=0;recoveryTimer?:ReturnType<typeof setTimeout>;stallTimer?:ReturnType<typeof setTimeout>;pauseTimer?:ReturnType<typeof setTimeout>;
 constructor(public camera:Camera,public view?:string){
  this.conceal();this.video.muted=true;this.video.preservesPitch=true;this.video.playsInline=true;this.video.preload=state.preload as 'metadata'|'auto';
  const header=element('div','tile-header'),title=element('button','tile-title',camera.name+(view==='wide'?' · Weitwinkel':view==='zoom'?' · Zoom':''));title.type='button';title.setAttribute('aria-label',title.textContent+' · Als Hotspot wählen');title.title=title.textContent+' · Als Hotspot wählen';title.onclick=()=>{hotspot=camera.id;state.autoHotspot=false;$<HTMLInputElement>('auto-hotspot').checked=false;renderGrid();saveView()};header.append(title,this.codec);
  const footer=element('div','tile-footer');footer.append(this.timestamp,element('span','','AUFNAHME'));this.container.append(this.video,header,this.message,footer);this.container.setAttribute('role','group');this.container.setAttribute('aria-label',title.textContent!);
  this.container.ondblclick=()=>void toggleFullscreen(this.container);
  const expand=element('button','tile-expand','⤢');expand.type='button';expand.setAttribute('aria-label',document.fullscreenEnabled?'Vollbild öffnen':'Ansicht vergrößern');expand.onclick=()=>void toggleFullscreen(this.container);this.audioButton.type='button';this.setAudio(false);
  this.audioButton.onclick=()=>{const enabled=audioTile!==this;if(audioTile)audioTile.setAudio(false);audioTile=enabled?this:undefined;this.setAudio(enabled);if(enabled&&playing)this.startPlayback()};
  this.audioButton.ondblclick=e=>e.stopPropagation();expand.ondblclick=e=>e.stopPropagation();footer.append(this.audioButton,expand);
  this.video.addEventListener('error',()=>{if(this.record&&!this.loading){if($<HTMLSelectElement>('quality').value==='native'&&this.video.error?.code===4)this.fail('Originalformat im Browser nicht unterstützt. Bitte Ausgewogen oder Schnell wählen');else this.recover('Video konnte nicht geladen werden')}});
  this.video.addEventListener('loadedmetadata',()=>{this.applyAspect();this.align()});
  this.video.addEventListener('canplay',()=>{if(!this.failed){clearTimeout(this.stallTimer);clearTimeout(this.recoveryTimer);this.align();this.reveal();log('ready',camera.id)}});
  this.video.addEventListener('seeked',()=>this.reveal());
  this.video.addEventListener('seeking',()=>{this.conceal();this.reveal()});
  this.video.addEventListener('waiting',()=>{if(playing&&!this.loading){setMessage(this,'Wird gepuffert …');log('buffering',camera.id);clearTimeout(this.stallTimer);this.stallTimer=setTimeout(()=>this.recover('Pufferung dauert zu lange'),10000)}});
  this.video.addEventListener('playing',()=>{clearTimeout(this.stallTimer);log('playing',camera.id)});
  // WebKit/GStreamer may clear muted when changing playback speed.
  this.video.addEventListener('volumechange',()=>{if(audioTile!==this&&!this.video.muted)this.video.muted=true});
  this.video.addEventListener('ended',()=>{if(this.record&&cursor<this.record.start+this.record.duration-1){this.release();this.update()}});
 }
 setAudio(enabled:boolean){this.video.muted=!enabled;this.audioButton.textContent=enabled?'Ton an':'Ton aus';this.audioButton.setAttribute('aria-pressed',String(enabled));const label=`${enabled?'Ton ausschalten':'Ton einschalten'} · ${this.camera.name}${this.view==='wide'?' · Weitwinkel':this.view==='zoom'?' · Zoom':''}`;this.audioButton.setAttribute('aria-label',label);this.audioButton.title=label}
 startPlayback(){if(this.playPending)return;this.playPending=true;const epoch=this.epoch;void this.video.play().catch((error:Error)=>{if(epoch!==this.epoch)return;if(error.name==='NotAllowedError'&&audioTile===this){audioTile=undefined;this.setAudio(false);notify('Der Browser hat die Tonwiedergabe blockiert. Bitte Ton erneut einschalten.')}}).finally(()=>{if(epoch===this.epoch)this.playPending=false})}
 applyAspect(){this.container.style.aspectRatio=state.aspect==='auto'?`${this.video.videoWidth||16} / ${this.video.videoHeight||9}`:state.aspect==='fill'?'16 / 9':state.aspect;this.video.style.objectFit=state.aspect==='fill'?'cover':'contain'}
 fail(message:string){const record=this.record;this.release();this.record=record;this.failed=true;setMessage(this,message+' · Bitte erneut versuchen.',true);log('failed',this.camera.id,message)}
 recover(message:string){if(this.failed)return;if(!this.recoveryStart)this.recoveryStart=performance.now();if(this.attempts>=3||performance.now()-this.recoveryStart>=30000){this.fail(message);return}const record=this.record;if(!record)return;this.attempts++;log('retry',this.camera.id,String(this.attempts));void this.load(record,true)}
 conceal(){if(this.frameCallback!==undefined)this.video.cancelVideoFrameCallback(this.frameCallback);this.frameCallback=undefined;this.container.classList.add('frame-pending')}
 reveal(){
  if(!this.record||this.failed||this.frameCallback!==undefined)return;
  const epoch=this.epoch;
  // currentTime can already reflect a seek while the compositor still holds
  // the previous frame. Reveal only after a matching frame has been decoded.
  this.frameCallback=this.video.requestVideoFrameCallback((_now,frame)=>{
   this.frameCallback=undefined;if(epoch!==this.epoch||!this.record||this.failed)return;
   if(this.loading||this.video.seeking){this.reveal();return}
   const target=Math.max(0,(cursor-this.record.start-this.offset)/this.factor);
   if(Math.abs(frame.mediaTime-target)>.65/this.factor){this.reveal();return}
   this.container.classList.remove('frame-pending');setMessage(this,'');
  });
 }
 release(){this.conceal();this.epoch++;clearTimeout(this.pauseTimer);clearTimeout(this.stallTimer);clearTimeout(this.recoveryTimer);this.abort?.abort();this.abort=undefined;this.hls?.destroy();this.hls=undefined;this.video.pause();this.video.removeAttribute('src');this.video.load();if(this.stream){const id=this.stream;this.stream=undefined;void api('/api/streams/'+id,{method:'DELETE',keepalive:true}).catch(()=>{})}this.loading=false;this.playPending=false;this.record=undefined;this.failed=false;this.offset=0;this.factor=1}
 destroy(){if(audioTile===this){audioTile=undefined;this.setAudio(false)}this.release();this.container.remove()}
 async load(record:Recording,recovering=false){
  const different=this.record?.id!==record.id;this.release();if(different&&!recovering){this.attempts=0;this.recoveryStart=0}this.record=record;const epoch=this.epoch;this.loading=true;this.reveal();setMessage(this,'Aufnahme wird geladen …');this.codec.textContent=record.codec==='hevc'?'H.265':record.codec.toUpperCase();this.loadedSpeed=speed();if(recovering&&this.recoveryStart)this.recoveryTimer=setTimeout(()=>this.fail('Wiederherstellung nach 30 Sekunden beendet'),Math.max(1,30000-(performance.now()-this.recoveryStart)));
  const quality=$<HTMLSelectElement>('quality').value;log('load',this.camera.id,quality);
  try{
   if(quality==='native'){this.video.src='/api/recordings/'+record.id+'/file';this.offset=0}
   else{
    this.abort=new AbortController();const transport=this.video.canPlayType('application/vnd.apple.mpegurl')?'hls':'mp4';
    const stream=await api<{id:string;url:string;offset:number;factor:number}>('/api/streams',{method:'POST',body:JSON.stringify({recording:record.id,offset:Math.max(0,cursor-record.start),profile:quality,speed:speed(),transport}),signal:this.abort.signal});
    if(epoch!==this.epoch){void api('/api/streams/'+stream.id,{method:'DELETE'}).catch(()=>{});return}
    this.stream=stream.id;this.offset=stream.offset;this.factor=stream.factor;
    if(transport==='mp4')this.video.src=stream.url;
    // A growing EVENT playlist is a recording from its beginning, not a live
    // camera. Prefer MSE with an explicit origin over native HLS live-edge seeks.
    else if(Hls.isSupported()){this.hls=new Hls({startPosition:0,maxBufferLength:20,maxMaxBufferLength:40,backBufferLength:10});this.hls.on(Hls.Events.ERROR,(_event,data)=>{if(data.fatal)this.recover('Streaming unterbrochen')});this.hls.loadSource(stream.url);this.hls.attachMedia(this.video)}
    else if(this.video.canPlayType('application/vnd.apple.mpegurl'))this.video.src=stream.url;
    else throw new Error('HLS wird nicht unterstützt');
   }
   if(epoch!==this.epoch)return;this.loading=false;this.align();
  }catch(e){if(epoch!==this.epoch)return;this.loading=false;if((e as Error).name!=='AbortError')this.recover((e as Error).message)}
 }
 align(){if(!this.record||this.loading||this.failed)return;const target=(cursor-this.record.start-this.offset)/this.factor;
  if(this.stream&&(target<-.1||this.loadedSpeed!==speed()||target>this.video.currentTime+3&&this.video.readyState>=1&&!Array.from({length:this.video.buffered.length},(_,i)=>[this.video.buffered.start(i),this.video.buffered.end(i)]).some(([a,b])=>target>=a&&target<b))){void this.load(this.record,true);return}
  if(this.video.readyState>=1&&Math.abs(this.video.currentTime-target)>.65/this.factor){try{this.conceal();this.reveal();this.video.currentTime=Math.max(0,target)}catch{}}
  const rate=speed()/this.factor;if(this.video.playbackRate!==rate)this.video.playbackRate=rate;if(playing){if(this.video.paused)this.startPlayback()}else if(!this.video.paused)this.video.pause();
 }
 current(){return recordingAt(playableRecords,this.camera.id,cursor,this.view)}
 update(timestamp=clock(cursor)){if(this.timestamp.textContent!==timestamp)this.timestamp.textContent=timestamp;const record=this.current();if(!record){if(this.record)this.release();setMessage(this,this.view&&recordingAt(records,this.camera.id,cursor,'unknown')?'Kennung unklar · Erkennung in Kameraeinstellungen prüfen':'Keine Aufnahme zu diesem Zeitpunkt');return}if(this.record?.id!==record.id){void this.load(record);return}this.align()}
 retry(){clearTimeout(this.pauseTimer);if(this.failed){const record=this.record;this.attempts=0;this.recoveryStart=0;this.release();if(record)void this.load(record)}}
 paused(){this.video.pause();clearTimeout(this.stallTimer);if(this.stream){const id=this.stream;this.pauseTimer=setTimeout(()=>{void api<{state:string}>('/api/playback-sessions/'+id).then(s=>{if(!playing&&this.stream===id&&s.state!=='complete')this.release()}).catch(()=>{if(!playing&&this.stream===id)this.release()})},2000)}}
}
function pause(){playing=false;$('play').textContent='▶';$('play').setAttribute('aria-label','Abspielen');tiles.forEach(t=>t.paused());log('pause')}
function play(){if(!playableRecords.length||!visible().length){notify('Für diese Auswahl sind keine Aufnahmen vorhanden.');return}if(!visible().some(c=>recordingAt(displayedRecords,c.id,cursor))){const next=nextRecording(displayedRecords,visible().map(c=>c.id),cursor);if(next===undefined){notify('Keine weiteren Aufnahmen an diesem Tag.');return}cursor=next}log('play');playing=true;lastFrame=performance.now();$('play').textContent='Ⅱ';$('play').setAttribute('aria-label','Pause');tiles.forEach(t=>t.retry());update()}
function seek(time:number){lastHotspotCursor=undefined;log('seek',undefined,String(time));cursor=clamp(time,from,to-.001);update()}
function update(){if(!from)return;const timestamp=clock(cursor);if($('clock').textContent!==timestamp)$('clock').textContent=timestamp;if(state.autoHotspot&&$<HTMLSelectElement>('layout').value==='hotspot'){const candidate=hotspotCandidate(displayedRecords,visible().map(c=>c.id),cursor,playing&&lastHotspotCursor!==undefined&&cursor>=lastHotspotCursor?lastHotspotCursor:undefined);if(candidate&&candidate!==hotspot){hotspot=candidate;renderGrid()}}lastHotspotCursor=cursor;tiles.forEach(t=>t.update(timestamp));draw();refreshRange()}
function frame(now:number){const matching=[...tiles.values()].filter(t=>t.current());const advancing=matching.length===0||matching.some(t=>t.failed||!t.loading&&t.video.readyState>=2);if(playing&&advancing){cursor+=(now-lastFrame)/1000*speed();if(cursor>=to){cursor=to-.001;pause()}else if(!visible().some(c=>recordingAt(displayedRecords,c.id,cursor))){const next=nextRecording(displayedRecords,visible().map(c=>c.id),cursor);if(next===undefined)pause();else cursor=next}if(cursor>viewEnd&&viewEnd-viewStart<to-from){const width=viewEnd-viewStart;viewStart=clamp(cursor-width/4,from,to-width);viewEnd=viewStart+width}update()}lastFrame=now;requestAnimationFrame(frame)}
function refreshDisplayedRecords(){displayedRecords=displayedRecordings(playableRecords,cameras,openZoom)}
function renderGrid(){
 const focused=document.activeElement instanceof HTMLElement&&$('video-grid').contains(document.activeElement)?document.activeElement:undefined;
 refreshDisplayedRecords();
 const active=visible();for(const[id,t]of tiles)if(!active.some(c=>c.id===t.camera.id&& (c.viewMode==='dual'?(t.view==='wide'||t.view==='zoom'&&openZoom.has(c.id)):!t.view))){t.destroy();tiles.delete(id)}
 const gridElement=$('video-grid'),[cols]=grid();gridElement.className='video-grid';gridElement.style.gridTemplateColumns=`repeat(${cols},minmax(0,1fr))`;
 gridElement.replaceChildren();
 const isHotspot=$<HTMLSelectElement>('layout').value==='hotspot';if(!active.some(c=>c.id===hotspot))hotspot=active[0]?.id;
 for(const c of active){
  const group=element('div','camera-group');group.id='camera-group-'+c.id;group.classList.toggle('dual',c.viewMode==='dual');group.classList.toggle('zoom-open',openZoom.has(c.id));group.classList.toggle('hotspot-primary',isHotspot&&c.id===hotspot&&cols>1&&grid()[1]>1);
  if(c.viewMode==='dual' && c.supportsZoom !== false){
   const toggle=element('button','zoom-toggle',openZoom.has(c.id)?'Zoom schließen':'Zoom anzeigen');toggle.type='button';toggle.id='zoom-toggle-'+c.id;toggle.setAttribute('aria-label',`${c.name}: ${toggle.textContent}`);toggle.setAttribute('aria-expanded',String(openZoom.has(c.id)));toggle.setAttribute('aria-controls',group.id);
   toggle.onclick=()=>{if(openZoom.has(c.id))openZoom.delete(c.id);else openZoom.add(c.id);page=Math.floor(ordered().findIndex(camera=>camera.id===c.id)/visiblePageCapacity());renderGrid();update();void thumbnails();$(toggle.id).focus()};group.append(toggle);
  }
  for(const view of c.viewMode==='dual'?(c.supportsZoom===false?['wide']:(openZoom.has(c.id)?['wide','zoom']:['wide'])):[undefined]){
   const key=c.id+':'+(view||'single');if(!tiles.has(key))tiles.set(key,new Tile(c,view));const tile=tiles.get(key)!;tile.camera=c;group.append(tile.container);tile.applyAspect();tile.video.preload=state.preload as 'metadata'|'auto';
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
async function loadDay(reset=true){const generation=++loadGeneration;if(reset){pause();tiles.forEach(t=>t.release())}const data=await api<Day>('/api/day?date='+encodeURIComponent(dateInput.value));if(generation!==loadGeneration)return;records=data.recordings;playableRecords=playbackRecordings(records,cameras);from=data.from;to=data.to;setScan(data.scan);refreshDisplayedRecords();if(reset){const ids=new Set(visible().map(c=>c.id));cursor=firstRecordingStart(displayedRecords.filter(r=>ids.has(r.cameraId)),from);viewStart=from;viewEnd=to}$('recording-count').textContent=`${records.length} Aufnahmen · ${cameras.length} Kameras`;update();void thumbnails();refreshRange()}
async function refreshCameras(){const previous=new Set(cameras.map(c=>c.id));cameras=await api<Camera[]>('/api/cameras');for(const id of openZoom)if(!cameras.some(c=>c.id===id&&c.viewMode==='dual'))openZoom.delete(id);order=[...order.filter(id=>cameras.some(c=>c.id===id)),...cameras.filter(c=>!order.includes(c.id)).map(c=>c.id)];for(const c of cameras)if(!previous.has(c.id))selected.add(c.id);for(const id of selected)if(!cameras.some(c=>c.id===id))selected.delete(id);renderCameras();renderCameraList()}
function updateTimelineDescription(cams:Camera[]){
 const description=$('timeline-description'),list=element('ul');
 description.replaceChildren(element('p','',`Aufnahmebereiche zwischen ${clock(viewStart)} und ${clock(viewEnd)}:`));
 for(const camera of cams){
  const ranges=displayedRecords.filter(r=>r.cameraId===camera.id&&r.start<viewEnd&&r.start+r.duration>viewStart);
  if(!ranges.length){list.append(element('li','',`${camera.name}: keine Aufnahme in diesem Zeitfenster`));continue}
  for(const record of ranges){
   const start=Math.max(viewStart,record.start),end=Math.min(viewEnd,record.start+record.duration);
   const view=record.view==='wide'?' · Weitwinkel':record.view==='zoom'?' · Zoom':'';
   list.append(element('li','',`${camera.name}${view}: ${clock(start)} bis ${clock(end)}`));
  }
 }
 if(!cams.length)list.append(element('li','','Keine Kameras sichtbar.'));
 description.append(list);
}
function timelineColors(){const css=getComputedStyle(document.documentElement);return Object.fromEntries(['line','muted','track','record','cursor','panel'].map(k=>[k,css.getPropertyValue('--timeline-'+k).trim()]))}
function draw(){if(!from)return;const palette=timelineColors();const cams=visible(),width=canvas.parentElement!.clientWidth,height=48+Math.max(1,cams.length)*34,dpr=devicePixelRatio||1;
 if(width<=0)return;
 // Compact the default viewport to its tracks; keep custom saved heights.
 $('timeline-viewport').style.height=(state.timelineHeight===240?Math.min(240,Math.max(100,height+12)):state.timelineHeight)+'px';
 if(canvas.width!==Math.round(width*dpr)||canvas.height!==Math.round(height*dpr)){canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);canvas.style.height=height+'px'}
 const ctx=canvas.getContext('2d')!;ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,width,height);const left=85,right=width-15,area=right-left,span=viewEnd-viewStart,x=(t:number)=>left+(t-viewStart)/span*area;
 const intervals=[60,300,600,1800,3600,7200,14400,21600];const interval=intervals.find(s=>span/s<area/65)||21600;
 const key=[width,height,dpr,viewStart,viewEnd,...cams.map(c=>c.id+':'+c.name+':'+c.viewMode)].join('/');
 if(timelineKey!==key||timelineRecords!==displayedRecords){
 ctx.font='11px system-ui';ctx.textAlign='center';for(let t=Math.ceil(viewStart/interval)*interval;t<=viewEnd;t+=interval){const p=x(t);ctx.strokeStyle=palette.line;ctx.beginPath();ctx.moveTo(p,30);ctx.lineTo(p,height-10);ctx.stroke();ctx.fillStyle=palette.muted;ctx.fillText(clock(t).slice(0,5),p,19)}
 cams.forEach((camera,i)=>{const y=39+i*34;ctx.fillStyle=palette.muted;ctx.textAlign='left';ctx.fillText(camera.name.length>11?camera.name.slice(0,10)+'…':camera.name,12,y+14);ctx.fillStyle=palette.track;ctx.fillRect(left,y,area,23);
  ctx.save();ctx.beginPath();ctx.rect(left,y,area,23);ctx.clip();for(const r of displayedRecords.filter(r=>r.cameraId===camera.id&&r.start<viewEnd&&(r.kind==='image'?r.start>=viewStart:r.start+r.duration>viewStart))){ctx.fillStyle=palette.record;ctx.fillRect(x(r.start),y+3,r.kind==='image'?4:Math.max(2,r.duration/span*area),17)}ctx.restore();
 });
 timelineLayer.width=canvas.width;timelineLayer.height=canvas.height;timelineLayer.getContext('2d')!.drawImage(canvas,0,0);timelineKey=key;timelineRecords=displayedRecords;updateTimelineDescription(cams);
 }else ctx.drawImage(timelineLayer,0,0,width,height);
 if(cursor>=viewStart&&cursor<=viewEnd){const p=x(cursor);ctx.strokeStyle=palette.cursor;ctx.lineWidth=1.5;ctx.beginPath();ctx.moveTo(p,29);ctx.lineTo(p,height-8);ctx.stroke();ctx.fillStyle=palette.cursor;ctx.beginPath();ctx.moveTo(p-4,27);ctx.lineTo(p+4,27);ctx.lineTo(p,33);ctx.fill()}
 drawOverview();
 const hours=span/3600;$('zoom-label').textContent=hours>=1?`${Math.round(hours*10)/10} Std.`:`${Math.round(span/60)} Min.`;
}
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
function cameraFormatChanged(){ $('dual-fields').hidden=$<HTMLSelectElement>('view-mode').value!=='dual';const automatic=$<HTMLSelectElement>('camera-format').value==='automatic';$('pattern-label').hidden=automatic;(cameraForm.elements.namedItem('pattern') as HTMLInputElement).required=!automatic;$('directory-pattern-label').hidden=$<HTMLSelectElement>('indexing-mode').value==='full'}
const dialog=$<HTMLDialogElement>('camera-dialog'),cameraForm=$<HTMLFormElement>('camera-form');
function cameraInput():Camera{const data={get:(name:string)=>(cameraForm.elements.namedItem(name) as HTMLInputElement|HTMLSelectElement)?.value};return{id:String(data.get('id')||''),name:String(data.get('name')),folder:String(data.get('folder')),pattern:String(data.get('pattern')),timezone:String(data.get('timezone')),offset:Number(data.get('offset')),format:String(data.get('format')),indexingMode:String(data.get('indexingMode')),directoryPattern:String(data.get('directoryPattern')),viewMode:String(data.get('viewMode')),wideMarker:String(data.get('wideMarker')),zoomMarker:String(data.get('zoomMarker')),zoomOffset:Number(data.get('zoomOffset')),supportsZoom:(cameraForm.elements.namedItem('supportsZoom') as HTMLInputElement)?.checked ?? true}}
let cameraBaseline='',cameraSaving=false;
function cameraSnapshot(){return JSON.stringify(cameraInput())}
function cameraDirty(){return cameraSnapshot()!==cameraBaseline}
function discardCamera(){return !cameraDirty()||confirm('Ungespeicherte Kameraänderungen verwerfen?')}
function markCameraSaved(){cameraBaseline=cameraSnapshot();renderCameraList()}
function resetCamera(){cameraForm.reset();(cameraForm.elements.namedItem('id') as HTMLInputElement).value='';$('preview-result').hidden=true;$('camera-error').textContent='';$('folder-browser').hidden=true;$('camera-editor-title').textContent='Neue Kamera';$<HTMLDetailsElement>('camera-advanced').open=false;cameraFormatChanged();markCameraSaved()}
function showCameraEditor(){dialog.classList.add('camera-editing');dialog.querySelector('.dialog-body')!.scrollTop=0;$('camera-editor-title').focus({preventScroll:true})}
function editCamera(c:Camera){
 cameraForm.reset();
 for(const[key,value]of Object.entries(c)){const input=cameraForm.elements.namedItem(key);if(input instanceof HTMLInputElement){if(input.type==='checkbox')input.checked=value==='true'||value===true;else input.value=String(value)}else if(input instanceof HTMLSelectElement)input.value=String(value)}
 $('preview-result').hidden=true;$('camera-error').textContent='';$('folder-browser').hidden=true;$('camera-editor-title').textContent=c.name+' bearbeiten';
 $<HTMLDetailsElement>('camera-advanced').open=!!(c.offset||c.format&&c.format!=='automatic'||c.timezone!=='Europe/Berlin');cameraFormatChanged();markCameraSaved();
}
function leaveCameraEditor(){if(cameraSaving||!discardCamera())return;resetCamera();dialog.classList.remove('camera-editing');$('new-camera').focus()}
function renderCameraList(){
 const list=$('camera-list');list.replaceChildren();$('camera-list-count').textContent=String(cameras.length);
 if(!cameras.length){const empty=element('p','camera-empty','Noch keine Kameras eingerichtet. Füge deine erste Aufnahmequelle hinzu.');list.append(empty)}
 const editing=(cameraForm.elements.namedItem('id') as HTMLInputElement).value;
 for(let i=0;i<order.length;i++){
  const c=cameras.find(c=>c.id===order[i]);if(!c)continue;
  const entry=element('article','camera-entry'+(editing===c.id?' is-selected':''));
  const select=element('button','camera-edit'),name=element('strong','camera-name',c.name);
  select.type='button';select.setAttribute('aria-label',c.name);select.setAttribute('aria-pressed',String(editing===c.id));
  const status=element('span','camera-status'+(c.sourceStatus==='offline'?' is-offline':''),`${c.sourceStatus==='offline'?'Quelle offline':'Quelle erreichbar'} · ${c.indexedVideos||0} ${c.indexedVideos===1?'Video':'Videos'}`);
  select.append(name,status,element('span','camera-edit-label','Bearbeiten →'));
  select.onclick=()=>{if(cameraSaving||!discardCamera())return;editCamera(c);showCameraEditor()};
  const actions=element('div','camera-entry-actions');
  const addAction=(text:string,label:string,action:()=>void|Promise<void>,className='')=>{const b=element('button',className,text);b.type='button';b.title=label;b.setAttribute('aria-label',label+' · '+c.name);b.disabled=cameraSaving;b.onclick=()=>void Promise.resolve(action()).catch(e=>notify(e.message));actions.append(b);return b};
  const up=addAction('↑','Nach oben verschieben',()=>reorder(i-1),'move-button');up.disabled=cameraSaving||i===0;
  const down=addAction('↓','Nach unten verschieben',()=>reorder(i+1),'move-button');down.disabled=cameraSaving||i===order.length-1;
  const cameraId=c.id;
  function reorder(target:number){order=moveCamera(order,cameraId,order[target]);renderCameras();renderCameraList();saveView()}
  addAction('Scannen','Kamera scannen',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value,camera:c.id,all:true})}))},'scan-camera');
  addAction('Entfernen','Kamera entfernen',async()=>{
   if(!confirm(`Kamera „${c.name}“ aus dem Viewer entfernen? Die Originalaufnahmen bleiben erhalten.`))return;
   if(editing===c.id&&!discardCamera())return;
   await api('/api/cameras/'+c.id,{method:'DELETE'});if(editing===c.id){resetCamera();dialog.classList.remove('camera-editing')}await refreshCameras();await loadDay();
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
 const controls=[...cameraForm.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLButtonElement>('input,select,button'),...['close-dialog','open-management','new-camera','batch-open'].map(id=>$<HTMLButtonElement>(id))];const disabled=controls.map(c=>c.disabled);
 const payload=cameraInput();controls.forEach(c=>c.disabled=true);
 try{
  $('camera-error').textContent='';const saved=await api<Camera&{detection?:ViewDetection}>('/api/cameras',{method:'POST',body:JSON.stringify(payload)});
  // The POST committed successfully even if refreshing recordings subsequently fails.
  (cameraForm.elements.namedItem('id') as HTMLInputElement).value=saved.id;cameraBaseline=cameraSnapshot();
  await refreshCameras();editCamera(cameras.find(c=>c.id===saved.id)||saved);await loadDay();notify(saved.detection?`Kamera gespeichert. ${saved.detection.message}`:'Kamera gespeichert. Aufnahmen werden eingelesen.');
 }catch(e){$('camera-error').textContent=(e as Error).message}
 finally{controls.forEach((c,i)=>c.disabled=disabled[i]);cameraSaving=false;submit.disabled=false;renderCameraList()}
});
on('preview',async()=>{try{const result=await api<{examples:{file:string;time?:string;error?:string;view?:string}[];limited:boolean;detection?:ViewDetection;wideMarker:string;zoomMarker:string}>('/api/cameras/preview',{method:'POST',body:JSON.stringify(cameraInput())});$('preview-result').hidden=false;const status=result.detection?`${result.detection.message}\nKennungen: Weitwinkel ${result.wideMarker}, Zoom ${result.zoomMarker}\n\n`:'';$('preview-result').textContent=status+(result.examples.length?result.examples.map(v=>`${v.file}${v.view?" · "+({wide:"Weitwinkel",zoom:"Zoom",unknown:"Kennung unklar"}[v.view]||v.view):""}\n→ ${v.error||v.time}`).join('\n\n'):'Keine passenden Videodateien gefunden.');if(result.limited)$('preview-result').textContent+='\nSuche auf 10.000 Einträge begrenzt.'}catch(e){$('camera-error').textContent=(e as Error).message}});
$<HTMLSelectElement>('camera-format').addEventListener('change',cameraFormatChanged);
async function browseFolder(folder=''){const data=await api<{path:string;folders:string[];samples:string[];limited:boolean}>('/api/cameras/folders?path='+encodeURIComponent(folder));const root=$('folder-browser');root.hidden=false;root.replaceChildren();root.append(element('strong','',data.path==='.'?'Aufnahmeverzeichnis':data.path));if(data.path!=='.'){const back=element('button','text-button','↑ Übergeordneter Ordner');back.type='button';back.onclick=()=>void browseFolder(data.path.split('/').slice(0,-1).join('/')).catch(e=>notify(e.message));root.append(back);const choose=element('button','button secondary','Diesen Ordner verwenden');choose.type='button';choose.onclick=()=>{(cameraForm.elements.namedItem('folder') as HTMLInputElement).value=data.path;const name=data.path.split('/').pop()||'';(cameraForm.elements.namedItem('name') as HTMLInputElement).value=name;root.hidden=true};root.append(choose)}for(const folder of data.folders){const b=element('button','text-button',folder.split('/').pop());b.type='button';b.onclick=()=>void browseFolder(folder).catch(e=>notify(e.message));root.append(b)}if(data.limited)root.append(element('small','','Suche begrenzt; bitte einen Unterordner wählen.'))}
on('browse-folder',()=>browseFolder());
on('new-camera',()=>{if(cameraSaving||!discardCamera())return;resetCamera();showCameraEditor()});
on('camera-back',leaveCameraEditor);on('camera-cancel',leaveCameraEditor);
function closeCamera(){if(!cameraSaving&&discardCamera()){resetCamera();dialog.classList.remove('camera-editing');dialog.close()}}
on('close-dialog',closeCamera);dialog.addEventListener('cancel',e=>{e.preventDefault();closeCamera()});
$('open-management').addEventListener('click',e=>{if(cameraSaving||!discardCamera()){e.stopImmediatePropagation();return}resetCamera();dialog.classList.remove('camera-editing')});
on('settings-button',()=>dialog.showModal());on('add-first',()=>{resetCamera();dialog.showModal();showCameraEditor()});
on('play',()=>playing?pause():play());on('skip-back',()=>{const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,-1);if(t!==undefined)seek(t)});on('skip-forward',()=>{const t=recordingBoundary(displayedRecords,visible().map(c=>c.id),cursor,1);if(t!==undefined)seek(t)});on('zoom-in',()=>zoom(.5));on('zoom-out',()=>zoom(2));on('reset-zoom',()=>{viewStart=from;viewEnd=to;draw();refreshRange()});
function changeDate(delta:number){const d=new Date(dateInput.value+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+delta);dateInput.value=d.toISOString().slice(0,10);return loadDay()}
on('prev-day',()=>changeDate(-1));on('next-day',()=>changeDate(1));on('today',()=>{dateInput.value=berlinDate(new Date());return loadDay()});dateInput.addEventListener('change',()=>void loadDay().catch(e=>notify(e.message)));
$<HTMLSelectElement>('layout').addEventListener('change',()=>{page=0;renderCameras();saveView()});$<HTMLSelectElement>('quality').addEventListener('change',()=>{tiles.forEach(t=>{t.release()});update();saveView()});$<HTMLSelectElement>('speed').addEventListener('change',()=>{update();saveView()});
on('rescan',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value})}))});
on('logout',async()=>{pause();tiles.forEach(t=>t.release());await api('/api/logout',{method:'POST'});location.reload()});
window.addEventListener('pagehide',()=>{pause();tiles.forEach(t=>t.release())});document.addEventListener('visibilitychange',()=>{if(document.hidden){pause();tiles.forEach(t=>t.release())}});
const loginForm=$<HTMLFormElement>('login-form');loginForm.addEventListener('submit',async e=>{e.preventDefault();const button=loginForm.querySelector('button')!;button.disabled=true;try{const data=new FormData(loginForm);const res=await fetch('/auth/local',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(data))});const body=await res.json();if(!res.ok)throw new Error(body.error);location.reload()}catch(e){$('login-error').textContent=(e as Error).message}finally{button.disabled=false}});
let managementInitialized=false;
async function boot(){
 $('app').hidden=true;$('login').hidden=true;
 const response=await fetch('/api/session');
 if(response.status===401){
  const methods=await api<{local:boolean;oidc:boolean}>('/auth/methods');
  loginForm.hidden=!methods.local;$('oidc-login').hidden=!methods.oidc;$('login-divider').hidden=!(methods.local&&methods.oidc);$('boot-error').hidden=true;$('login').hidden=false;return;
 }
 if(!response.ok)throw new Error(`Sitzung konnte nicht geladen werden (${response.status})`);
 session=await response.json();$('password-button').hidden=session.kind!=='local';accountMenu.open=false;const accountSummary=accountMenu.querySelector('summary')!;accountSummary.setAttribute('aria-disabled',String(session.kind!=='local'));accountSummary.tabIndex=session.kind==='local'?0:-1;
 if(!managementInitialized){initManagement({api,getSession:()=>session,getCameras:()=>cameras,getCursor:()=>cursor,notify});prepareDialogs();managementInitialized=true}
 $('user-name').textContent=session.name;$('avatar').textContent=session.name.slice(0,2).toUpperCase();document.querySelector('#account-menu summary')!.setAttribute('aria-label','Konto: '+session.name);document.querySelector('#account-menu summary')!.setAttribute('title',session.name);$('settings-button').hidden=session.role!=='admin';$('rescan').hidden=session.role!=='admin';dateInput.value=berlinDate(new Date());await refreshCameras();
 let stored:unknown;try{stored=JSON.parse(localStorage.getItem('cctv-view:'+session.preferenceKey)||'null')}catch{}
 const saved=validView(stored,cameras.map(c=>c.id));if(saved.originalQualityVersion!==1){saved.quality='native';saved.originalQualityVersion=1;try{localStorage.setItem('cctv-view:'+session.preferenceKey,JSON.stringify(saved))}catch{}}for(const id of ['layout','quality','speed'] as const)if(saved[id])$<HTMLSelectElement>(id).value=saved[id]!;
 Object.assign(state,saved);applyRail();hotspot=saved.hotspot;if(saved.cameras)selected=new Set(saved.cameras);if(saved.order)order=[...saved.order,...order.filter(id=>!saved.order!.includes(id))];for(const id of ['preload','aspect'] as const)$<HTMLSelectElement>(id).value=state[id];$<HTMLInputElement>('grid-columns').value=String(state.columns);$<HTMLInputElement>('grid-rows').value=String(state.rows);$<HTMLInputElement>('auto-hotspot').checked=state.autoHotspot;applyPreferences();setTimelineHeight(state.timelineHeight);renderCameras();await loadDay();
 refreshRange=attachTimelineControls(canvas,{bounds:()=>({from,to,cursor,viewStart,viewEnd}),seek,zoom,shortcuts:()=>state.shortcuts,seekStep:()=>state.seekStep,pan:(start,end)=>{viewStart=start;viewEnd=end;draw();refreshRange()}});
 $('boot-error').hidden=true;$('app').hidden=false;$('playback-diagnostics').hidden=false;requestAnimationFrame(frame);
 setInterval(async()=>{try{const scan=await api<Scan>('/api/scan');const wasRunning=scanRunning,completedBefore=scanCompletion;setScan(scan);if((wasRunning&&!scan.running)||scanCompletion>completedBefore)await loadDay(false)}catch(e){notify((e as Error).message)}},4000);
}
function showBootError(error:unknown){$('boot-error-message').textContent=(error as Error).message||'Unbekannter Fehler';$('boot-error').hidden=false}
async function runBoot(){const button=$<HTMLButtonElement>('boot-retry');button.disabled=true;if(!$('boot-error').hidden)$('boot-error-message').textContent='Verbindung wird hergestellt …';try{await boot()}catch(error){showBootError(error)}finally{button.disabled=false}}
$('boot-retry').addEventListener('click',()=>void runBoot());
void runBoot();

const preferences=$<HTMLDialogElement>('preferences-dialog'),preferencesForm=$<HTMLFormElement>('preferences-form');
let preferencesBaseline='',resetPreferencesPending=false;
const preferenceChecks=['showNames','showTimestamps','showCodec','showOverview','showThumbnails','shortcuts'] as const;
function preferencesSnapshot(){return JSON.stringify(Array.from(preferencesForm.querySelectorAll<HTMLInputElement|HTMLSelectElement>('input,select')).map(e=>[e.id,e.type==='checkbox'?(e as HTMLInputElement).checked:e.value]))}
function updatePreferencesSave(){$<HTMLButtonElement>('preferences-save').disabled=!resetPreferencesPending&&preferencesSnapshot()===preferencesBaseline}
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
 $('overview').hidden=!state.showOverview;$('thumbnails').hidden=!state.showThumbnails;if(!preferences.open)syncPreferences();scheduleResize();
}
function openPreferences(){syncPreferences();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();preferences.showModal()}
function closePreferences(){if((resetPreferencesPending||preferencesSnapshot()!==preferencesBaseline)&&!confirm('Ungespeicherte Einstellungen verwerfen?'))return;preferences.close();syncPreferences();resetPreferencesPending=false}
on('preferences-button',openPreferences);on('preferences-close',closePreferences);on('preferences-cancel',closePreferences);
preferences.addEventListener('cancel',e=>{e.preventDefault();closePreferences()});
preferencesForm.addEventListener('input',updatePreferencesSave);preferencesForm.addEventListener('change',updatePreferencesSave);
preferencesForm.addEventListener('submit',e=>{
 e.preventDefault();if(!preferencesForm.reportValidity())return;
 const theme=$<HTMLSelectElement>('theme').value;
 if(resetPreferencesPending){Object.assign(state,defaultView());delete state.railCollapsed;selected=new Set(cameras.map(c=>c.id));order=cameras.map(c=>c.id);hotspot=undefined;page=0;for(const id of ['layout','quality','speed'] as const)$<HTMLSelectElement>(id).value=state[id]}
 for(const key of preferenceChecks)state[key]=$<HTMLInputElement>('pref-'+key).checked;
 state.density=$<HTMLSelectElement>('pref-density').value as ViewState['density'];state.seekStep=Number($<HTMLSelectElement>('pref-seekStep').value);
 for(const id of ['preload','aspect'] as const)state[id]=$<HTMLSelectElement>(id).value;
 state.columns=Number($<HTMLInputElement>('grid-columns').value);state.rows=Number($<HTMLInputElement>('grid-rows').value);state.autoHotspot=$<HTMLInputElement>('auto-hotspot').checked;
 setTimelineHeight(Number($<HTMLInputElement>('pref-timelineHeight').value));
 window.dispatchEvent(new CustomEvent('themecommit',{detail:theme}));
 applyRail();applyPreferences();if(resetPreferencesPending)tiles.forEach(t=>t.release());renderCameras();saveView();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();preferences.close();notify('Persönliche Einstellungen gespeichert.');
});
on('preferences-reset',()=>{
 if(!confirm('Persönliche Einstellungen und Kameraauswahl auf die Standardwerte zurücksetzen? Erst Speichern übernimmt die Änderung.'))return;
 syncPreferences(defaultView());$<HTMLSelectElement>('theme').value='system';resetPreferencesPending=true;updatePreferencesSave();
});
on('select-all',()=>{selected=new Set(cameras.map(c=>c.id));page=0;renderCameras();saveView()});
on('select-none',()=>{selected.clear();page=0;renderCameras();saveView()});
$<HTMLSelectElement>('indexing-mode').onchange=cameraFormatChanged;
on('page-prev',()=>{page--;renderCameras()});on('page-next',()=>{page++;renderCameras()});
on('scan-all',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value,all:true})}))});
on('rebuild-index',async()=>{if(!confirm('Den abgeleiteten Index und Vorschaubilder neu aufbauen? Kameras und Originalvideos bleiben erhalten.'))return;pause();tiles.forEach(t=>t.release());await api('/api/index/rebuild',{method:'POST'});records=[];await loadDay();notify('Index geleert. Der geöffnete Tag wird erneut eingelesen.')});
const overview=$<HTMLCanvasElement>('overview'),overviewLayer=document.createElement('canvas');let overviewKey='',overviewRecords:Recording[]|undefined;
function drawOverview(){if(!from)return;const palette=timelineColors();const width=Math.max(100,overview.parentElement!.clientWidth),dpr=devicePixelRatio||1;if(overview.width!==width*dpr){overview.width=width*dpr;overview.height=34*dpr}const ctx=overview.getContext('2d')!;ctx.setTransform(dpr,0,0,dpr,0,0);const ids=visible().map(c=>c.id),key=[from,to,width,dpr,...ids].join('/');if(overviewKey!==key||overviewRecords!==displayedRecords){ctx.fillStyle=palette.track;ctx.fillRect(0,0,width,34);ctx.fillStyle=palette.record;for(const r of displayedRecords)if(ids.includes(r.cameraId)){const start=clamp(r.start,from,to);ctx.fillRect((start-from)/(to-from)*width,10,Math.max(1,(Math.min(to,r.start+r.duration)-start)/(to-from)*width),14)}overviewLayer.width=overview.width;overviewLayer.height=overview.height;overviewLayer.getContext('2d')!.drawImage(overview,0,0);overviewKey=key;overviewRecords=displayedRecords}else ctx.drawImage(overviewLayer,0,0,width,34);ctx.strokeStyle=palette.cursor;ctx.strokeRect((viewStart-from)/(to-from)*width,1,(viewEnd-viewStart)/(to-from)*width,32);ctx.fillStyle=palette.cursor;ctx.fillRect((cursor-from)/(to-from)*width,0,1,34)}

overview.onpointerdown=e=>{overview.setPointerCapture(e.pointerId);seek(from+clamp((e.clientX-overview.getBoundingClientRect().left)/overview.clientWidth,0,1)*(to-from));const span=viewEnd-viewStart;viewStart=clamp(cursor-span/2,from,to-span);viewEnd=viewStart+span;draw()};
overview.onpointermove=e=>{if(e.buttons===1){const span=viewEnd-viewStart,start=clamp(from+(e.clientX-overview.getBoundingClientRect().left)/overview.clientWidth*(to-from)-span/2,from,to-span);viewStart=start;viewEnd=start+span;draw()}};
overview.onkeydown=e=>{if(state.shortcuts&&['ArrowLeft','ArrowRight'].includes(e.key)){e.preventDefault();seek(cursor+(e.key==='ArrowRight'?1:-1)*(e.shiftKey?60:state.seekStep))}};
function setTimelineHeight(height:number){state.timelineHeight=Math.round(clamp(height,100,650));$('timeline-viewport').style.height=state.timelineHeight+'px';$<HTMLInputElement>('pref-timelineHeight').value=String(state.timelineHeight);resize.setAttribute('aria-valuenow',String(state.timelineHeight));resize.setAttribute('aria-valuetext',`${state.timelineHeight} Pixel`);draw()}
let resizeStart=0,resizeHeight=0;const resize=$('timeline-resize');resize.onpointerdown=e=>{resize.setPointerCapture(e.pointerId);resizeStart=e.clientY;resizeHeight=state.timelineHeight};resize.onpointermove=e=>{if(!resize.hasPointerCapture(e.pointerId))return;setTimelineHeight(resizeHeight+e.clientY-resizeStart)};resize.onpointerup=()=>saveView();resize.onkeydown=e=>{if(['ArrowUp','ArrowDown'].includes(e.key)){e.preventDefault();setTimelineHeight(state.timelineHeight+(e.key==='ArrowDown'?20:-20));saveView()}};
let observedGridWidth=-1;
new ResizeObserver(([entry])=>{const width=entry.contentRect.width;if(width===observedGridWidth)return;observedGridWidth=width;scheduleResize(true)}).observe($('video-grid'));
on('search-button',()=>{searchGeneration++;const select=$<HTMLSelectElement>('search-camera');select.replaceChildren(new Option('Alle Kameras',''));for(const c of cameras)select.add(new Option(c.name,c.id));$('search-count').hidden=true;$('search-error').textContent='';$('search-results').replaceChildren();$<HTMLButtonElement>('search-more').hidden=true;$<HTMLButtonElement>('search-more').disabled=false;$<HTMLButtonElement>('search-submit').disabled=false;$<HTMLDialogElement>('search-dialog').showModal()});on('search-close',()=>{searchGeneration++;$<HTMLDialogElement>('search-dialog').close()});
let searchGeneration=0,searchParams:URLSearchParams|undefined,searchOffset=0;
async function loadSearchPage(append=false){
 if(!searchParams)return;
 const own=searchGeneration,params=new URLSearchParams(searchParams);
 params.set('offset',String(searchOffset));
 const more=$<HTMLButtonElement>('search-more');
 more.disabled=true;
 if(!append){$('search-error').textContent='';$<HTMLButtonElement>('search-submit').disabled=true}
 try{
  const data=await api<SearchPage>('/api/search?'+params);
  if(own!==searchGeneration)return;
  const root=$('search-results');if(!append)root.replaceChildren();
  for(const r of data.results){
   const button=element('button','search-result',`${r.cameraName} · ${berlinDate(new Date(r.start*1000))} ${clock(r.start)} · ${r.filename} · ${Math.round(r.duration)} s`);
   button.onclick=()=>{void(async()=>{
    $<HTMLDialogElement>('search-dialog').close();selected.add(r.cameraId);dateInput.value=berlinDate(new Date(r.start*1000));page=0;renderCameras();await loadDay();seek(r.start);
    const cameraIndex=ordered().findIndex(c=>c.id===r.cameraId);if(cameraIndex>=0)page=Math.floor(cameraIndex/visiblePageCapacity());renderCameras();saveView();
   })().catch(e=>notify((e as Error).message))};
   root.append(button);
  }
  searchOffset=data.offset+data.results.length;
  $('search-count').hidden=data.total===0;
  $('search-count').textContent=data.total?`${searchOffset} von ${data.total} Treffern angezeigt`:'';
  if(!data.total)root.textContent='Keine Treffer im bisherigen Index. Weitere Tage öffnen oder Kameras scannen.';
  more.hidden=!data.hasMore;
  more.textContent=`Weitere ${Math.min(data.limit,data.total-searchOffset)} laden`;
 }catch(e){if(own===searchGeneration)$('search-error').textContent=(e as Error).message}
 finally{if(own===searchGeneration){more.disabled=false;$<HTMLButtonElement>('search-submit').disabled=false}}
}
$<HTMLFormElement>('search-form').onsubmit=e=>{
 e.preventDefault();searchGeneration++;searchOffset=0;
 try{
  const data=new FormData(e.currentTarget as HTMLFormElement);searchParams=new URLSearchParams();
  for(const[key,value]of data){if(!value)continue;searchParams.set(key,(key==='from'||key==='to')?String(berlinEpoch(String(value).length===16?String(value)+':00':String(value))):String(value))}
  $('search-results').replaceChildren();$('search-count').hidden=true;$<HTMLButtonElement>('search-more').hidden=true;
  void loadSearchPage();
 }catch(error){$('search-error').textContent=(error as Error).message}
};
on('search-more',()=>loadSearchPage(true));
on('trace-stop',()=>{tracing=false;$<HTMLInputElement>('trace-enabled').checked=false;$<HTMLButtonElement>('trace-stop').disabled=true;$<HTMLButtonElement>('trace-download').disabled=trace.length===0;$('trace-status').textContent='Gestoppt · '+trace.length+' Einträge'});
$<HTMLInputElement>('trace-enabled').onchange=()=>{tracing=$<HTMLInputElement>('trace-enabled').checked;if(tracing){trace=[];traceStart=performance.now();log('start')}else log('stop');$<HTMLButtonElement>('trace-stop').disabled=!tracing;$<HTMLButtonElement>('trace-download').disabled=tracing||trace.length===0;$('trace-status').textContent=tracing?'Aufzeichnung läuft (maximal 20.000 Einträge)':'Gestoppt'};
on('trace-download',async()=>{const status=await api('/api/playback-status');const blob=new Blob([JSON.stringify({version:1,status,events:trace},null,2)],{type:'application/json'});const url=URL.createObjectURL(blob),a=element('a');a.href=url;a.download='wiedergabediagnose.json';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000)});

$<HTMLSelectElement>('view-mode').onchange=cameraFormatChanged;
document.addEventListener('keydown',e=>{
 if(e.key==='Escape'&&document.fullscreenElement)void document.exitFullscreen().catch(()=>notify('Vollbild konnte nicht geschlossen werden.'));
 if(e.key==='Escape'&&!document.querySelector('dialog[open]'))document.querySelectorAll('.camera-tile.expanded').forEach(t=>{t.classList.remove('expanded');t.querySelector('.tile-expand')?.setAttribute('aria-label','Ansicht vergrößern')});
 const target=e.target instanceof HTMLElement?e.target:undefined;
 const tag=target?.tagName;
 if(tag==='INPUT'||tag==='SELECT'||tag==='TEXTAREA'||tag==='CANVAS'||target?.isContentEditable)return;
 if(target?.closest('button,a,summary,[role="button"],[role="link"]')&&!(e.key.toLowerCase()==='f'&&target.closest('.camera-tile')))return;
 if(e.defaultPrevented||e.ctrlKey||e.metaKey||e.altKey||document.querySelector('dialog[open]')||!state.shortcuts||!session)return;
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
function renderBatchSelection(){const root=$('batch-selected');root.replaceChildren();for(const [folder,name] of batchSelection){const row=element('label','batch-row',folder),input=element('input');input.value=name;input.maxLength=100;input.setAttribute('aria-label','Anzeigename für '+folder);input.oninput=()=>batchSelection.set(folder,input.value);const remove=element('button','text-button','Entfernen');remove.type='button';remove.onclick=()=>{batchSelection.delete(folder);renderBatchSelection();void browseBatch(batchFolder)};row.append(input,remove);root.append(row)}$<HTMLButtonElement>('batch-save').disabled=batchSelection.size===0}
let batchFolder='';
async function browseBatch(folder=''){batchFolder=folder;const data=await api<{path:string;folders:string[];limited:boolean}>('/api/cameras/folders?path='+encodeURIComponent(folder));const root=$('batch-browser');root.replaceChildren();root.append(element('strong','',data.path==='.'?'Aufnahmeverzeichnis':data.path));if(data.path!=='.'){const back=element('button','text-button','↑ Übergeordneter Ordner');back.type='button';back.onclick=()=>void browseBatch(data.path.split('/').slice(0,-1).join('/')).catch(e=>notify(e.message));root.append(back)}for(const path of data.folders){const row=element('div','batch-row'),check=element('input');check.type='checkbox';check.checked=batchSelection.has(path);check.disabled=cameras.some(c=>c.folder===path);check.setAttribute('aria-label','Kameraordner '+path+' auswählen');check.onchange=()=>{if(check.checked)batchSelection.set(path,path.split('/').pop()||path);else batchSelection.delete(path);renderBatchSelection()};const open=element('button','text-button',path.split('/').pop());open.type='button';open.onclick=()=>void browseBatch(path).catch(e=>notify(e.message));row.append(check,open);if(check.disabled)row.append(element('small','','Bereits eingerichtet'));root.append(row)}if(data.limited)root.append(element('small','','Suche begrenzt; bitte einen Unterordner öffnen.'))}
on('batch-open',async()=>{batchSelection.clear();$('batch-error').textContent='';renderBatchSelection();$<HTMLDialogElement>('batch-dialog').showModal();await browseBatch()});
on('batch-close',()=>$<HTMLDialogElement>('batch-dialog').close());
on('batch-save',async()=>{const button=$<HTMLButtonElement>('batch-save');button.disabled=true;try{const settings=cameraInput();const added=await api<(Camera&{detection?:ViewDetection})[]>('/api/cameras/batch',{method:'POST',body:JSON.stringify({date:dateInput.value,cameras:[...batchSelection].map(([folder,name])=>({...settings,id:'',folder,name}))})});$<HTMLDialogElement>('batch-dialog').close();resetCamera();dialog.classList.remove('camera-editing');dialog.close();await refreshCameras();await loadDay();const dual=added.filter(c=>c.viewMode==='dual').length,swapped=added.filter(c=>c.detection?.status==='swapped').length,unclear=added.filter(c=>c.detection?.status==='unclear').length;notify(dual?`Kameras hinzugefügt. ${swapped} Zuordnungen korrigiert${unclear?`, ${unclear} nicht sicher bestimmbar`:''}.`:'Kameras hinzugefügt. Bis zu drei Kameras werden gleichzeitig indexiert.')}catch(e){$('batch-error').textContent=(e as Error).message}finally{button.disabled=false}});

window.addEventListener('themechange',()=>{timelineKey='';overviewKey='';draw()});
