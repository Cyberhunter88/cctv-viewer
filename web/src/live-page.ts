import {registerPage,navigate} from './navigation';
import {LivePlayer} from './live-player';
import {liveSelection,type LivePreferences} from './live-state';
import {gridSize,moveCamera} from './view-state';
interface Camera{id:string;name:string}
interface Capability{enabled:boolean;high:boolean;zoom:boolean;zoomHigh:boolean}
interface LiveInfo{ready:boolean;message:string;cameras:Record<string,Capability>}
type Context={api:<T>(url:string,options?:RequestInit)=>Promise<T>;cameras:()=>Camera[];preferences:()=>LivePreferences;save:()=>Promise<void>;admin:()=>boolean;notify:(message:string)=>void};
export function initLivePage(c:Context){
 const root=document.createElement('section');root.id='live-page';root.className='content-page';root.setAttribute('role','main');root.hidden=true;
 root.innerHTML=`<div class="page-heading"><div><span class="eyebrow">JETZT VOR ORT</span><h1>Liveansicht</h1></div><span id="live-service-status" class="muted" role="status"></span></div>
 <section class="archive-controls" aria-label="Livekameras auswählen"><div class="view-toolbar"><p class="muted">Aktuelle Kamerabilder</p><label class="compact-label">Ansicht<select id="live-layout"><option value="auto">Automatisch</option><option value="1x1">1 × 1</option><option value="2x2">2 × 2</option><option value="3x3">3 × 3</option><option value="4x4">4 × 4</option></select></label></div><section class="camera-selection" aria-label="Livekameraauswahl"><div class="selection-heading"><strong>Kameras</strong><div><button id="live-select-all" class="text-button" type="button">Alle</button><button id="live-select-none" class="text-button" type="button">Keine</button></div></div><div id="live-camera-filters" class="camera-filters"></div></section></section>
 <p id="live-empty" class="empty-filter-hint"></p><div id="live-grid" class="video-grid live-grid"></div><div class="live-pagination"><button id="live-prev" class="button secondary" type="button">← Zurück</button><span id="live-range" role="status"></span><button id="live-next" class="button secondary" type="button">Weiter →</button></div><p><button id="live-setup" class="button secondary" type="button">Livequellen einrichten</button></p>`;
 document.querySelector('.workspace')!.append(root);
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>root.querySelector<T>('#'+id)!;
 let info:LiveInfo={ready:false,message:'',cameras:{}},active=false,page=0,generation=0,audio:HTMLVideoElement|undefined,poll:ReturnType<typeof setTimeout>|undefined;
 const players=new Map<string,LivePlayer>(),openZoom=new Set<string>(),nodes=new Map<string,HTMLElement>(),groups=new Map<string,HTMLElement>();
 function stop(){for(const player of players.values())player.dispose();players.clear();nodes.clear();groups.clear();$('live-grid').replaceChildren();audio=undefined}
 function persist(){void c.save().catch(()=>{})}
 function setSelected(ids:string[]){c.preferences().liveCameras=ids;page=0;persist();render()}
 function render(){
  if(!active||document.hidden)return;
  const prefs=c.preferences(),cameras=c.cameras(),ids=cameras.map(camera=>camera.id),{order,selected}=liveSelection(prefs,ids);
  if(prefs.liveCameras===undefined){selected.clear();for(const id of ids)if(info.cameras[id]?.enabled)selected.add(id)}
  const focused=document.activeElement instanceof HTMLElement?document.activeElement.id:'';
  $('live-service-status').textContent=info.message;$('live-setup').hidden=!c.admin();
  const filters=$('live-camera-filters');filters.replaceChildren();
  for(const id of order){const camera=cameras.find(camera=>camera.id===id)!;const enabled=!!info.cameras[id]?.enabled;
   const b=document.createElement('button');b.id='live-filter-'+id;b.type='button';b.className='filter'+(selected.has(id)?' selected':'');b.textContent=camera.name+(enabled?'':' · nicht eingerichtet');b.setAttribute('aria-pressed',String(selected.has(id)));b.draggable=true;
   b.onclick=()=>{if(selected.has(id))selected.delete(id);else selected.add(id);setSelected([...selected])};
   b.ondragstart=e=>e.dataTransfer?.setData('text/plain',id);b.ondragover=e=>e.preventDefault();b.ondrop=e=>{e.preventDefault();const source=e.dataTransfer?.getData('text/plain');if(source&&order.includes(source)){prefs.liveOrder=moveCamera(order,source,id);persist();render()}};filters.append(b);
  }
  const layout=$<HTMLSelectElement>('live-layout');layout.value=prefs.liveLayout||'auto';
  const chosen=order.filter(id=>selected.has(id)),grid=$('live-grid'),[columns,rows]=gridSize(layout.value,2,2,chosen.length,grid.clientWidth||innerWidth);
  const capacity=columns*rows;page=Math.max(0,Math.min(page,Math.ceil(chosen.length/capacity)-1));grid.style.gridTemplateColumns=`repeat(${columns},minmax(0,1fr))`;
  const visible=chosen.slice(page*capacity,(page+1)*capacity),keys=new Set(visible.flatMap(id=>openZoom.has(id)&&info.cameras[id]?.zoom?[id+':wide',id+':zoom']:[id+':wide']));
  for(const [key,node] of nodes)if(!keys.has(key)){players.get(key)?.dispose();players.delete(key);node.remove();nodes.delete(key)}
  for(const [id,group] of groups)if(!visible.includes(id)){group.remove();groups.delete(id)}
  $('live-empty').textContent=!cameras.length?'Noch keine Kameras eingerichtet.':!selected.size?'Keine Kameras ausgewählt. Wähle oben mindestens eine Kamera aus.':'';
  $('live-empty').hidden=!$('live-empty').textContent;
  for(const [index,id] of visible.entries()){
   const camera=cameras.find(camera=>camera.id===id)!,capability=info.cameras[id],group=groups.get(id)||document.createElement('div');groups.set(id,group);group.className='camera-group'+(openZoom.has(id)?' dual zoom-open':'');if(grid.children[index]!==group)grid.insertBefore(group,grid.children[index]||null);
   const existingZoom=document.getElementById('live-zoom-'+id);if(existingZoom){existingZoom.textContent=openZoom.has(id)?'Zoom schließen':'Zoom anzeigen';existingZoom.setAttribute('aria-expanded',String(openZoom.has(id)))}
   for(const view of openZoom.has(id)&&capability?.zoom?['wide','zoom']:['wide']){
    const key=id+':'+view;if(nodes.has(key)){const video=nodes.get(key)!.querySelector('video');if(video&&!document.hidden)void video.play().catch(()=>{});continue}
    const tile=document.createElement('article');tile.className='camera-tile live-tile';tile.setAttribute('aria-label',camera.name+(view==='zoom'?' · Zoom':''));
    const heading=document.createElement('strong');heading.textContent=camera.name+(view==='zoom'?' · Zoom':'');
    const video=document.createElement('video');video.setAttribute('aria-label','Livebild '+heading.textContent);video.onclick=()=>void video.play().catch(()=>{});
    const status=document.createElement('p');status.className='live-tile-status';status.setAttribute('role','status');
    const controls=document.createElement('div');controls.className='live-tile-controls';
    const button=(text:string,fn:()=>void)=>{const b=document.createElement('button');b.type='button';b.className='text-button';b.textContent=text;b.onclick=fn;controls.append(b);return b};
    const quality=document.createElement('select');quality.setAttribute('aria-label','Livequalität '+heading.textContent);quality.innerHTML='<option value="fast">Schnell</option>';
    if(view==='wide'?capability?.high:capability?.zoomHigh)quality.add(new Option('Hohe Qualität','high'));
    controls.append(quality);
    const sound=button('Ton einschalten',()=>{if(!video.muted){video.muted=true;sound.textContent='Ton einschalten';return}for(const v of grid.querySelectorAll('video'))v.muted=true;for(const b of grid.querySelectorAll('[data-live-audio]'))b.textContent='Ton einschalten';audio=video;audio.muted=false;sound.textContent='Ton ausschalten';void video.play().catch(()=>{video.muted=true;sound.textContent='Ton einschalten'})});sound.dataset.liveAudio='';
    button('Vergrößern',()=>{if(document.fullscreenElement)void document.exitFullscreen();else if(tile.requestFullscreen)void tile.requestFullscreen().catch(()=>tile.classList.toggle('expanded'));else tile.classList.toggle('expanded')});
    if(view==='wide'&&capability?.zoom){const zoom=button(openZoom.has(id)?'Zoom schließen':'Zoom anzeigen',()=>{if(openZoom.has(id))openZoom.delete(id);else openZoom.add(id);render();document.getElementById('live-zoom-'+id)?.focus()});zoom.id='live-zoom-'+id;zoom.setAttribute('aria-expanded',String(openZoom.has(id)))}
    nodes.set(key,tile);
    const start=()=>{players.get(key)?.dispose();if(!info.ready||!capability?.enabled||document.hidden){status.textContent=!capability?.enabled?'Livequelle nicht eingerichtet':info.message||'Livebild angehalten';return}players.set(key,new LivePlayer(video,id,view,quality.value,message=>{status.textContent=message},()=>{quality.value='fast';c.notify('Hohe Qualität wird nicht unterstützt. Schneller Stream wird verwendet.');start()}))};
    quality.onchange=start;button('Erneut verbinden',start);tile.append(heading,video,status,controls);group.append(tile);start();
   }
  }
  $<HTMLButtonElement>('live-prev').disabled=page===0;$<HTMLButtonElement>('live-next').disabled=(page+1)*capacity>=chosen.length;
  if(focused)document.getElementById(focused)?.focus({preventScroll:true});
  $('live-range').textContent=chosen.length?`Kameras ${page*capacity+1}–${Math.min(chosen.length,(page+1)*capacity)} von ${chosen.length}`:'Keine Kamera ausgewählt';
 }
 async function refresh(){const current=++generation;try{const next=await c.api<LiveInfo>('/api/live');if(current!==generation)return;const changed=JSON.stringify(next)!==JSON.stringify(info);info=next;if(changed){stop();render()}}catch(error){if(current===generation)$('live-service-status').textContent=(error as Error).message}finally{if(current===generation){clearTimeout(poll);if(active&&!document.hidden)poll=setTimeout(()=>void refresh(),10000)}}}
 $('live-select-all').onclick=()=>setSelected(c.cameras().map(camera=>camera.id));$('live-select-none').onclick=()=>setSelected([]);
 $('live-layout').onchange=()=>{c.preferences().liveLayout=$<HTMLSelectElement>('live-layout').value;page=0;persist();render()};
 $('live-prev').onclick=()=>{page--;render()};$('live-next').onclick=()=>{page++;render()};$('live-setup').onclick=()=>navigate('admin/cameras/live');
 window.addEventListener('pagehide',()=>{stop();clearTimeout(poll)});
 window.addEventListener('pageshow',event=>{if(event.persisted&&active&&!document.hidden){render();void refresh()}});
 document.addEventListener('visibilitychange',()=>{if(document.hidden){stop();clearTimeout(poll)}else if(active){render();void refresh()}});
 let resize:ReturnType<typeof setTimeout>;window.addEventListener('resize',()=>{clearTimeout(resize);resize=setTimeout(()=>{if(active&&!document.fullscreenElement)render()},200)});
 registerPage('live',{element:root,title:'Liveansicht',enter:async()=>{active=true;render();await refresh()},leave:()=>{active=false;generation++;clearTimeout(poll);stop();return true}});
 return {refresh:()=>{if(active){render();void refresh()}},stop};
}
