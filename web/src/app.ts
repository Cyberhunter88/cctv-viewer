import Hls from 'hls.js';
import {validView,readTimeLink,shouldFallback} from './view-state';
import {initArchiveUI} from './archive-ui';
import {attachTileZoom} from './tile-zoom';
import {attachTimelineControls} from './timeline-controls';
import {initManagement} from './management';
import {recordingAt,nextRecording,clamp,berlinDate,clock,type Recording} from './timeline';
interface Camera {id:string;name:string;folder:string;pattern:string;timezone:string;offset:number;format?:string;prefix?:string}
interface Session {preferenceKey:string;name:string;role:'admin'|'viewer';csrf:string;kind:'local'|'oidc'}
interface Scan {running:boolean;date:string;camera:string;indexed:number;warnings:string[]}
interface Day {recordings:Recording[];from:number;to:number;scan:Scan}
const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
let session:Session, cameras:Camera[]=[],records:Recording[]=[],selected=new Set<string>(),from=0,to=0,cursor=0,viewStart=0,viewEnd=0,playing=false,lastFrame=0,scanRunning=false,loadGeneration=0;
let thumbGeneration=0;const thumbURLs:string[]=[];
const tiles=new Map<string,Tile>();
const dateInput=$<HTMLInputElement>('date'),canvas=$<HTMLCanvasElement>('timeline');
const speed=()=>Number($<HTMLSelectElement>('speed').value);
let refreshRange=()=>{};let openExport:(range?:{from:number;to:number;camera?:string})=>void;
function saveView(){if(!session)return;try{localStorage.setItem('cctv-view:'+session.preferenceKey,JSON.stringify({cameras:[...selected],layout:$<HTMLSelectElement>('layout').value,quality:$<HTMLSelectElement>('quality').value,speed:$<HTMLSelectElement>('speed').value}))}catch{}}
function notify(message:string){$('toast').textContent=message;$('toast').hidden=false;setTimeout(()=>$('toast').hidden=true,6000)}
async function api<T>(url:string,options:RequestInit={}):Promise<T>{
 const headers=new Headers(options.headers);if(options.body)headers.set('Content-Type','application/json');if(session)headers.set('X-CSRF-Token',session.csrf);
 const response=await fetch(url,{...options,headers});if(response.status===401){pause();location.reload();throw new Error('Anmeldung abgelaufen')}
 const body=await response.json();if(!response.ok)throw Object.assign(new Error((body.error||`Anfrage fehlgeschlagen (${response.status})`)+(body.gaps?'\n'+body.gaps.map((g:number[])=>clock(g[0])+' – '+clock(g[1])).join('\n'):'')),{indexing:body.indexing===true});return body as T;
}
function on(id:string,fn:()=>void|Promise<void>){$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>notify(e.message))})}
function element<K extends keyof HTMLElementTagNameMap>(tag:K,className='',text=''):HTMLElementTagNameMap[K]{const e=document.createElement(tag);e.className=className;e.textContent=text;return e}
function visible():Camera[]{return cameras.filter(c=>selected.has(c.id)).slice(0,Number($<HTMLSelectElement>('layout').value))}
function setMessage(tile:Tile,message:string,error=false){tile.message.replaceChildren();if(message){tile.message.append(element('span','symbol',error?'⚠':'▧'),element('span','',message))}tile.message.hidden=!message;tile.message.classList.toggle('error',error)}
class Tile {
 audioButton=element('button','icon-button','🔇');volumeSlider=element('input');
 video=element('video');container=element('div','camera-tile');message=element('div','tile-message');timestamp=element('span');codec=element('small');hls?:Hls;stream?:string;record?:Recording;loading=false;failed=false;epoch=0;offset=0;abort?:AbortController;fallbackRecord?:string;disposeZoom:()=>void=()=>{};
 constructor(public camera:Camera){
  this.video.muted=true;this.video.playsInline=true;this.video.preload='metadata';
  const audioControls=element('div','tile-audio');this.audioButton.type='button';this.volumeSlider.type='range';this.volumeSlider.min='0';this.volumeSlider.max='100';this.volumeSlider.step='5';this.volumeSlider.value='100';this.volumeSlider.setAttribute('aria-label',camera.name+' Lautstärke');
  this.audioButton.onclick=()=>this.chooseAudio(this.video.muted);this.volumeSlider.oninput=()=>{this.video.volume=Number(this.volumeSlider.value)/100;if(this.video.volume===0)this.chooseAudio(false)};
  audioControls.addEventListener('keydown',e=>{if(e.key!=='Escape')e.stopPropagation()});audioControls.append(this.audioButton,this.volumeSlider);this.setAudio(false);
  const header=element('div','tile-header'),title=element('span','tile-title');title.append(element('i','secure-dot'),document.createTextNode(camera.name));header.append(title,this.codec);
  const footer=element('div','tile-footer');footer.append(this.timestamp,element('span','','AUFNAHME'));
  this.container.append(this.video,header,this.message,footer,audioControls);this.disposeZoom=attachTileZoom(this.container,this.video,camera.name);
  this.video.addEventListener('error',()=>{const error=this.video.error,record=this.record;if(!error||!record||this.loading)return;if(shouldFallback($<HTMLSelectElement>('quality').value,error.code,this.fallbackRecord===record.id,!!this.stream||!!this.hls)){this.fallbackRecord=record.id;notify(camera.name+': Originalformat nicht unterstützt. Wechsel zu kompatibler Wiedergabe.');void this.load(record,true)}else this.fail(error.code===2?'Netzwerkfehler. Pause und Abspielen zum Wiederholen.':'Wiedergabe unterbrochen. Pause und Abspielen zum Wiederholen.')});
  this.video.addEventListener('loadedmetadata',()=>this.align());
  this.video.addEventListener('canplay',()=>{if(this.record&&!this.loading&&!this.video.error){this.failed=false;setMessage(this,'');this.align()}});
  this.video.addEventListener('ended',()=>{if(this.stream&&this.record&&cursor<this.record.start+this.record.duration-1){this.record=undefined;this.update()}});
 }
 setAudio(enabled:boolean){this.video.muted=!enabled;this.audioButton.textContent=enabled?'🔊':'🔇';this.audioButton.setAttribute('aria-pressed',String(enabled));this.audioButton.setAttribute('aria-label',this.camera.name+(enabled?' Ton ausschalten':' Ton einschalten'));this.audioButton.title=enabled?'Ton ausschalten':'Ton einschalten, sofern die Aufnahme eine Audiospur hat';this.volumeSlider.hidden=!enabled}
 chooseAudio(enabled:boolean){if(enabled){for(const tile of tiles.values())tile.setAudio(false);if(this.video.volume===0){this.video.volume=1;this.volumeSlider.value='100'}}this.setAudio(enabled);if(enabled&&playing&&this.record)void this.video.play().catch(()=>notify('Tonwiedergabe konnte nicht gestartet werden. Erneut Abspielen wählen.'))}
 fail(message:string){this.failed=true;setMessage(this,message,true)}
 release(){this.epoch++;this.abort?.abort();this.abort=undefined;this.hls?.destroy();this.hls=undefined;this.video.pause();this.video.removeAttribute('src');this.video.load();if(this.stream){const id=this.stream;this.stream=undefined;void api('/api/streams/'+id,{method:'DELETE'}).catch(()=>{})}this.loading=false;this.record=undefined;this.failed=false;this.offset=0}
 destroy(){this.setAudio(false);this.disposeZoom();this.release();this.container.remove()}
 async load(record:Recording,forceCompatible=false){
  this.release();this.record=record;const epoch=this.epoch;this.loading=true;setMessage(this,'Aufnahme wird geladen …');this.codec.textContent=record.codec==='hevc'?'H.265':record.codec.toUpperCase();
  const quality=$<HTMLSelectElement>('quality').value;const native=!forceCompatible&&this.fallbackRecord!==record.id&&(quality==='native'||(quality==='auto'&&['.mp4','.mov'].includes(record.container||'')&&(record.codec==='h264'||(record.codec==='hevc'&&!!this.video.canPlayType('video/mp4; codecs="hvc1"')))));
  try{
   if(native){this.video.src='/api/recordings/'+record.id+'/file';this.offset=0}
   else{
    this.abort=new AbortController();const stream=await api<{id:string;url:string;offset:number}>('/api/streams',{method:'POST',body:JSON.stringify({recording:record.id,offset:Math.max(0,cursor-record.start)}),signal:this.abort.signal});
    if(epoch!==this.epoch){void api('/api/streams/'+stream.id,{method:'DELETE'}).catch(()=>{});return}
    this.stream=stream.id;this.offset=stream.offset;
    if(Hls.isSupported()){
     const hls=new Hls({maxBufferLength:20,maxMaxBufferLength:40,backBufferLength:10,liveSyncDurationCount:9999,liveMaxLatencyDurationCount:10000});this.hls=hls;
     hls.on(Hls.Events.ERROR,(_event,data)=>{if(data.fatal)this.fail('Streaming unterbrochen. Pause und Abspielen zum Wiederholen.')});hls.loadSource(stream.url);hls.attachMedia(this.video);
    }else if(this.video.canPlayType('application/vnd.apple.mpegurl'))this.video.src=stream.url;
    else throw new Error('Dieser Browser unterstützt keine HLS-Wiedergabe.');
   }
   if(epoch!==this.epoch)return;this.loading=false;setMessage(this,'');this.align();
  }catch(e){if(epoch!==this.epoch)return;this.loading=false;if((e as Error).name!=='AbortError')this.fail((e as Error).message)}
 }
 align(){if(this.failed&&this.video.readyState>=2&&!this.video.error){this.failed=false;setMessage(this,'')}if(!this.record||this.loading||this.failed)return;const target=cursor-this.record.start-this.offset;
  if(target<-.5){this.record=undefined;this.update();return}
  if(this.video.readyState>=1&&Math.abs(this.video.currentTime-target)>.65){try{this.video.currentTime=Math.max(0,target)}catch{}}
  this.video.playbackRate=speed();if(playing){void this.video.play().catch(()=>{})}else this.video.pause();
 }
 update(){this.timestamp.textContent=clock(cursor);const record=recordingAt(records,this.camera.id,cursor);
  if(!record){if(this.record)this.release();setMessage(this,'Keine Aufnahme zu diesem Zeitpunkt');return}
  if(this.record?.id!==record.id){void this.load(record);return}this.align();
 }
 retry(){if(this.failed){const record=this.record;this.fallbackRecord=undefined;this.release();if(record)void this.load(record)}}
}
function pause(){playing=false;$('play').textContent='▶';$('play').setAttribute('aria-label','Abspielen');tiles.forEach(t=>t.video.pause())}
function play(){if(!records.length||!visible().length){notify('Für diese Auswahl sind keine Aufnahmen vorhanden.');return}if(!visible().some(c=>recordingAt(records,c.id,cursor))){const next=nextRecording(records,visible().map(c=>c.id),cursor);if(next===undefined){notify('Keine weiteren Aufnahmen an diesem Tag.');return}cursor=next}playing=true;lastFrame=performance.now();$('play').textContent='Ⅱ';$('play').setAttribute('aria-label','Pause');tiles.forEach(t=>t.retry());update()}
function seek(time:number){cursor=clamp(time,from,to-.001);update()}
function update(){if(!from)return;$('clock').textContent=clock(cursor);tiles.forEach(t=>t.update());draw();refreshRange()}
function frame(now:number){if(playing){cursor+=(now-lastFrame)/1000*speed();if(cursor>=to){cursor=to-.001;pause()}else if(!visible().some(c=>recordingAt(records,c.id,cursor))){const next=nextRecording(records,visible().map(c=>c.id),cursor);if(next===undefined)pause();else cursor=next}if(cursor>viewEnd&&viewEnd-viewStart<to-from){const width=viewEnd-viewStart;viewStart=clamp(cursor-width/4,from,to-width);viewEnd=viewStart+width}update()}lastFrame=now;requestAnimationFrame(frame)}
function renderCameras(){
 const filters=$('camera-filters');filters.replaceChildren();for(const camera of cameras){const b=element('button','filter'+(selected.has(camera.id)?' selected':''));b.setAttribute('aria-pressed',String(selected.has(camera.id)));b.append(element('i'),document.createTextNode(camera.name));b.onclick=()=>{if(selected.has(camera.id))selected.delete(camera.id);else selected.add(camera.id);imagePage=0;renderCameras();saveView()};filters.append(b)}
 const active=visible();for(const[id,t]of tiles){if(!active.some(c=>c.id===id)){t.destroy();tiles.delete(id)}}
 const grid=$('video-grid');for(const c of active){if(!tiles.has(c.id))tiles.set(c.id,new Tile(c));grid.append(tiles.get(c.id)!.container)}grid.className='video-grid'+($<HTMLSelectElement>('layout').value==='1'?' single':$<HTMLSelectElement>('layout').value==='9'?' nine':'');
 $('empty').hidden=cameras.length!==0;$('timeline-section').hidden=cameras.length===0;$('add-first').hidden=session.role!=='admin';update();renderImages();void thumbnails();
}
function setScan(scan:Scan){scanRunning=scan.running;$('scan-status').textContent=scan.running?`Indexierung · ${scan.camera||'wird vorbereitet'}`:'Index bereit';$('index-dot').classList.toggle('busy',scan.running);$<HTMLButtonElement>('rescan').disabled=scan.running;$('warnings').hidden=!scan.warnings?.length;$('warnings').textContent=(scan.warnings||[]).join('\n')}
function showImage(record:Recording){pause();const modal=document.createElement('dialog'),title=element('h2','',(cameras.find(c=>c.id===record.cameraId)?.name||'Kamera')+' · '+clock(record.start)),image=element('img'),close=element('button','button secondary','Schließen'),download=element('a','button primary','Bild herunterladen');image.src='/api/recordings/'+record.id+'/file';image.alt=title.textContent||'Einzelbild';image.style.maxWidth='100%';download.href=image.src+'?download=1';close.onclick=()=>modal.close();modal.append(title,image,download,close);modal.addEventListener('close',()=>modal.remove());document.body.append(modal);modal.showModal()}
let imagePage=0;
function renderImages(){const root=$('image-events');root.replaceChildren();const images=records.filter(r=>r.kind==='image'&&selected.has(r.cameraId));imagePage=clamp(imagePage,0,Math.max(0,Math.ceil(images.length/100)-1));if(!images.length)return;root.append(element('h2','','Einzelbilder'));const list=element('div','image-event-list');for(const r of images.slice(imagePage*100,imagePage*100+100)){const b=element('button','button secondary',(cameras.find(c=>c.id===r.cameraId)?.name||'Kamera')+' · '+clock(r.start));b.onclick=()=>showImage(r);list.append(b)}root.append(list);if(images.length>100){const prev=element('button','text-button','Vorherige Bilder'),next=element('button','text-button','Weitere Bilder');prev.disabled=imagePage===0;next.disabled=(imagePage+1)*100>=images.length;prev.onclick=()=>{imagePage--;renderImages()};next.onclick=()=>{imagePage++;renderImages()};root.append(prev,element('span','',` Seite ${imagePage+1} von ${Math.ceil(images.length/100)} `),next)}}
async function loadDay(reset=true){const generation=++loadGeneration;if(reset){imagePage=0;pause();tiles.forEach(t=>t.release())}const data=await api<Day>('/api/day?date='+encodeURIComponent(dateInput.value));if(generation!==loadGeneration)return;records=data.recordings;from=data.from;to=data.to;setScan(data.scan);if(reset){cursor=records.length?Math.max(from,Math.min(...records.map(r=>r.start))):from;viewStart=from;viewEnd=to}$('recording-count').textContent=`${records.length} Aufnahmen · ${cameras.length} Kameras`;update();renderImages();void thumbnails();refreshRange()}
async function refreshCameras(){const previous=new Set(cameras.map(c=>c.id));cameras=await api<Camera[]>('/api/cameras');for(const c of cameras)if(!previous.has(c.id))selected.add(c.id);for(const id of selected)if(!cameras.some(c=>c.id===id))selected.delete(id);renderCameras();renderCameraList()}
function draw(){if(!from)return;const cams=visible(),width=canvas.parentElement!.clientWidth,height=48+Math.max(1,cams.length)*34,dpr=devicePixelRatio||1;
 if(canvas.width!==Math.round(width*dpr)||canvas.height!==Math.round(height*dpr)){canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);canvas.style.height=height+'px'}
 const ctx=canvas.getContext('2d')!;ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,width,height);const left=85,right=width-15,area=right-left,span=viewEnd-viewStart,x=(t:number)=>left+(t-viewStart)/span*area;
 const intervals=[60,300,600,1800,3600,7200,14400,21600];const interval=intervals.find(s=>span/s<area/65)||21600;
 ctx.font='9px system-ui';ctx.textAlign='center';for(let t=Math.ceil(viewStart/interval)*interval;t<=viewEnd;t+=interval){const p=x(t);ctx.strokeStyle='#25313a';ctx.beginPath();ctx.moveTo(p,30);ctx.lineTo(p,height-10);ctx.stroke();ctx.fillStyle='#758997';ctx.fillText(clock(t).slice(0,5),p,19)}
 cams.forEach((camera,i)=>{const y=39+i*34;ctx.fillStyle='#7d909c';ctx.textAlign='left';ctx.fillText(camera.name.length>11?camera.name.slice(0,10)+'…':camera.name,12,y+14);ctx.fillStyle='#1b2730';ctx.fillRect(left,y,area,23);
  ctx.save();ctx.beginPath();ctx.rect(left,y,area,23);ctx.clip();for(const r of records.filter(r=>r.cameraId===camera.id&&r.start<viewEnd&&(r.kind==='image'?r.start>=viewStart:r.start+r.duration>viewStart))){ctx.fillStyle=r.kind==='image'?'#e4b86c':'#709f80';ctx.fillRect(x(r.start),y+3,r.kind==='image'?4:Math.max(2,r.duration/span*area),17)}ctx.restore();
 });
 if(cursor>=viewStart&&cursor<=viewEnd){const p=x(cursor);ctx.strokeStyle='#c8f9d5';ctx.lineWidth=1.5;ctx.beginPath();ctx.moveTo(p,29);ctx.lineTo(p,height-8);ctx.stroke();ctx.fillStyle='#c8f9d5';ctx.beginPath();ctx.moveTo(p-4,27);ctx.lineTo(p+4,27);ctx.lineTo(p,33);ctx.fill()}
 const hours=span/3600;$('zoom-label').textContent=hours>=1?`${Math.round(hours*10)/10} Std.`:`${Math.round(span/60)} Min.`;
}
function zoom(factor:number,anchor=cursor){const span=clamp((viewEnd-viewStart)*factor,300,to-from);viewStart=clamp(anchor-span/2,from,to-span);viewEnd=viewStart+span;draw();refreshRange()}
new ResizeObserver(()=>{draw();refreshRange()}).observe(canvas.parentElement!);
async function thumbnails(){const generation=++thumbGeneration;for(const url of thumbURLs)URL.revokeObjectURL(url);thumbURLs.length=0;const root=$('thumbnails');root.replaceChildren();const ids=visible().map(c=>c.id),items=records.filter(r=>ids.includes(r.cameraId)).sort((a,b)=>a.start-b.start);const sample=items.filter((_,i)=>i%Math.max(1,Math.ceil(items.length/8))===0).slice(0,8);
 for(const record of sample){if(generation!==thumbGeneration)return;const button=element('button','thumb'),img=element('img'),label=element('span','',clock(record.start));img.alt='Vorschau '+clock(record.start);button.title=clock(record.start);button.append(img,label);button.onclick=()=>record.kind==='image'?showImage(record):seek(record.start);root.append(button);
  try{const response=await fetch('/api/recordings/'+record.id+'/thumbnail');if(!response.ok)continue;const blob=await response.blob();if(generation!==thumbGeneration)return;const url=URL.createObjectURL(blob);thumbURLs.push(url);img.src=url}catch{}
 }
}
function cameraFormatChanged(){const reolink=$<HTMLSelectElement>('camera-format').value==='reolink';$('pattern-label').hidden=reolink;const pattern=cameraForm.elements.namedItem('pattern') as HTMLInputElement;pattern.required=!reolink}
const dialog=$<HTMLDialogElement>('camera-dialog'),cameraForm=$<HTMLFormElement>('camera-form');
function cameraInput():Camera{const data=new FormData(cameraForm);return{id:String(data.get('id')||''),name:String(data.get('name')),folder:String(data.get('folder')),pattern:String(data.get('pattern')),timezone:String(data.get('timezone')),offset:Number(data.get('offset')),format:String(data.get('format'))}}
function resetCamera(){cameraForm.reset();(cameraForm.elements.namedItem('id') as HTMLInputElement).value='';$('preview-result').hidden=true;$('camera-error').textContent='';cameraFormatChanged()}
function renderCameraList(){const list=$('camera-list');list.replaceChildren();for(const c of cameras){const entry=element('div','camera-entry'),edit=element('button','',c.name),remove=element('button','delete','×');remove.title='Kamera entfernen';edit.onclick=()=>{for(const[key,value]of Object.entries(c)){const input=cameraForm.elements.namedItem(key);if(input instanceof HTMLInputElement||input instanceof HTMLSelectElement)input.value=String(value)}cameraFormatChanged()};remove.onclick=async()=>{if(!confirm(`Kamera „${c.name}“ aus dem Viewer entfernen? Die Originalaufnahmen bleiben erhalten.`))return;try{await api('/api/cameras/'+c.id,{method:'DELETE'});resetCamera();await refreshCameras();await loadDay()}catch(e){$('camera-error').textContent=(e as Error).message}};entry.append(edit,remove);list.append(entry)}}
cameraForm.addEventListener('submit',async e=>{e.preventDefault();const submit=cameraForm.querySelector<HTMLButtonElement>('button[type=submit]')!;submit.disabled=true;try{await api('/api/cameras',{method:'POST',body:JSON.stringify(cameraInput())});resetCamera();await refreshCameras();await loadDay();notify('Kamera gespeichert. Aufnahmen werden eingelesen.')}catch(e){$('camera-error').textContent=(e as Error).message}finally{submit.disabled=false}});
on('preview',async()=>{try{const result=await api<{examples:{file:string;time?:string;error?:string}[];limited:boolean}>('/api/cameras/preview',{method:'POST',body:JSON.stringify(cameraInput())});$('preview-result').hidden=false;$('preview-result').textContent=result.examples.length?result.examples.map(v=>`${v.file}\n→ ${v.error||v.time}`).join('\n\n'):'Keine passenden Videodateien gefunden.';if(result.limited)$('preview-result').textContent+='\nSuche auf 10.000 Einträge begrenzt.'}catch(e){$('camera-error').textContent=(e as Error).message}});
$<HTMLSelectElement>('camera-format').addEventListener('change',cameraFormatChanged);
async function browseFolder(folder=''){const data=await api<{path:string;folders:string[];samples:string[];limited:boolean}>('/api/cameras/folders?path='+encodeURIComponent(folder));const root=$('folder-browser');root.hidden=false;root.replaceChildren();root.append(element('strong','',data.path==='.'?'Aufnahmeverzeichnis':data.path));if(data.path!=='.'){const back=element('button','text-button','↑ Übergeordneter Ordner');back.type='button';back.onclick=()=>void browseFolder(data.path.split('/').slice(0,-1).join('/')).catch(e=>notify(e.message));root.append(back);const choose=element('button','button secondary','Diesen Ordner verwenden');choose.type='button';choose.onclick=()=>{(cameraForm.elements.namedItem('folder') as HTMLInputElement).value=data.path;const name=data.path.split('/').pop()||'';(cameraForm.elements.namedItem('name') as HTMLInputElement).value=name;const sample=data.samples.find(s=>/_[0-9]+_[0-9]{14}\.(mp4|jpg|jpeg)$/i.test(s));if(sample){$<HTMLSelectElement>('camera-format').value='reolink';cameraFormatChanged()}root.hidden=true};root.append(choose)}for(const folder of data.folders){const b=element('button','text-button',folder.split('/').pop());b.type='button';b.onclick=()=>void browseFolder(folder).catch(e=>notify(e.message));root.append(b)}if(data.limited)root.append(element('small','','Suche begrenzt; bitte einen Unterordner wählen.'))}
on('browse-folder',()=>browseFolder());
on('new-camera',resetCamera);on('close-dialog',()=>dialog.close());on('settings-button',()=>dialog.showModal());on('add-first',()=>dialog.showModal());
on('play',()=>playing?pause():play());on('skip-back',()=>seek(cursor-10));on('skip-forward',()=>seek(cursor+10));on('zoom-in',()=>zoom(.5));on('zoom-out',()=>zoom(2));on('reset-zoom',()=>{viewStart=from;viewEnd=to;draw();refreshRange()});
function changeDate(delta:number){const d=new Date(dateInput.value+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+delta);dateInput.value=d.toISOString().slice(0,10);return loadDay()}
on('prev-day',()=>changeDate(-1));on('next-day',()=>changeDate(1));on('today',()=>{dateInput.value=berlinDate(new Date());return loadDay()});dateInput.addEventListener('change',()=>void loadDay().catch(e=>notify(e.message)));
$<HTMLSelectElement>('layout').addEventListener('change',()=>{renderCameras();saveView()});$<HTMLSelectElement>('quality').addEventListener('change',()=>{tiles.forEach(t=>{t.fallbackRecord=undefined;t.release()});update();saveView()});$<HTMLSelectElement>('speed').addEventListener('change',()=>{update();saveView()});
on('rescan',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value})}))});
on('logout',async()=>{pause();tiles.forEach(t=>t.release());await api('/api/logout',{method:'POST'});location.reload()});
window.addEventListener('pagehide',()=>{pause();tiles.forEach(t=>t.release())});document.addEventListener('visibilitychange',()=>{if(document.hidden){pause();tiles.forEach(t=>t.release())}});
const loginForm=$<HTMLFormElement>('login-form');loginForm.addEventListener('submit',async e=>{e.preventDefault();const button=loginForm.querySelector('button')!;button.disabled=true;try{const data=new FormData(loginForm);const res=await fetch('/auth/local',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(data))});const body=await res.json();if(!res.ok)throw new Error(body.error);location.reload()}catch(e){$('login-error').textContent=(e as Error).message}finally{button.disabled=false}});
async function boot(){const response=await fetch('/api/session');if(!response.ok){try{if(location.search)sessionStorage.setItem('cctv-pending-link',location.search)}catch{}$('login').hidden=false;const methods=await api<{local:boolean;oidc:boolean}>('/auth/methods');loginForm.hidden=!methods.local;$('oidc-login').hidden=!methods.oidc;$('login-divider').hidden=!(methods.local&&methods.oidc);return}
 session=await response.json();$('password-button').hidden=session.kind!=='local';openExport=initManagement({api,getSession:()=>session,getCameras:()=>cameras,getCursor:()=>cursor,notify});$('app').hidden=false;$('user-name').textContent=session.name;$('avatar').textContent=session.name.slice(0,2).toUpperCase();$('settings-button').hidden=session.role!=='admin';$('rescan').hidden=session.role!=='admin';dateInput.value=berlinDate(new Date());await refreshCameras();
 let stored:unknown;try{stored=JSON.parse(localStorage.getItem('cctv-view:'+session.preferenceKey)||'null')}catch{}
 const saved=validView(stored,cameras.map(c=>c.id));for(const id of ['layout','quality','speed'] as const)if(saved[id])$<HTMLSelectElement>(id).value=saved[id]!;
 if(saved.cameras)selected=new Set(saved.cameras);let pending=location.search;try{if(!pending)pending=sessionStorage.getItem('cctv-pending-link')||'';sessionStorage.removeItem('cctv-pending-link')}catch{}const link=readTimeLink(pending,cameras.map(c=>c.id));if(link.cameras)selected=new Set(link.cameras);if(link.date)dateInput.value=link.date;renderCameras();await loadDay();if(link.time!==undefined)seek(link.time);
 refreshRange=attachTimelineControls(canvas,{bounds:()=>({from,to,cursor,viewStart,viewEnd}),seek,zoom,pan:(start,end)=>{viewStart=start;viewEnd=end;draw();refreshRange()},pause,export:range=>openExport({...range,camera:visible()[0]?.id}),imageAt:(time,row)=>{const camera=visible()[row];const image=camera?records.filter(r=>r.kind==='image'&&r.cameraId===camera.id&&Math.abs(r.start-time)<(viewEnd-viewStart)/Math.max(1,canvas.clientWidth-100)*6).sort((a,b)=>Math.abs(a.start-time)-Math.abs(b.start-time))[0]:undefined;if(image){showImage(image);return true}return false}});
 initArchiveUI({api,date:()=>dateInput.value,cameras:()=>cameras,selected:()=>[...selected],cursor:()=>cursor,notify,open:async(date,time,ids)=>{if(ids){selected=new Set(ids.filter(id=>cameras.some(c=>c.id===id)));if(selected.size===0)notify('Die Kameras dieses Lesezeichens sind nicht mehr vorhanden.');renderCameras();saveView()}dateInput.value=date;await loadDay();if(time!==undefined){seek(time);const matches=records.filter(r=>selected.has(r.cameraId)&&(r.kind==='image'?Math.abs(r.start-time)<1:r.start<=time&&r.start+r.duration>time));if(!matches.length)notify('Für diesen gespeicherten Zeitpunkt ist keine Aufnahme vorhanden.');else{const sources=await Promise.all(matches.map(r=>fetch('/api/recordings/'+r.id+'/file',{method:'HEAD'})));if(sources.some(r=>r.status===404))notify('Eine Aufnahme dieses Lesezeichens ist nicht mehr verfügbar. Index gegebenenfalls aktualisieren.');}}}});requestAnimationFrame(frame);
 setInterval(async()=>{try{const scan=await api<Scan>('/api/scan');const wasRunning=scanRunning;setScan(scan);if(wasRunning&&!scan.running)await loadDay(false)}catch(e){notify((e as Error).message)}},4000);
}
void boot().catch(e=>notify(e.message));
