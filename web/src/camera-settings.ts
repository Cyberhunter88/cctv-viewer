import {registerPage,navigate} from './navigation';
import type {initLiveAdmin} from './live-admin';
type LiveAdmin=ReturnType<typeof initLiveAdmin>;
export function initCameraSettings(form:HTMLFormElement,dialog:HTMLElement,live:LiveAdmin,first:()=>string,selectCamera:(id:string)=>boolean,cameraLeave:()=>boolean,reset:()=>void){
 // The markup of the page lives in index.html; this module only wires tabs and the live panel.
 const byId=(id:string)=>document.getElementById(id) as HTMLElement;
 const editor=dialog.querySelector<HTMLElement>('.camera-settings-editor')!;
 const heading=editor.querySelector<HTMLElement>('.camera-editor-heading')!,empty=editor.querySelector<HTMLElement>('.camera-editor-empty')!,tabs=editor.querySelector<HTMLElement>('[role=tablist]')!;
 const general=byId('camera-general-panel'),recordings=byId('camera-recordings-panel');
 editor.append(live.root);
 let active='general',opened=false,previousID='';
 const buttons=new Map<string,HTMLButtonElement>();
 for(const [key,panel] of [['general',general],['recordings',recordings],['live',live.root]] as const){
  const button=byId('camera-tab-'+key) as HTMLButtonElement;
  panel.setAttribute('role','tabpanel');panel.setAttribute('aria-labelledby',button.id);
  button.onclick=()=>show(key);buttons.set(key,button);
 }
 tabs.addEventListener('keydown',event=>{
  if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
  event.preventDefault();
  const keys=[...buttons.keys()],index=keys.indexOf(active);
  const key=event.key==='Home'?keys[0]:event.key==='End'?keys.at(-1)!:keys[(index+(event.key==='ArrowRight'?1:2))%3];
  show(key);
  buttons.get(key)!.focus()
 });
 function id(){return (form.elements.namedItem('id') as HTMLInputElement).value}
 function show(key:string){
  active=key;empty.hidden=opened;heading.hidden=!opened;tabs.hidden=!opened;
  form.hidden=!opened||key==='live';general.hidden=key!=='general';recordings.hidden=key!=='recordings';
  if(opened&&key==='live'){live.show();void live.load(id())}else live.hide();
  for(const [name,button] of buttons){button.setAttribute('aria-selected',String(name===key));button.tabIndex=name===key?0:-1}
  dialog.classList.toggle('camera-editing',opened);
 }
 function selected(){opened=true;if(previousID!==id()){previousID=id();void live.load(id());active='general'}show(active)}
 form.addEventListener('cameraeditorchange',selected);
 document.addEventListener('cameraliveopen',event=>{
  const target=(event as CustomEvent<string|undefined>).detail||id()||first();
  if(target!==id()){if(!canChange())return;if(!selectCamera(target))return}
  opened=true;navigate('admin/cameras/live');show('live');
 });
 function canChange(){return live.leave()}
 function leave(){if(!live.leave())return false;reset();opened=false;previousID='';void live.load('');show('general');return true}
 registerPage('admin/cameras/live',{element:dialog,title:'Kamera · Live',admin:true,enter:()=>{
  if(!id()&&first())selectCamera(first());opened=true;show('live');
 },leave:next=>next==='admin/cameras'||cameraLeave()&&leave()});
 form.addEventListener('invalid',event=>{const target=event.target as HTMLElement;show(recordings.contains(target)?'recordings':'general')},true);
 show('general');
 // Page level tabs: the camera workspace and the global maintenance panel.
 const sections=new Map([['list',byId('camera-section-list')],['maintenance',byId('camera-section-maintenance')]]);
 const panels={list:byId('camera-workspace'),maintenance:byId('camera-maintenance-panel')};
 function showSection(key:'list'|'maintenance'){
  for(const [name,button] of sections){button.setAttribute('aria-selected',String(name===key));button.tabIndex=name===key?0:-1}
  panels.list.hidden=key!=='list';panels.maintenance.hidden=key!=='maintenance';
 }
 for(const [key,button] of sections)button.onclick=()=>showSection(key as 'list'|'maintenance');
 byId('camera-section-list').parentElement!.addEventListener('keydown',event=>{
  if(!['ArrowLeft','ArrowRight'].includes(event.key))return;
  event.preventDefault();
  const key=event.key==='ArrowRight'?'maintenance':'list';
  showSection(key);
  sections.get(key)!.focus()
 });
 return {showSection,canChange,leave,show,selected,clear(){opened=false;previousID='';void live.load('');show('general')}};
}
