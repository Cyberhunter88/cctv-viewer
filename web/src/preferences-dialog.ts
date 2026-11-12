import {navigate,registerPage} from './navigation';
import {defaultView,type ViewState} from './view-state';
interface Context {
 state:ViewState;notify:(message:string)=>void;scheduleResize:()=>void;setTimelineHeight:(height:number)=>void;
 resetSelection:()=>void;applyRail:()=>void;releaseTiles:()=>void;renderCameras:()=>void;saveView:()=>Promise<void>;
}
// Personal settings pages. Changes stay a draft until the form is saved.
export function initPreferencesDialog(c:Context){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const on=(id:string,fn:()=>void|Promise<void>)=>$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>c.notify(e.message))});
 const preferences=$('preferences-dialog'),preferencesForm=$<HTMLFormElement>('preferences-form');
 let preferencesBaseline='',resetPreferencesPending=false,preferencesSaving=false;
 const preferenceChecks=['showNames','showTimestamps','showCodec','showOverview','showThumbnails','shortcuts'] as const;
 function preferencesSnapshot(){return JSON.stringify(Array.from(preferencesForm.querySelectorAll<HTMLInputElement|HTMLSelectElement>('input,select')).map(e=>[e.id,e.type==='checkbox'?(e as HTMLInputElement).checked:e.value]))}
 function updatePreferencesSave(){const dirty=resetPreferencesPending||preferencesSnapshot()!==preferencesBaseline;$<HTMLButtonElement>('preferences-save').disabled=!dirty;preferencesForm.dataset.dirty=String(dirty)}
 function syncPreferences(value:ViewState=c.state){
  for(const key of preferenceChecks)$<HTMLInputElement>('pref-'+key).checked=value[key];
  $<HTMLSelectElement>('pref-density').value=value.density;$<HTMLSelectElement>('pref-seekStep').value=String(value.seekStep);$<HTMLInputElement>('pref-timelineHeight').value=String(value.timelineHeight);
  for(const id of ['preload','aspect'] as const)$<HTMLSelectElement>(id).value=value[id];
  $<HTMLInputElement>('grid-columns').value=String(value.columns);$<HTMLInputElement>('grid-rows').value=String(value.rows);$<HTMLInputElement>('auto-hotspot').checked=value.autoHotspot;
  $<HTMLSelectElement>('theme').value=$<HTMLSelectElement>('login-theme').value;
 }
 function applyPreferences(){
  document.documentElement.dataset.density=c.state.density;
  for(const key of ['showNames','showTimestamps','showCodec'] as const)document.body.classList.toggle('hide-'+key,!c.state[key]);
  $('overview').hidden=!c.state.showOverview;$('thumbnails').hidden=!c.state.showThumbnails;if(preferences.hidden)syncPreferences();c.scheduleResize();
 }
 function openPreferences(){if(preferencesForm.dataset.draftActive==='true')return;preferencesForm.dataset.draftActive='true';syncPreferences();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();preferencesForm.dataset.dirty='false'}
 function leavePreferences(){if(preferencesSaving)return false;if((resetPreferencesPending||preferencesSnapshot()!==preferencesBaseline)&&!confirm('Ungespeicherte Einstellungen verwerfen?'))return false;preferencesForm.dataset.draftActive='false';syncPreferences();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();return true}
 function closePreferences(){navigate('recordings')}
 for(const tab of ['appearance','playback'])registerPage('settings/'+tab,{element:preferences,group:'preferences',title:tab==='appearance'?'Darstellung':'Wiedergabe',leave:leavePreferences,enter:()=>{openPreferences();$('preferences-title').textContent=tab==='appearance'?'Darstellung':'Wiedergabe';for(const section of preferences.querySelectorAll<HTMLElement>('[data-preference-page]'))section.hidden=section.dataset.preferencePage!==tab}});
 on('preferences-close',closePreferences);on('preferences-cancel',closePreferences);
 preferences.addEventListener('cancel',e=>{e.preventDefault();closePreferences()});
 preferencesForm.addEventListener('input',updatePreferencesSave);preferencesForm.addEventListener('change',updatePreferencesSave);
 preferencesForm.noValidate=true;
 preferencesForm.addEventListener('submit',async e=>{
  e.preventDefault();const invalid=preferencesForm.querySelector<HTMLInputElement>(':invalid');if(invalid){const tab=invalid.closest<HTMLElement>('[data-preference-page]')?.dataset.preferencePage;if(tab)navigate('settings/'+tab);preferencesForm.reportValidity();return;}
  const theme=$<HTMLSelectElement>('theme').value;
  if(resetPreferencesPending){Object.assign(c.state,defaultView());delete c.state.railCollapsed;c.resetSelection();for(const id of ['layout','quality','speed'] as const)$<HTMLSelectElement>(id).value=c.state[id]}
  for(const key of preferenceChecks)c.state[key]=$<HTMLInputElement>('pref-'+key).checked;
  c.state.density=$<HTMLSelectElement>('pref-density').value as ViewState['density'];c.state.seekStep=Number($<HTMLSelectElement>('pref-seekStep').value);
  for(const id of ['preload','aspect'] as const)c.state[id]=$<HTMLSelectElement>(id).value;
  c.state.columns=Number($<HTMLInputElement>('grid-columns').value);c.state.rows=Number($<HTMLInputElement>('grid-rows').value);c.state.autoHotspot=$<HTMLInputElement>('auto-hotspot').checked;
  c.setTimelineHeight(Number($<HTMLInputElement>('pref-timelineHeight').value));
  c.state.theme=theme as ViewState['theme'];window.dispatchEvent(new CustomEvent('themecommit',{detail:theme}));
  c.applyRail();applyPreferences();if(resetPreferencesPending)c.releaseTiles();c.renderCameras();const saving=c.saveView();resetPreferencesPending=false;preferencesBaseline=preferencesSnapshot();updatePreferencesSave();preferencesSaving=true;try{await saving;preferencesSaving=false;if(preferencesSnapshot()===preferencesBaseline){preferencesForm.dataset.dirty='false';navigate('recordings');c.notify('Persönliche Einstellungen gespeichert.')}else c.notify('Einstellungen gespeichert. Weitere Änderungen sind noch nicht gespeichert.')}catch{}finally{preferencesSaving=false}
 });
 on('preferences-reset',()=>{
  if(!confirm('Persönliche Einstellungen und Kameraauswahl auf die Standardwerte zurücksetzen? Erst Speichern übernimmt die Änderung.'))return;
  syncPreferences(defaultView());$<HTMLSelectElement>('theme').value='system';resetPreferencesPending=true;updatePreferencesSave();
 });
 return {applyPreferences};
}
