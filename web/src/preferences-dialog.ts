import {confirmAction} from './confirm-dialog';
import {navigate,registerPage} from './navigation';
import {defaultView,type ViewState} from './view-state';
interface Context {
 state:ViewState;notify:(message:string)=>void;scheduleResize:()=>void;setTimelineHeight:(height:number)=>void;
 resetSelection:()=>void;applyRail:()=>void;releaseTiles:()=>void;renderCameras:()=>void;saveView:()=>Promise<void>;
 /** Captures and restores everything a reset changes, for "Rückgängig". */
 snapshot:()=>unknown;restore:(snapshot:unknown)=>void;
 notifyAction:(message:string,label:string,run:()=>void)=>void;
}
// Personal settings pages. Every valid change applies at once and is saved to the account;
// number fields wait until typing pauses, invalid values are never stored.
export function initPreferencesDialog(c:Context){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const on=(id:string,fn:()=>void|Promise<void>)=>$(id).addEventListener('click',()=>{Promise.resolve(fn()).catch(e=>c.notify(e.message))});
 const preferences=$('preferences-dialog'),preferencesForm=$<HTMLFormElement>('preferences-form'),status=$('preference-status');
 const preferenceChecks=['showTimestamps','showOverview','showThumbnails','shortcuts'] as const;
 let typingTimer:ReturnType<typeof setTimeout>|undefined,saveGeneration=0;
 function syncPreferences(value:ViewState=c.state){
  for(const key of preferenceChecks)$<HTMLInputElement>('pref-'+key).checked=value[key];
  $<HTMLSelectElement>('pref-density').value=value.density;$<HTMLSelectElement>('pref-seekStep').value=String(value.seekStep);
  for(const id of ['preload','aspect'] as const)$<HTMLSelectElement>(id).value=value[id];
  $<HTMLInputElement>('grid-columns').value=String(value.columns);$<HTMLInputElement>('grid-rows').value=String(value.rows);$<HTMLInputElement>('auto-hotspot').checked=value.autoHotspot;
  $<HTMLSelectElement>('theme').value=$<HTMLSelectElement>('login-theme').value;
 }
 function applyPreferences(){
  document.documentElement.dataset.density=c.state.density;
  document.body.classList.toggle('hide-showTimestamps',!c.state.showTimestamps);
  $('overview').hidden=!c.state.showOverview;if(preferences.hidden)syncPreferences();c.scheduleResize();
 }
 // Saves the current state and confirms it in the status line once the account has it.
 function persist(){
  const generation=++saveGeneration;
  c.saveView().then(()=>{if(generation===saveGeneration&&!status.textContent)status.textContent='Alle Änderungen gespeichert.'},()=>{});
 }
 function commit(){
  clearTimeout(typingTimer);typingTimer=undefined;
  const invalid=preferencesForm.querySelector<HTMLInputElement>(':invalid');
  if(invalid){status.textContent='Ungültiger Wert wird nicht gespeichert: '+(invalid.closest('label')?.textContent?.trim()||invalid.name);return}
  for(const key of preferenceChecks)c.state[key]=$<HTMLInputElement>('pref-'+key).checked;
  c.state.density=$<HTMLSelectElement>('pref-density').value as ViewState['density'];c.state.seekStep=Number($<HTMLSelectElement>('pref-seekStep').value);
  for(const id of ['preload','aspect'] as const)c.state[id]=$<HTMLSelectElement>(id).value;
  c.state.columns=Number($<HTMLInputElement>('grid-columns').value);c.state.rows=Number($<HTMLInputElement>('grid-rows').value);c.state.autoHotspot=$<HTMLInputElement>('auto-hotspot').checked;
  const theme=$<HTMLSelectElement>('theme').value as ViewState['theme'];
  if(theme!==c.state.theme){c.state.theme=theme;window.dispatchEvent(new CustomEvent('themecommit',{detail:theme}))}
  applyPreferences();c.renderCameras();persist();
 }
 for(const tab of ['appearance','playback'])registerPage('settings/'+tab,{element:preferences,group:'preferences',title:tab==='appearance'?'Darstellung':'Wiedergabe',enter:()=>{syncPreferences();status.textContent='';$('preferences-title').textContent=tab==='appearance'?'Darstellung':'Wiedergabe';for(const section of preferences.querySelectorAll<HTMLElement>('[data-preference-page]'))section.hidden=section.dataset.preferencePage!==tab},
  // A value typed just before leaving is still stored.
  leave:()=>{if(typingTimer!==undefined)commit();return true}});
 on('preferences-close',()=>navigate('recordings'));
 preferences.addEventListener('cancel',e=>{e.preventDefault();navigate('recordings')});
 preferencesForm.noValidate=true;
 preferencesForm.addEventListener('change',commit);
 preferencesForm.addEventListener('input',e=>{if(e.target instanceof HTMLInputElement&&e.target.type==='number'){clearTimeout(typingTimer);typingTimer=setTimeout(commit,600)}});
 preferencesForm.addEventListener('submit',e=>{e.preventDefault();commit()});
 on('preferences-reset',async()=>{
  if(!await confirmAction({title:'Einstellungen zurücksetzen',message:'Persönliche Einstellungen und Kameraauswahl werden auf die Standardwerte zurückgesetzt.',confirm:'Zurücksetzen',danger:true}))return;
  clearTimeout(typingTimer);typingTimer=undefined;
  const previous=c.snapshot();
  Object.assign(c.state,defaultView());delete c.state.railCollapsed;c.state.theme='system';c.resetSelection();
  for(const id of ['layout','quality','speed'] as const)$<HTMLSelectElement>(id).value=c.state[id];
  window.dispatchEvent(new CustomEvent('themecommit',{detail:'system'}));
  c.setTimelineHeight(c.state.timelineHeight);c.applyRail();applyPreferences();c.releaseTiles();c.renderCameras();syncPreferences();
  status.textContent='';persist();
  // The reset is saved at once; the message offers to restore the previous settings.
  c.notifyAction('Persönliche Einstellungen zurückgesetzt.','Rückgängig',()=>{c.restore(previous);syncPreferences();c.notify('Persönliche Einstellungen wiederhergestellt.')});
 });
 return {applyPreferences};
}
