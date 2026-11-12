import {maxSavedViews,matchingSavedView,upsertSavedView,type SavedView} from './view-state';
interface Context {
 views:()=>SavedView[];setViews:(views:SavedView[])=>void;
 current:()=>{selected:ReadonlySet<string>;layout:string;columns:number;rows:number};
 apply:(view:SavedView)=>void;notify:(message:string)=>void;
}
// Named combinations of camera selection and grid, stored with the personal settings.
export function initSavedViews(c:Context){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const select=$<HTMLSelectElement>('saved-view'),form=$<HTMLFormElement>('saved-view-form'),name=$<HTMLInputElement>('saved-view-name'),remove=$<HTMLButtonElement>('saved-view-delete'),open=$<HTMLButtonElement>('saved-view-save');
 // Reflects the current state: a view stays selected only while nothing differs from it.
 function sync(){
  const views=c.views(),{selected,layout,columns,rows}=c.current(),active=matchingSavedView(views,selected,layout,columns,rows);
  const options=[new Option(views.length?'Gespeicherte Ansicht wählen':'Keine gespeicherte Ansicht','')];
  for(const view of views)options.push(new Option(view.name,view.name));
  select.replaceChildren(...options);select.value=active;select.disabled=!views.length;remove.hidden=!active;
 }
 function close(focus=true){form.hidden=true;open.setAttribute('aria-expanded','false');if(focus)open.focus()}
 select.addEventListener('change',()=>{const view=c.views().find(view=>view.name===select.value);if(view)c.apply(view);else sync()});
 open.addEventListener('click',()=>{
  if(!form.hidden){close();return}
  form.hidden=false;open.setAttribute('aria-expanded','true');name.value=select.value;name.focus();name.select();
 });
 $('saved-view-cancel').addEventListener('click',()=>close());
 form.addEventListener('keydown',event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close()}});
 form.addEventListener('submit',event=>{
  event.preventDefault();
  const label=name.value.trim(),{selected,layout,columns,rows}=c.current();
  if(!label){name.focus();return}
  const exists=c.views().some(view=>view.name===label);
  if(exists&&!confirm(`Ansicht „${label}“ überschreiben?`))return;
  const next=upsertSavedView(c.views(),{name:label,cameras:[...selected],layout,columns,rows});
  if(!next){c.notify(`Höchstens ${maxSavedViews} Ansichten möglich. Bitte zuerst eine Ansicht löschen.`);return}
  c.setViews(next);close();sync();c.notify(exists?`Ansicht „${label}“ aktualisiert.`:`Ansicht „${label}“ gespeichert.`);
 });
 remove.addEventListener('click',()=>{
  const label=select.value;if(!label||!confirm(`Ansicht „${label}“ löschen?`))return;
  c.setViews(c.views().filter(view=>view.name!==label));sync();select.focus();c.notify(`Ansicht „${label}“ gelöscht.`);
 });
 return {sync};
}
