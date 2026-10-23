import {registerPage,navigate} from './navigation';
import type {initLiveAdmin} from './live-admin';
type LiveAdmin=ReturnType<typeof initLiveAdmin>;
export function initCameraSettings(form:HTMLFormElement,dialog:HTMLElement,live:LiveAdmin,first:()=>string,selectCamera:(id:string)=>boolean,cameraLeave:()=>boolean,reset:()=>void){
 const maintenance=dialog.querySelector<HTMLElement>('.camera-maintenance');
 if(maintenance){const details=document.createElement('details');details.className='camera-maintenance';const summary=document.createElement('summary');summary.textContent='Indexierung und Wartung';details.append(summary,...Array.from(maintenance.children));maintenance.replaceWith(details)}
 const editor=document.createElement('section');editor.className='camera-settings-editor';form.before(editor);
 const heading=form.querySelector<HTMLElement>('.camera-editor-heading')!;editor.append(heading);
 const empty=document.createElement('div');empty.className='camera-editor-empty';empty.innerHTML='<h3>Kamera auswählen</h3><p>Wähle links eine Kamera zum Bearbeiten oder füge eine neue hinzu.</p>';
 const tabs=document.createElement('div');tabs.className='settings-tabs';tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Kameraeinstellungen');
 const general=document.createElement('section'),recordings=document.createElement('section');
 general.id='camera-general-panel';recordings.id='camera-recordings-panel';
 const name=form.querySelector('[name=name]')!.closest('label')!,folder=form.querySelector('[name=folder]')!.closest('label')!,views=form.querySelector('#view-mode')!.closest('label')!,help=views.nextElementSibling!;
 general.append(name,views,help);
 recordings.append(folder,form.querySelector('#dual-fields')!,form.querySelector('#camera-advanced')!);
 for(const h of form.querySelectorAll('h4'))h.remove();
 for(const row of form.querySelectorAll('.form-row'))if(!row.children.length)row.remove();
 const firstAction=form.querySelector('.camera-editor-actions')!;firstAction.before(general,recordings);
 editor.append(tabs,empty,form,live.root);
 let active='general',opened=false,previousID='';
 const buttons=new Map<string,HTMLButtonElement>();
 for(const [key,title,panel] of [['general','Allgemein',general],['recordings','Aufnahmen',recordings],['live','Live',live.root]] as const){
  const button=document.createElement('button');button.type='button';button.textContent=title;button.id='camera-tab-'+key;button.setAttribute('role','tab');button.setAttribute('aria-controls',panel.id);panel.setAttribute('role','tabpanel');panel.setAttribute('aria-labelledby',button.id);
  button.onclick=()=>show(key);buttons.set(key,button);tabs.append(button);
 }
 tabs.addEventListener('keydown',event=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const keys=[...buttons.keys()],index=keys.indexOf(active);const key=event.key==='Home'?keys[0]:event.key==='End'?keys.at(-1)!:keys[(index+(event.key==='ArrowRight'?1:2))%3];show(key);buttons.get(key)!.focus()});
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
 return {canChange,leave,show,selected,clear(){opened=false;previousID='';void live.load('');show('general')}};
}
