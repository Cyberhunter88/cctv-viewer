import {registerPage,navigate,formGuard,currentPage} from './navigation';
import {liveKeys,importLiveURL} from './live-source';
type Camera={id:string;name:string;viewMode?:string};
type Sources=Record<string,string>;
interface LiveConfig{enabled:boolean;username:string;hasPassword:boolean;sources:Sources;shared:boolean;preload?:Record<string,string>;skipTLSVerify?:Record<string,boolean>}
interface Share{enabled:boolean;username:string;password:string;hasPassword:boolean;address:string;listening:boolean}
type Context={api:<T>(url:string,options?:RequestInit)=>Promise<T>;cameras:()=>Camera[];viewMode:(id:string)=>string;notify:(message:string)=>void;changed:()=>void};
export function initLiveAdmin(c:Context){
 const root=document.createElement('section');root.id='live-admin';root.className='live-editor';root.hidden=true;
 const names={fast:'Weitwinkel · Schnell',high:'Weitwinkel · Hohe Qualität',zoomFast:'Zoom · Schnell',zoomHigh:'Zoom · Hohe Qualität'};
 root.innerHTML=`<p id="live-admin-empty" class="setting-help">Zuerst eine Kamera speichern, um Livequellen einzurichten.</p>
 <form id="live-camera-form" hidden><fieldset><legend>Kamerazugang</legend><label class="checkbox"><input id="live-enabled" type="checkbox"> Liveansicht aktivieren</label><div class="form-row"><label>Benutzername<input id="live-username" maxlength="256" autocomplete="off"></label><label>Neues Kamerapasswort<input id="live-password" type="password" maxlength="1024" autocomplete="new-password"><small id="live-password-state"></small></label></div><details><summary>Passwort verwalten</summary><label class="checkbox"><input id="live-clear-password" type="checkbox"> Gespeichertes Kamerapasswort entfernen</label></details><p class="setting-help">Die Zugangsdaten gelten für alle Quellen dieser Kamera. RTSP und HTTPS-FLV werden unterstützt. Für FLV H.264 verwenden.</p></fieldset>
 <div class="live-source-list">${liveKeys.map(key=>`<fieldset class="live-source-card" data-source="${key}"><legend>${names[key]}</legend><label>Streamadresse${key==='fast'?'':' (optional)'}<input id="live-${key}" type="url" maxlength="2048" autocomplete="off" placeholder="rtsp://kamera.example.org/sub oder https://kamera.example.org/flv?…"></label><p id="live-type-${key}" class="setting-help"></p><div class="form-row"><label>Verbindung bereithalten<select id="live-preload-${key}"><option value="off">Bei Bedarf</option><option value="idle">60 Sekunden Nachlauf</option><option value="always">Dauerhaft bereit</option></select></label><p id="live-status-${key}" class="source-state" role="status">Nicht verbunden</p></div><details><summary>Erweitert</summary><label class="checkbox"><input id="live-tls-${key}" type="checkbox"> Zertifikatsprüfung für diese HTTPS-Quelle überspringen</label><small>Nur für Kameras mit selbstsigniertem Zertifikat aktivieren. Die Identität der Kamera wird dann nicht geprüft.</small></details></fieldset>`).join('')}</div>
 <p class="setting-help">Nachlauf hält eine genutzte Quelle kurz bereit. Dauerhaft bereit verbindet sie auch ohne Zuschauer und benötigt laufend Bandbreite. Es werden keine Liveaufnahmen gespeichert.</p><label class="checkbox"><input id="live-shared" type="checkbox"> Diese Kamera für Home Assistant freigeben</label>
 <div class="dialog-actions settings-actions"><button id="live-test" class="button secondary" type="button">Verbindung testen</button><button id="live-discard" class="button secondary" type="button">Änderungen verwerfen</button><button class="button primary" type="submit">Liveeinstellungen speichern</button></div><pre id="live-test-result" hidden></pre><details class="danger-zone"><summary>Livekonfiguration entfernen</summary><p>Entfernt die Livequellen und die gespeicherten Kamerazugangsdaten.</p><button id="live-remove" class="button secondary" type="button">Livekonfiguration entfernen</button></details></form><p id="live-admin-error" class="error" role="alert"></p><button id="live-load-retry" class="button secondary" type="button" hidden>Erneut laden</button>`;
 const shareRoot=document.createElement('section');shareRoot.className='content-page';shareRoot.id='live-share-page';
 shareRoot.innerHTML=`<div class="dialog-heading"><div><span class="eyebrow">VERWALTUNG</span><h2>Streamfreigabe</h2><p class="muted">Kamerabilder im Heimnetz für Home Assistant bereitstellen.</p></div><button id="live-share-back" class="button secondary" type="button">← Kameras</button></div><form id="live-share-form"><fieldset><legend>Home Assistant</legend><p>In der Integration „Generic Camera“ RTSP über TCP auswählen.</p><label class="checkbox"><input id="live-share-enabled" type="checkbox"> RTSP-Freigabe im Heimnetz aktivieren</label><details><summary>Zugangsdaten erneuern</summary><label class="checkbox"><input id="live-share-reset" type="checkbox"> Neue Zugangsdaten erzeugen und bestehende ersetzen</label></details><p id="live-share-status" class="muted" role="status"></p><p class="setting-help">Der Heimnetz-Port wird separat über die Installation freigegeben.</p><div id="live-share-secret" hidden><strong>Neues Passwort – jetzt sicher übernehmen:</strong><code id="live-share-password"></code></div></fieldset><fieldset><legend>Streamadressen</legend><label>Kamera<select id="live-admin-camera"></select></label><p class="setting-help">Viewer-Adresse ersetzen; Benutzername und Passwort separat in Home Assistant eintragen.</p><div id="live-share-urls"></div></fieldset><div class="dialog-actions settings-actions"><button id="live-share-discard" type="button" class="button secondary">Änderungen verwerfen</button><button class="button primary" type="submit">Freigabe speichern</button></div><p id="live-share-error" class="error" role="alert"></p></form>`;
 document.querySelector('.workspace')!.append(root,shareRoot);
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const input=(id:string)=>$<HTMLInputElement>(id);
 const form=$<HTMLFormElement>('live-camera-form'),shareForm=$<HTMLFormElement>('live-share-form'),picker=$<HTMLSelectElement>('live-admin-camera');
 const guard=formGuard(form),shareGuard=formGuard(shareForm);
 let current='',loading=0,loaded=false,busy=false,timer:ReturnType<typeof setTimeout>|undefined;
 function showSources(){const zoom=c.viewMode(current)==='dual';for(const key of ['zoomFast','zoomHigh'])root.querySelector<HTMLElement>(`[data-source="${key}"]`)!.hidden=!zoom}
 function types(){for(const key of liveKeys){const value=input('live-'+key).value;$('live-type-'+key).textContent=value.startsWith('https:')?'HTTPS · FLV':value?'RTSP · Livequelle':'Keine Quelle eingerichtet';input('live-tls-'+key).disabled=busy||!value.startsWith('https:')}}
 function importURL(key:string){
  const field=input('live-'+key);if(!field.value)return;
  try{
   const imported=importLiveURL(field.value);
   if(imported.username==null&&imported.password==null){types();return}
   const username=input('live-username'),password=input('live-password');
   const conflict=!!(username.value&&imported.username&&username.value!==imported.username||password.value&&imported.password&&password.value!==imported.password);
   if(!conflict||confirm('Die Adresse enthält abweichende Zugangsdaten. Diese für alle Quellen der Kamera übernehmen?')){
    if(imported.username)username.value=imported.username;
    if(imported.password)password.value=imported.password;
    c.notify('Zugangsdaten aus der Adresse übernommen. Zum Übernehmen speichern.');
   }else c.notify('Vorhandene Zugangsdaten beibehalten.');
   field.value=imported.url;types();form.dispatchEvent(new Event('input',{bubbles:true}));
  }catch{$('live-admin-error').textContent='Die Streamadresse ist ungültig.'}
 }
 for(const key of liveKeys)input('live-'+key).addEventListener('change',()=>importURL(key));
 function payload(){
  for(const key of liveKeys)importURL(key);
  return {enabled:input('live-enabled').checked,username:input('live-username').value,...(input('live-password').value?{password:input('live-password').value}:{}),clearPassword:input('live-clear-password').checked,shared:input('live-shared').checked,
   sources:Object.fromEntries(liveKeys.map(key=>[key,input('live-'+key).value.trim()])),
   preload:Object.fromEntries(liveKeys.map(key=>[key,$<HTMLSelectElement>('live-preload-'+key).value])),
   skipTLSVerify:Object.fromEntries(liveKeys.map(key=>[key,input('live-tls-'+key).checked]))};
 }
 const states:Record<string,string>={disconnected:'Nicht verbunden',connecting:'Verbindet …',ready:'Bereit',idle:'Nachlauf',active:'In Verwendung',error:'Verbindung fehlgeschlagen · neuer Versuch folgt'};
 async function poll(){
  clearTimeout(timer);if(root.hidden||!current||!currentPage().startsWith('admin/cameras')||document.hidden)return;const id=current;
  try{const status=await c.api<Record<string,string>>('/api/cameras/'+encodeURIComponent(id)+'/live/status');if(id!==current)return;for(const key of liveKeys)$('live-status-'+key).textContent=states[status[key]]||'Nicht verbunden'}catch{/* Configuration and playback remain available if a status refresh fails. */}
  if(!root.hidden&&currentPage().startsWith('admin/cameras')&&!document.hidden)timer=setTimeout(()=>void poll(),3000);
 }
 async function load(id:string,force=false){
  if(id===current&&loaded&&!force){if(!root.hidden)void poll();return}
  const generation=++loading;loaded=false;current=id;showSources();$('live-load-retry').hidden=true;form.hidden=true;$('live-admin-empty').hidden=!!id;$('live-admin-error').textContent='';$('live-test-result').hidden=true;
  if(!id){loaded=true;guard.clean();return}
  root.setAttribute('aria-busy','true');
  try{
   const config=await c.api<LiveConfig>('/api/cameras/'+encodeURIComponent(id)+'/live');if(generation!==loading)return;
   input('live-enabled').checked=config.enabled;input('live-username').value=config.username;input('live-password').value='';input('live-clear-password').checked=false;input('live-shared').checked=config.shared;
   for(const key of liveKeys){input('live-'+key).value=config.sources[key]||'';$<HTMLSelectElement>('live-preload-'+key).value=config.preload?.[key]||'idle';input('live-tls-'+key).checked=config.skipTLSVerify?.[key]||false}
   $('live-password-state').textContent=config.hasPassword?'Passwort gespeichert. Leer lassen, um es beizubehalten.':'Noch kein Passwort gespeichert.';types();guard.clean();form.hidden=false;loaded=true;
  }catch(error){if(generation===loading){$('live-admin-error').textContent=(error as Error).message;$('live-load-retry').hidden=false}}
  finally{if(generation===loading){root.removeAttribute('aria-busy');if(!root.hidden)void poll()}}
 }
 async function action(fn:()=>Promise<void>,errorID='live-admin-error'){
  if(busy||root.hasAttribute('aria-busy'))return;busy=true;
  const controls=[...root.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLButtonElement>('input,select,button'),...shareRoot.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLButtonElement>('input,select,button')],disabled=controls.map(control=>control.disabled);
  controls.forEach(control=>control.disabled=true);
  try{$(errorID).textContent='';await fn()}catch(error){$(errorID).textContent=(error as Error).message}
  finally{controls.forEach((control,i)=>control.disabled=disabled[i]);busy=false;types()}
 }
 form.onsubmit=event=>{event.preventDefault();void action(async()=>{await c.api('/api/cameras/'+encodeURIComponent(current)+'/live',{method:'PUT',body:JSON.stringify(payload())});guard.clean();await load(current,true);c.changed();c.notify('Liveeinstellungen gespeichert.')})};
 $('live-load-retry').onclick=()=>void load(current,true);
 $('live-discard').onclick=()=>{if(guard.leave())void load(current,true)};
 $('live-test').onclick=()=>void action(async()=>{
  const result=await c.api<Record<string,{ok:boolean;message?:string;codecs?:{streams:{codec_type:string;codec_name:string;width?:number;height?:number}[]}}>>('/api/cameras/'+encodeURIComponent(current)+'/live/test',{method:'POST',body:JSON.stringify(payload())});
  $('live-test-result').hidden=false;$('live-test-result').textContent=Object.entries(result).map(([key,value])=>key+': '+(value.ok?value.codecs?.streams.map(stream=>stream.codec_type+' '+stream.codec_name+(stream.width?' '+stream.width+'×'+stream.height:'')).join(', '):value.message)).join('\n')||'Keine Quellen eingetragen.';
 });
 $('live-remove').onclick=()=>{if(!confirm('Livekonfiguration und gespeicherte Kamerazugangsdaten entfernen?'))return;void action(async()=>{await c.api('/api/cameras/'+encodeURIComponent(current)+'/live',{method:'DELETE'});guard.clean();await load(current,true);c.changed()})};
 function showShare(value:Share){input('live-share-enabled').checked=value.enabled;input('live-share-reset').checked=false;$('live-share-status').textContent=`Benutzer: ${value.username} · ${value.enabled?(value.listening?'Freigabe aktiv':'Listener nicht verfügbar'):'Freigabe deaktiviert'} · Server: ${value.address}`;$('live-share-secret').hidden=!value.password;$('live-share-password').textContent=value.password||'';shareGuard.clean()}
 async function urls(){
  const id=picker.value,container=$('live-share-urls');container.replaceChildren();if(!id)return;
  try{const config=await c.api<LiveConfig>('/api/cameras/'+encodeURIComponent(id)+'/live');if(picker.value!==id)return;
   const zoom=c.cameras().find(camera=>camera.id===id)?.viewMode==='dual';
   for(const key of liveKeys)if(config.sources[key]&&(!key.startsWith('zoom')||zoom)){const view=key.startsWith('zoom')?'zoom':'wide',quality=key==='high'||key==='zoomHigh'?'high':'fast';const row=document.createElement('div');row.className='share-url-row';const code=document.createElement('code');code.textContent=`rtsp://<viewer-im-heimnetz>:8554/live/${encodeURIComponent(id)}/${view}/${quality}`;const copy=document.createElement('button');copy.type='button';copy.className='button secondary';copy.textContent='Kopieren';copy.setAttribute('aria-label',names[key]+' Adresse kopieren');copy.onclick=()=>void navigator.clipboard.writeText(code.textContent||'').then(()=>c.notify('Adresse kopiert.'),()=>c.notify('Kopieren nicht möglich. Adresse bitte markieren.'));row.append(code,copy);container.append(row)}
   if(!container.childElementCount)container.textContent='Noch keine Livequellen eingerichtet.';
  }catch(error){$('live-share-error').textContent=(error as Error).message}
 }
 $('live-share-back').onclick=()=>navigate('admin/cameras');
 picker.onchange=()=>void urls();
 shareForm.onsubmit=event=>{event.preventDefault();void action(async()=>{showShare(await c.api<Share>('/api/admin/live-share',{method:'PUT',body:JSON.stringify({enabled:input('live-share-enabled').checked,resetPassword:input('live-share-reset').checked})}));c.notify('Streamfreigabe gespeichert.')},'live-share-error')};
 $('live-share-discard').onclick=()=>{if(shareGuard.leave())void action(async()=>showShare(await c.api<Share>('/api/admin/live-share')),'live-share-error')};
 guard.clean();shareGuard.clean();
 registerPage('admin/live-share',{element:shareRoot,title:'Streamfreigabe',admin:true,enter:async()=>{picker.replaceChildren(...c.cameras().map(camera=>new Option(camera.name,camera.id)));$('live-share-error').textContent='';await action(async()=>{showShare(await c.api<Share>('/api/admin/live-share'));await urls()},'live-share-error')},leave:()=>{if(busy||!shareGuard.leave())return false;$('live-share-secret').hidden=true;$('live-share-password').textContent='';return true}});
 document.addEventListener('visibilitychange',()=>{if(document.hidden)clearTimeout(timer);else if(!root.hidden)void poll()});
 document.addEventListener('pagechange',()=>{if(!currentPage().startsWith('admin/cameras'))clearTimeout(timer)});
 return {root,load,leave:()=>!busy&&!root.hasAttribute('aria-busy')&&guard.leave(),show(){showSources();root.hidden=false;void poll()},hide(){root.hidden=true;clearTimeout(timer)},open(id?:string){document.dispatchEvent(new CustomEvent('cameraliveopen',{detail:id}))}};
}
