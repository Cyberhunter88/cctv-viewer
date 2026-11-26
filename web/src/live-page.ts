import {registerPage,navigate} from './navigation';
import {LivePlayer} from './live-player';
import {liveSelection,type LivePreferences} from './live-state';
import {gridSize,moveCamera} from './view-state';
import {attachIdleFade,collapseTile,toggleEnlarged} from './expanded-tile';
import {wrapChipWithMoveButtons,createReorderToggle} from './chip-order';
import {setControlIcon} from './playback-tile';
import {popoverMenu} from './popover-menu';
// Live expand buttons show whether their tile is currently enlarged.
function syncLiveExpand(){
 for(const b of document.querySelectorAll('#live-grid .live-expand')){
  const tile=b.closest('.live-tile');
  setControlIcon(b,tile&&(document.fullscreenElement===tile||tile.classList.contains('expanded'))?'collapse':'expand')
 }
}
document.addEventListener('fullscreenchange',syncLiveExpand);
document.addEventListener('tileexpandchange',syncLiveExpand);
interface Camera{id:string;name:string;viewMode?:string}
interface Capability{enabled:boolean;high:boolean;zoom:boolean;zoomHigh:boolean}
interface LiveInfo{ready:boolean;message:string;webrtc?:boolean;cameras:Record<string,Capability>}
type Context={api:<T>(url:string,options?:RequestInit)=>Promise<T>;cameras:()=>Camera[];preferences:()=>LivePreferences;save:()=>Promise<void>;admin:()=>boolean;notify:(message:string)=>void};
export function initLivePage(c:Context){
 const root=document.createElement('section');
 root.id='live-page';
 root.className='content-page settings-page';
 root.setAttribute('role','main');
 root.hidden=true;
 // Same structure as the recordings page: heading with status, one toolbar, camera chips with a "⋯" menu, grid.
 root.innerHTML=`<div class="page-heading"><div><h1>Liveansicht</h1></div><div class="index-state"><span class="live-dot" aria-hidden="true"></span><span id="live-service-status" role="status"></span></div></div>
 <section class="archive-controls" aria-label="Livekameras auswählen"><div class="view-toolbar"><span class="live-toolbar-label">Aktuelle Kamerabilder</span><div class="view-options"><label class="compact-label"><span class="sr-only">Kameraraster</span><select id="live-layout" aria-label="Kameraraster"><option value="auto">Automatisch</option><option value="1x1">1 × 1</option><option value="2x2">2 × 2</option><option value="3x3">3 × 3</option><option value="4x4">4 × 4</option></select></label></div></div><section class="camera-selection" aria-label="Livekameraauswahl"><div class="selection-heading"><strong>Kameras</strong><details id="live-selection-menu" class="popover-menu"><summary class="icon-button" aria-label="Kameraaktionen" title="Kameraaktionen"><svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.8"/><circle cx="12" cy="12" r="1.8"/><circle cx="19" cy="12" r="1.8"/></svg></summary><div class="popover-panel"><button id="live-select-all" class="text-button" type="button">Alle auswählen</button><button id="live-select-none" class="text-button" type="button">Auswahl aufheben</button></div></details></div><p id="live-reorder-hint" class="setting-help" hidden>Mit ‹ und › verschieben. Alternativ eine Kamera ziehen oder Alt + Pfeil links/rechts drücken.</p><div id="live-camera-filters" class="camera-filters"></div><div id="live-page-controls" class="camera-page-controls"><span id="live-range" role="status"></span><button id="live-prev" class="icon-button" type="button" aria-label="Vorherige Kameraseite"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m14 6-6 6 6 6"/></svg></button><button id="live-next" class="icon-button" type="button" aria-label="Nächste Kameraseite"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m10 6 6 6-6 6"/></svg></button></div></section></section>
 <p id="live-empty" class="empty-filter-hint"></p><div id="live-grid" class="video-grid live-grid"></div><p class="live-setup-row"><button id="live-setup" class="text-button" type="button">Livequellen einrichten</button></p>`;
 document.querySelector('.workspace')!.append(root);
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>root.querySelector<T>('#'+id)!;
 let info:LiveInfo={ready:false,message:'',cameras:{}},reorderMode=false,active=false,page=0,generation=0,audio:HTMLVideoElement|undefined,poll:ReturnType<typeof setTimeout>|undefined;
 const players=new Map<string,LivePlayer>(),openZoom=new Set<string>(),nodes=new Map<string,HTMLElement>(),groups=new Map<string,HTMLElement>();
 function stop(){collapseTile(false);for(const player of players.values())player.dispose();players.clear();nodes.clear();groups.clear();$('live-grid').replaceChildren();audio=undefined}
 function persist(){void c.save().catch(()=>{})}
 function setSelected(ids:string[]){c.preferences().liveCameras=ids;page=0;persist();render()}
 function render(){
  if(!active||document.hidden)return;
  let staggered=0;
  const prefs=c.preferences(),cameras=c.cameras(),ids=cameras.map(camera=>camera.id),{order,selected}=liveSelection(prefs,ids);
  if(prefs.liveCameras===undefined){selected.clear();for(const id of ids)if(info.cameras[id]?.enabled)selected.add(id)}
  const focused=document.activeElement instanceof HTMLElement?document.activeElement.id:'';
  $('live-service-status').textContent=info.message;
  $('live-setup').hidden=!c.admin();
  const filters=$('live-camera-filters');
  filters.replaceChildren();
  for(const id of order){
   const camera=cameras.find(camera=>camera.id===id)!;
   const enabled=!!info.cameras[id]?.enabled;
   const b=document.createElement('button');
   b.id='live-filter-'+id;
   b.type='button';
   b.className='filter'+(selected.has(id)?' selected':'');
   b.textContent=camera.name+(enabled?'':' · nicht eingerichtet');
   b.setAttribute('aria-pressed',String(selected.has(id)));
   b.setAttribute('aria-keyshortcuts','Alt+ArrowLeft Alt+ArrowRight');
   b.title='Reihenfolge mit Alt + Pfeil links/rechts ändern';
   b.draggable=true;
   b.onclick=()=>{if(selected.has(id))selected.delete(id);else selected.add(id);setSelected([...selected])};
   b.ondragstart=e=>e.dataTransfer?.setData('text/plain',id);
   b.ondragover=e=>e.preventDefault();
   b.ondrop=e=>{
    e.preventDefault();
    const source=e.dataTransfer?.getData('text/plain');
    if(source&&order.includes(source)){
     prefs.liveOrder=moveCamera(order,source,id);
     persist();
     render()
    }
   };
   b.onkeydown=e=>{
    if(!e.altKey||!['ArrowLeft','ArrowRight'].includes(e.key))return;
    const target=order.indexOf(id)+(e.key==='ArrowLeft'?-1:1);
    if(target<0||target>=order.length)return;
    e.preventDefault();
    prefs.liveOrder=moveCamera(order,id,order[target]);
    persist();
    render()
   };
   const index=order.indexOf(id);
   filters.append(reorderMode?wrapChipWithMoveButtons(b,camera.name,index,order.length,delta=>{
    const focusId=`${b.id}-move-${delta<0?'left':'right'}`;
    prefs.liveOrder=moveCamera(order,id,order[index+delta]);
    persist();
    render();
    const next=document.getElementById(focusId) as HTMLButtonElement|null;
    (next&&!next.disabled?next:document.getElementById(b.id))?.focus({preventScroll:true})
   }):b);
  }
  const layout=$<HTMLSelectElement>('live-layout');
  layout.value=prefs.liveLayout||'auto';
  const chosen=order.filter(id=>selected.has(id)),grid=$('live-grid'),[columns,rows]=gridSize(layout.value,2,2,chosen.length,grid.clientWidth||innerWidth);
  const capacity=columns*rows;
  page=Math.max(0,Math.min(page,Math.ceil(chosen.length/capacity)-1));
  grid.style.gridTemplateColumns=`repeat(${columns},minmax(0,1fr))`;
  const visible=chosen.slice(page*capacity,(page+1)*capacity),zoomAvailable=(id:string)=>cameras.some(camera=>camera.id===id&&camera.viewMode==='dual')&&!!info.cameras[id]?.zoom;
  const keys=new Set(visible.flatMap(id=>openZoom.has(id)&&zoomAvailable(id)?[id+':wide',id+':zoom']:[id+':wide']));
  for(const [key,node] of nodes)if(!keys.has(key)){players.get(key)?.dispose();players.delete(key);node.remove();nodes.delete(key)}
  for(const [id,group] of groups)if(!visible.includes(id)){group.remove();groups.delete(id)}
  $('live-empty').textContent=!cameras.length?'Noch keine Kameras eingerichtet.':!selected.size?'Keine Kameras ausgewählt. Wähle oben mindestens eine Kamera aus.':'';
  $('live-empty').hidden=!$('live-empty').textContent;
  for(const [index,id] of visible.entries()){
   const camera=cameras.find(camera=>camera.id===id)!,capability=info.cameras[id],group=groups.get(id)||document.createElement('div');
   groups.set(id,group);
   group.className='camera-group'+(openZoom.has(id)&&zoomAvailable(id)?' dual zoom-open':'');
   if(grid.children[index]!==group)grid.insertBefore(group,grid.children[index]||null);
   const existingZoom=document.getElementById('live-zoom-'+id);
   if(existingZoom){
    existingZoom.textContent=openZoom.has(id)?'Zoom schließen':'Zoom anzeigen';
    existingZoom.setAttribute('aria-expanded',String(openZoom.has(id)))
   }
   for(const view of openZoom.has(id)&&zoomAvailable(id)?['wide','zoom']:['wide']){
    const key=id+':'+view;
    if(nodes.has(key)){const video=nodes.get(key)!.querySelector('video');if(video&&!document.hidden)void video.play().catch(()=>{});continue}
    // Same anatomy as a recording tile: picture, name overlay, footer with status and icon actions.
    const tile=document.createElement('article');
    tile.className='camera-tile live-tile';
    tile.setAttribute('aria-label',camera.name+(view==='zoom'?' · Zoom':''));
    const header=document.createElement('div');
    header.className='tile-header';
    const heading=document.createElement('strong');
    heading.className='tile-title';
    heading.textContent=camera.name+(view==='zoom'?' · Zoom':'');
    const badge=document.createElement('small');
    badge.className='live-badge';
    badge.textContent='LIVE';
    header.append(heading,badge);
    const video=document.createElement('video');
    video.setAttribute('aria-label','Livebild '+heading.textContent);
    video.onclick=()=>void video.play().catch(()=>{});
    const message=document.createElement('div');
    message.className='tile-message';
    message.hidden=true;
    const startButton=document.createElement('button');
    startButton.type='button';
    startButton.className='tile-retry live-start';
    startButton.textContent='Wiedergabe starten';
    startButton.setAttribute('aria-label','Wiedergabe starten: '+heading.textContent);
    startButton.hidden=true;
    startButton.onclick=()=>void video.play().then(()=>{
     startButton.hidden=true;
     message.hidden=true
    }).catch(()=>{});
    message.append(startButton);
    // Tile states are not live regions: a grid of tiles would flood screen readers.
    const status=document.createElement('span');
    status.className='live-tile-status';
    const setStatus=(message_:string)=>{
     status.textContent=message_;
     startButton.hidden=!message_.startsWith('Zum Starten');
     message.hidden=startButton.hidden;
     tile.classList.toggle('live-playing',message_.startsWith('Live ·'))
    };
    const qualityHint=document.createElement('span');
    qualityHint.className='setting-help live-quality-hint';
    qualityHint.hidden=true;
    const footer=document.createElement('div');
    footer.className='tile-footer';
    const controls=document.createElement('div');
    controls.className='tile-actions live-tile-controls';
    const button=(text:string,fn:()=>void)=>{
     const b=document.createElement('button');
     b.type='button';
     b.className='text-button';
     b.textContent=text;
     b.onclick=fn;
     b.ondblclick=e=>e.stopPropagation();
     controls.append(b);
     return b
    };
    const quality=document.createElement('select');
    quality.className='live-quality';
    quality.setAttribute('aria-label','Livequalität '+heading.textContent);
    quality.innerHTML='<option value="fast">Schnell</option>';
    if(view==='wide'?capability?.high:capability?.zoomHigh)quality.add(new Option('Hohe Qualität','high'));
    controls.append(quality);
    const soundState=(b:Element,on:boolean)=>{
     setControlIcon(b,on?'sound':'muted');
     const label=(on?'Ton ausschalten':'Ton einschalten')+': '+((b as HTMLElement).dataset.cameraName||'');
     b.setAttribute('aria-label',label);
     b.setAttribute('title',on?'Ton ausschalten':'Ton einschalten');
     b.setAttribute('aria-pressed',String(on))
    };
    const sound=button('Ton einschalten',()=>{
     if(!video.muted){
      video.muted=true;
      soundState(sound,false);
      return
     }
     for(const v of grid.querySelectorAll('video'))v.muted=true;
     for(const b of grid.querySelectorAll('[data-live-audio]'))soundState(b,false);
     audio=video;
     audio.muted=false;
     soundState(sound,true);
     void video.play().catch(()=>{
      video.muted=true;
      soundState(sound,false)
     })
    });
    sound.dataset.liveAudio='';
    sound.dataset.cameraName=heading.textContent||'';
    sound.className='tile-audio live-sound';
    soundState(sound,false);
    if(view==='wide'&&zoomAvailable(id)){
     const zoom=button(openZoom.has(id)?'Zoom schließen':'Zoom anzeigen',()=>{
      if(openZoom.has(id))openZoom.delete(id);else openZoom.add(id);
      render();
      document.getElementById('live-zoom-'+id)?.focus()
     });
     zoom.id='live-zoom-'+id;
     zoom.className='zoom-toggle';
     zoom.setAttribute('aria-expanded',String(openZoom.has(id)))
    }
    const reconnect=button('Erneut verbinden',()=>start());
    reconnect.setAttribute('aria-label','Erneut verbinden: '+heading.textContent);
    reconnect.title='Erneut verbinden';
    reconnect.className='tile-reconnect live-reconnect';
    setControlIcon(reconnect,'reconnect');
    const expand=button('Vergrößern',()=>void toggleEnlarged(tile,expand).catch(()=>{}));
    expand.setAttribute('aria-label','Vergrößern: '+heading.textContent);
    expand.title='Vergrößern';
    expand.className='tile-expand live-expand';
    setControlIcon(expand,'expand');
    tile.ondblclick=()=>void toggleEnlarged(tile,expand).catch(()=>{});
    footer.append(status,controls,qualityHint);
    attachIdleFade(tile,()=>!video.paused);
    nodes.set(key,tile);
    // Tiles of a new grid connect 60 ms apart so camera and restreamer do not take all starts at once.
    const start=(delay=0)=>{
     players.get(key)?.dispose();
     players.delete(key);
     if(!info.ready||!capability?.enabled||document.hidden){
      setStatus(!capability?.enabled?'Livequelle nicht eingerichtet':info.message||'Livebild angehalten');
      return
     }
     const launch=()=>{
      if(nodes.get(key)!==tile||document.hidden)return;
      players.get(key)?.dispose();
      players.set(key,new LivePlayer(video,id,view,quality.value,setStatus,()=>{
       quality.value='fast';
       qualityHint.textContent='Hohe Qualität nicht verfügbar · schneller Stream aktiv';
       qualityHint.hidden=false;
       start()
      },info.webrtc===true))
     };
     if(delay>0){setStatus('Verbindung wird aufgebaut …');setTimeout(launch,delay)}else launch()
    };
    quality.onchange=()=>{qualityHint.hidden=true;start()};
    tile.append(video,header,message,footer);
    group.append(tile);
    start(Math.min(staggered++*60,900));
   }
  }
  $<HTMLButtonElement>('live-prev').disabled=page===0;
  $<HTMLButtonElement>('live-next').disabled=(page+1)*capacity>=chosen.length;
  $('live-page-controls').classList.toggle('single-page',chosen.length<=capacity);
  if(focused)document.getElementById(focused)?.focus({preventScroll:true});
  $('live-range').textContent=chosen.length?`Kameras ${page*capacity+1}–${Math.min(chosen.length,(page+1)*capacity)} von ${chosen.length}`:'Keine Kamera ausgewählt';
 }
 async function refresh(){
  const current=++generation;
  try{
   const next=await c.api<LiveInfo>('/api/live');
   if(current!==generation)return;
   const previous=info;
   info=next;
   $('live-service-status').textContent=next.message;
  if(previous.ready!==next.ready){stop();render()}
  else{
    let changed=false;
    for(const id of new Set([...Object.keys(previous.cameras),...Object.keys(next.cameras)]))if(JSON.stringify(previous.cameras[id])!==JSON.stringify(next.cameras[id])){
     changed=true;
     for(const [key,node] of nodes)if(key.startsWith(id+':')){
      players.get(key)?.dispose();
      players.delete(key);
      node.remove();
      nodes.delete(key)
     }
    }
    if(changed)render()
   }
 }catch(error){
   if(current===generation)$('live-service-status').textContent=(error as Error).message
  }finally{
   if(current===generation){
    clearTimeout(poll);
    if(active&&!document.hidden)poll=setTimeout(()=>void refresh(),10000)
   }
  }
 }
 $('live-select-none').after(createReorderToggle('live-reorder-cameras',()=>reorderMode,()=>{
  reorderMode=!reorderMode;
  $('live-reorder-hint').hidden=!reorderMode;
  render()
 }));
 popoverMenu($<HTMLDetailsElement>('live-selection-menu'));
 $('live-select-all').onclick=()=>setSelected(c.cameras().map(camera=>camera.id));
 $('live-select-none').onclick=()=>setSelected([]);
 $('live-layout').onchange=()=>{c.preferences().liveLayout=$<HTMLSelectElement>('live-layout').value;page=0;persist();render()};
 $('live-prev').onclick=()=>{page--;render()};
 $('live-next').onclick=()=>{page++;render()};
 $('live-setup').onclick=()=>navigate('admin/cameras/live');
 window.addEventListener('pagehide',()=>{stop();clearTimeout(poll)});
 window.addEventListener('pageshow',event=>{if(event.persisted&&active&&!document.hidden){render();void refresh()}});
 document.addEventListener('visibilitychange',()=>{if(document.hidden){stop();clearTimeout(poll)}else if(active){render();void refresh()}});
 let resize:ReturnType<typeof setTimeout>;
 window.addEventListener('resize',()=>{clearTimeout(resize);resize=setTimeout(()=>{if(active&&!document.fullscreenElement)render()},200)});
 registerPage('live',{element:root,title:'Liveansicht',enter:async()=>{active=true;render();await refresh()},leave:()=>{active=false;generation++;clearTimeout(poll);stop();return true}});
 return {refresh:()=>{if(active){render();void refresh()}},stop};
}
