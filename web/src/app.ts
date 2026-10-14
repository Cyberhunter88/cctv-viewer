import Hls from 'hls.js';
import {recordingAt,nextRecording,clamp,berlinDate,clock,type Recording} from './timeline';
interface Camera {id:string;name:string;folder:string;pattern:string;timezone:string;offset:number}
interface Session {name:string;role:'admin'|'viewer';csrf:string}
interface Scan {running:boolean;date:string;camera:string;indexed:number;warnings:string[]}
interface Day {recordings:Recording[];from:number;to:number;scan:Scan}
const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
let session:Session, cameras:Camera[]=[],records:Recording[]=[],selected=new Set<string>(),from=0,to=0,cursor=0,viewStart=0,viewEnd=0,playing=false,lastFrame=0,scanRunning=false,loadGeneration=0;
let thumbGeneration=0;const thumbURLs:string[]=[];
const tiles=new Map<string,Tile>();
const dateInput=$<HTMLInputElement>('date'),canvas=$<HTMLCanvasElement>('timeline');
const speed=()=>Number($<HTMLSelectElement>('speed').value);
function notify(message:string){$('toast').textContent=message;$('toast').hidden=false;setTimeout(()=>$('toast').hidden=true,6000)}
async function api<T>(url:string,options:RequestInit={}):Promise<T>{
 const headers=new Headers(options.headers);if(options.body)headers.set('Content-Type','application/json');if(session)headers.set('X-CSRF-Token',session.csrf);
 const response=await fetch(url,{...options,headers});if(response.status===401){pause();location.reload();throw new Error('Anmeldung abgelaufen')}
 const body=await response.json();if(!response.ok)throw new Error(body.error||`Anfrage fehlgeschlagen (${response.status})`);return body as T;
}
function on(id:string,fn:()=>void|Promise<void>){$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>notify(e.message))})}
function element<K extends keyof HTMLElementTagNameMap>(tag:K,className='',text=''):HTMLElementTagNameMap[K]{const e=document.createElement(tag);e.className=className;e.textContent=text;return e}
function visible():Camera[]{return cameras.filter(c=>selected.has(c.id)).slice(0,Number($<HTMLSelectElement>('layout').value))}
function setMessage(tile:Tile,message:string,error=false){tile.message.replaceChildren();if(message){tile.message.append(element('span','symbol',error?'⚠':'▧'),element('span','',message))}tile.message.hidden=!message;tile.message.classList.toggle('error',error)}
class Tile {
 video=element('video');container=element('div','camera-tile');message=element('div','tile-message');timestamp=element('span');codec=element('small');hls?:Hls;stream?:string;record?:Recording;loading=false;failed=false;epoch=0;offset=0;abort?:AbortController;
 constructor(public camera:Camera){
  this.video.muted=true;this.video.playsInline=true;this.video.preload='metadata';
  const header=element('div','tile-header'),title=element('span','tile-title');title.append(element('i','secure-dot'),document.createTextNode(camera.name));header.append(title,this.codec);
  const footer=element('div','tile-footer');footer.append(this.timestamp,element('span','','AUFNAHME'));
  this.container.append(this.video,header,this.message,footer);
  this.video.addEventListener('error',()=>{if(this.record&&!this.hls)this.fail('Originalformat nicht unterstützt. „Kompatibel“ wählen.')});
  this.video.addEventListener('loadedmetadata',()=>this.align());
  this.video.addEventListener('ended',()=>{if(this.stream&&this.record&&cursor<this.record.start+this.record.duration-1){this.record=undefined;this.update()}});
 }
 fail(message:string){this.failed=true;setMessage(this,message,true)}
 release(){this.epoch++;this.abort?.abort();this.abort=undefined;this.hls?.destroy();this.hls=undefined;this.video.pause();this.video.removeAttribute('src');this.video.load();if(this.stream){const id=this.stream;this.stream=undefined;void api('/api/streams/'+id,{method:'DELETE'}).catch(()=>{})}this.loading=false;this.record=undefined;this.failed=false;this.offset=0}
 destroy(){this.release();this.container.remove()}
 async load(record:Recording){
  this.release();this.record=record;const epoch=this.epoch;this.loading=true;setMessage(this,'Aufnahme wird geladen …');this.codec.textContent=record.codec==='hevc'?'H.265':record.codec.toUpperCase();
  const quality=$<HTMLSelectElement>('quality').value;const native=quality==='native'||(quality==='auto'&&['.mp4','.mov'].includes(record.container||'')&&(record.codec==='h264'||(record.codec==='hevc'&&!!this.video.canPlayType('video/mp4; codecs="hvc1"'))));
  try{
   if(native){this.video.src='/api/recordings/'+record.id+'/file';this.offset=0}
   else{
    this.abort=new AbortController();const stream=await api<{id:string;url:string;offset:number}>('/api/streams',{method:'POST',body:JSON.stringify({recording:record.id,offset:Math.max(0,cursor-record.start)}),signal:this.abort.signal});
    if(epoch!==this.epoch){void api('/api/streams/'+stream.id,{method:'DELETE'}).catch(()=>{});return}
    this.stream=stream.id;this.offset=stream.offset;
    if(this.video.canPlayType('application/vnd.apple.mpegurl'))this.video.src=stream.url;
    else if(Hls.isSupported()){
     const hls=new Hls({maxBufferLength:20,maxMaxBufferLength:40,backBufferLength:10,liveSyncDurationCount:9999,liveMaxLatencyDurationCount:10000});this.hls=hls;
     hls.on(Hls.Events.ERROR,(_event,data)=>{if(data.fatal)this.fail('Streaming unterbrochen. Pause und Abspielen zum Wiederholen.')});hls.loadSource(stream.url);hls.attachMedia(this.video);
    }else throw new Error('Dieser Browser unterstützt keine HLS-Wiedergabe.');
   }
   if(epoch!==this.epoch)return;this.loading=false;setMessage(this,'');this.align();
  }catch(e){if(epoch!==this.epoch)return;this.loading=false;if((e as Error).name!=='AbortError')this.fail((e as Error).message)}
 }
 align(){if(!this.record||this.loading||this.failed)return;const target=cursor-this.record.start-this.offset;
  if(target<-.5){this.record=undefined;this.update();return}
  if(this.video.readyState>=1&&Math.abs(this.video.currentTime-target)>.65){try{this.video.currentTime=Math.max(0,target)}catch{}}
  this.video.playbackRate=speed();if(playing){void this.video.play().catch(()=>{})}else this.video.pause();
 }
 update(){this.timestamp.textContent=clock(cursor);const record=recordingAt(records,this.camera.id,cursor);
  if(!record){if(this.record)this.release();setMessage(this,'Keine Aufnahme zu diesem Zeitpunkt');return}
  if(this.record?.id!==record.id){void this.load(record);return}this.align();
 }
 retry(){if(this.failed){const record=this.record;this.release();if(record)void this.load(record)}}
}
function pause(){playing=false;$('play').textContent='▶';$('play').setAttribute('aria-label','Abspielen');tiles.forEach(t=>t.video.pause())}
function play(){if(!records.length||!visible().length){notify('Für diese Auswahl sind keine Aufnahmen vorhanden.');return}if(!visible().some(c=>recordingAt(records,c.id,cursor))){const next=nextRecording(records,visible().map(c=>c.id),cursor);if(next===undefined){notify('Keine weiteren Aufnahmen an diesem Tag.');return}cursor=next}playing=true;lastFrame=performance.now();$('play').textContent='Ⅱ';$('play').setAttribute('aria-label','Pause');tiles.forEach(t=>t.retry());update()}
function seek(time:number){cursor=clamp(time,from,to-.001);update()}
function update(){if(!from)return;$('clock').textContent=clock(cursor);tiles.forEach(t=>t.update());draw()}
function frame(now:number){if(playing){cursor+=(now-lastFrame)/1000*speed();if(cursor>=to){cursor=to-.001;pause()}else if(!visible().some(c=>recordingAt(records,c.id,cursor))){const next=nextRecording(records,visible().map(c=>c.id),cursor);if(next===undefined)pause();else cursor=next}if(cursor>viewEnd&&viewEnd-viewStart<to-from){const width=viewEnd-viewStart;viewStart=clamp(cursor-width/4,from,to-width);viewEnd=viewStart+width}update()}lastFrame=now;requestAnimationFrame(frame)}
function renderCameras(){
 const filters=$('camera-filters');filters.replaceChildren();for(const camera of cameras){const b=element('button','filter'+(selected.has(camera.id)?' selected':''));b.setAttribute('aria-pressed',String(selected.has(camera.id)));b.append(element('i'),document.createTextNode(camera.name));b.onclick=()=>{if(selected.has(camera.id))selected.delete(camera.id);else selected.add(camera.id);renderCameras()};filters.append(b)}
 const active=visible();for(const[id,t]of tiles){if(!active.some(c=>c.id===id)){t.destroy();tiles.delete(id)}}
 const grid=$('video-grid');for(const c of active){if(!tiles.has(c.id))tiles.set(c.id,new Tile(c));grid.append(tiles.get(c.id)!.container)}grid.className='video-grid'+($<HTMLSelectElement>('layout').value==='1'?' single':$<HTMLSelectElement>('layout').value==='9'?' nine':'');
 $('empty').hidden=cameras.length!==0;$('timeline-section').hidden=cameras.length===0;$('add-first').hidden=session.role!=='admin';update();void thumbnails();
}
function setScan(scan:Scan){scanRunning=scan.running;$('scan-status').textContent=scan.running?`Indexierung · ${scan.camera||'wird vorbereitet'}`:'Index bereit';$('index-dot').classList.toggle('busy',scan.running);$<HTMLButtonElement>('rescan').disabled=scan.running;$('warnings').hidden=!scan.warnings?.length;$('warnings').textContent=(scan.warnings||[]).join('\n')}
async function loadDay(reset=true){const generation=++loadGeneration;if(reset){pause();tiles.forEach(t=>t.release())}const data=await api<Day>('/api/day?date='+encodeURIComponent(dateInput.value));if(generation!==loadGeneration)return;records=data.recordings;from=data.from;to=data.to;setScan(data.scan);if(reset){cursor=records.length?Math.max(from,Math.min(...records.map(r=>r.start))):from;viewStart=from;viewEnd=to}$('recording-count').textContent=`${records.length} Aufnahmen · ${cameras.length} Kameras`;update();void thumbnails()}
async function refreshCameras(){const previous=new Set(cameras.map(c=>c.id));cameras=await api<Camera[]>('/api/cameras');for(const c of cameras)if(!previous.has(c.id))selected.add(c.id);renderCameras();renderCameraList()}
function draw(){if(!from)return;const cams=visible(),width=canvas.parentElement!.clientWidth,height=48+Math.max(1,cams.length)*34,dpr=devicePixelRatio||1;
 if(canvas.width!==Math.round(width*dpr)||canvas.height!==Math.round(height*dpr)){canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);canvas.style.height=height+'px'}
 const ctx=canvas.getContext('2d')!;ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,width,height);const left=85,right=width-15,area=right-left,span=viewEnd-viewStart,x=(t:number)=>left+(t-viewStart)/span*area;
 const intervals=[60,300,600,1800,3600,7200,14400,21600];const interval=intervals.find(s=>span/s<area/65)||21600;
 ctx.font='9px system-ui';ctx.textAlign='center';for(let t=Math.ceil(viewStart/interval)*interval;t<=viewEnd;t+=interval){const p=x(t);ctx.strokeStyle='#25313a';ctx.beginPath();ctx.moveTo(p,30);ctx.lineTo(p,height-10);ctx.stroke();ctx.fillStyle='#758997';ctx.fillText(clock(t).slice(0,5),p,19)}
 cams.forEach((camera,i)=>{const y=39+i*34;ctx.fillStyle='#7d909c';ctx.textAlign='left';ctx.fillText(camera.name.length>11?camera.name.slice(0,10)+'…':camera.name,12,y+14);ctx.fillStyle='#1b2730';ctx.fillRect(left,y,area,23);
  ctx.save();ctx.beginPath();ctx.rect(left,y,area,23);ctx.clip();for(const r of records.filter(r=>r.cameraId===camera.id&&r.start<viewEnd&&r.start+r.duration>viewStart)){ctx.fillStyle='#709f80';ctx.fillRect(x(r.start),y+3,Math.max(2,r.duration/span*area),17)}ctx.restore();
 });
 if(cursor>=viewStart&&cursor<=viewEnd){const p=x(cursor);ctx.strokeStyle='#c8f9d5';ctx.lineWidth=1.5;ctx.beginPath();ctx.moveTo(p,29);ctx.lineTo(p,height-8);ctx.stroke();ctx.fillStyle='#c8f9d5';ctx.beginPath();ctx.moveTo(p-4,27);ctx.lineTo(p+4,27);ctx.lineTo(p,33);ctx.fill()}
 const hours=span/3600;$('zoom-label').textContent=hours>=1?`${Math.round(hours*10)/10} Std.`:`${Math.round(span/60)} Min.`;
}
function zoom(factor:number,anchor=cursor){const span=clamp((viewEnd-viewStart)*factor,300,to-from);viewStart=clamp(anchor-span/2,from,to-span);viewEnd=viewStart+span;draw()}
canvas.addEventListener('pointerdown',e=>{const rect=canvas.getBoundingClientRect(),fraction=(e.clientX-rect.left-85)/(rect.width-100);if(fraction>=0)seek(viewStart+clamp(fraction,0,1)*(viewEnd-viewStart));canvas.setPointerCapture(e.pointerId)});
canvas.addEventListener('pointermove',e=>{if(e.buttons===1){const rect=canvas.getBoundingClientRect();seek(viewStart+clamp((e.clientX-rect.left-85)/(rect.width-100),0,1)*(viewEnd-viewStart))}});
canvas.addEventListener('wheel',e=>{e.preventDefault();zoom(e.deltaY>0?1.5:1/1.5)},{passive:false});
canvas.addEventListener('keydown',e=>{if(e.key==='ArrowRight'){e.preventDefault();seek(cursor+10)}if(e.key==='ArrowLeft'){e.preventDefault();seek(cursor-10)}if(e.key==='+')zoom(.5);if(e.key==='-')zoom(2)});
new ResizeObserver(draw).observe(canvas.parentElement!);
async function thumbnails(){const generation=++thumbGeneration;for(const url of thumbURLs)URL.revokeObjectURL(url);thumbURLs.length=0;const root=$('thumbnails');root.replaceChildren();const ids=visible().map(c=>c.id),items=records.filter(r=>ids.includes(r.cameraId)).sort((a,b)=>a.start-b.start);const sample=items.filter((_,i)=>i%Math.max(1,Math.ceil(items.length/8))===0).slice(0,8);
 for(const record of sample){if(generation!==thumbGeneration)return;const button=element('button','thumb'),img=element('img'),label=element('span','',clock(record.start));img.alt='Vorschau '+clock(record.start);button.title=clock(record.start);button.append(img,label);button.onclick=()=>seek(record.start);root.append(button);
  try{const response=await fetch('/api/recordings/'+record.id+'/thumbnail');if(!response.ok)continue;const blob=await response.blob();if(generation!==thumbGeneration)return;const url=URL.createObjectURL(blob);thumbURLs.push(url);img.src=url}catch{}
 }
}
const dialog=$<HTMLDialogElement>('camera-dialog'),cameraForm=$<HTMLFormElement>('camera-form');
function cameraInput():Camera{const data=new FormData(cameraForm);return{id:String(data.get('id')||''),name:String(data.get('name')),folder:String(data.get('folder')),pattern:String(data.get('pattern')),timezone:String(data.get('timezone')),offset:Number(data.get('offset'))}}
function resetCamera(){cameraForm.reset();(cameraForm.elements.namedItem('id') as HTMLInputElement).value='';$('preview-result').hidden=true;$('camera-error').textContent=''}
function renderCameraList(){const list=$('camera-list');list.replaceChildren();for(const c of cameras){const entry=element('div','camera-entry'),edit=element('button','',c.name),remove=element('button','delete','×');remove.title='Kamera entfernen';edit.onclick=()=>{for(const[key,value]of Object.entries(c)){const input=cameraForm.elements.namedItem(key);if(input instanceof HTMLInputElement)input.value=String(value)}};remove.onclick=async()=>{if(!confirm(`Kamera „${c.name}“ aus dem Viewer entfernen? Die Originalaufnahmen bleiben erhalten.`))return;try{await api('/api/cameras/'+c.id,{method:'DELETE'});resetCamera();await refreshCameras();await loadDay()}catch(e){$('camera-error').textContent=(e as Error).message}};entry.append(edit,remove);list.append(entry)}}
cameraForm.addEventListener('submit',async e=>{e.preventDefault();const submit=cameraForm.querySelector<HTMLButtonElement>('button[type=submit]')!;submit.disabled=true;try{await api('/api/cameras',{method:'POST',body:JSON.stringify(cameraInput())});resetCamera();await refreshCameras();await loadDay();notify('Kamera gespeichert. Aufnahmen werden eingelesen.')}catch(e){$('camera-error').textContent=(e as Error).message}finally{submit.disabled=false}});
on('preview',async()=>{try{const result=await api<{examples:{file:string;time?:string;error?:string}[];limited:boolean}>('/api/cameras/preview',{method:'POST',body:JSON.stringify(cameraInput())});$('preview-result').hidden=false;$('preview-result').textContent=result.examples.length?result.examples.map(v=>`${v.file}\n→ ${v.error||v.time}`).join('\n\n'):'Keine passenden Videodateien gefunden.';if(result.limited)$('preview-result').textContent+='\nSuche auf 10.000 Einträge begrenzt.'}catch(e){$('camera-error').textContent=(e as Error).message}});
on('new-camera',resetCamera);on('close-dialog',()=>dialog.close());on('settings-button',()=>dialog.showModal());on('add-first',()=>dialog.showModal());
on('play',()=>playing?pause():play());on('skip-back',()=>seek(cursor-10));on('skip-forward',()=>seek(cursor+10));on('zoom-in',()=>zoom(.5));on('zoom-out',()=>zoom(2));on('reset-zoom',()=>{viewStart=from;viewEnd=to;draw()});
function changeDate(delta:number){const d=new Date(dateInput.value+'T12:00:00Z');d.setUTCDate(d.getUTCDate()+delta);dateInput.value=d.toISOString().slice(0,10);return loadDay()}
on('prev-day',()=>changeDate(-1));on('next-day',()=>changeDate(1));on('today',()=>{dateInput.value=berlinDate(new Date());return loadDay()});dateInput.addEventListener('change',()=>void loadDay().catch(e=>notify(e.message)));
$<HTMLSelectElement>('layout').addEventListener('change',renderCameras);$<HTMLSelectElement>('quality').addEventListener('change',()=>{tiles.forEach(t=>t.release());update()});$<HTMLSelectElement>('speed').addEventListener('change',()=>update());
on('rescan',async()=>{setScan(await api<Scan>('/api/scan',{method:'POST',body:JSON.stringify({date:dateInput.value})}))});
on('logout',async()=>{pause();tiles.forEach(t=>t.release());await api('/api/logout',{method:'POST'});location.reload()});
window.addEventListener('pagehide',()=>{pause();tiles.forEach(t=>t.release())});document.addEventListener('visibilitychange',()=>{if(document.hidden){pause();tiles.forEach(t=>t.release())}});
const loginForm=$<HTMLFormElement>('login-form');loginForm.addEventListener('submit',async e=>{e.preventDefault();const button=loginForm.querySelector('button')!;button.disabled=true;try{const data=new FormData(loginForm);const res=await fetch('/auth/local',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(data))});const body=await res.json();if(!res.ok)throw new Error(body.error);location.reload()}catch(e){$('login-error').textContent=(e as Error).message}finally{button.disabled=false}});
async function boot(){const response=await fetch('/api/session');if(!response.ok){$('login').hidden=false;const methods=await api<{local:boolean;oidc:boolean}>('/auth/methods');loginForm.hidden=!methods.local;$('oidc-login').hidden=!methods.oidc;$('login-divider').hidden=!(methods.local&&methods.oidc);return}
 session=await response.json();$('app').hidden=false;$('user-name').textContent=session.name;$('avatar').textContent=session.name.slice(0,2).toUpperCase();$('settings-button').hidden=session.role!=='admin';$('rescan').hidden=session.role!=='admin';dateInput.value=berlinDate(new Date());await refreshCameras();await loadDay();requestAnimationFrame(frame);
 setInterval(async()=>{try{const scan=await api<Scan>('/api/scan');const wasRunning=scanRunning;setScan(scan);if(wasRunning&&!scan.running)await loadDay(false)}catch(e){notify((e as Error).message)}},4000);
}
void boot().catch(e=>notify(e.message));
