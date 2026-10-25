import {navigate,registerPage} from './navigation';
import {createLatestRequest} from './latest-request';
import {berlinEpoch} from './management';
import {berlinDate,clock} from './timeline';

export interface SearchResult {id:string;cameraId:string;cameraName:string;filename:string;start:number;duration:number}
export interface SearchPage {results:SearchResult[];total:number;offset:number;limit:number;hasMore:boolean;nextCursor?:string}
interface SearchOptions {
 cameras:()=>{id:string;name:string}[];
 request:(url:string,signal:AbortSignal)=>Promise<SearchPage>;
 open:(result:SearchResult)=>Promise<void>;
 notify:(message:string)=>void;
}

export function initSearchPage(options:SearchOptions){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const search=$('search-dialog'),form=$<HTMLFormElement>('search-form'),more=$<HTMLButtonElement>('search-more');
 const requests=createLatestRequest();
 let hasMore=false,retryInitial=false;
 let generation=0,params:URLSearchParams|undefined,offset=0,cursor='',total=0,lastDay='';
 const sortLabel=document.createElement('label');sortLabel.textContent='Sortierung';
 const sort=document.createElement('select');sort.name='order';sort.add(new Option('Neueste zuerst','desc'));sort.add(new Option('Älteste zuerst','asc'));sortLabel.append(sort);
 form.querySelector('#search-submit')?.before(sortLabel);
 const searchStatus=document.createElement('p');searchStatus.id='search-status';searchStatus.className='muted';searchStatus.setAttribute('role','status');form.after(searchStatus);
 const retry=document.createElement('button');retry.type='button';retry.className='button secondary';retry.textContent='Suche erneut versuchen';retry.hidden=true;$('search-error').after(retry);
 let retryAppend=false;
 function cancelSearch(){generation++;requests.cancel();more.disabled=false;$<HTMLButtonElement>('search-submit').disabled=false;search.removeAttribute('aria-busy');searchStatus.textContent=''}
 registerPage('search',{element:search,title:'Suche',leave:()=>{cancelSearch();return true},enter:()=>{
  const select=$<HTMLSelectElement>('search-camera'),previous=select.value;
  select.replaceChildren(new Option('Alle Kameras',''));
  for(const camera of options.cameras())select.add(new Option(camera.name,camera.id));
  select.value=previous;
  if(retryInitial)void load();
 }});
 $('search-close').addEventListener('click',()=>navigate('recordings'));
 async function load(append=false){
  if(!params)return;
  if(!append)retryInitial=true;
  const request=requests.begin();
  const own=++generation,query=new URLSearchParams(params);
  query.set('offset',String(offset));if(cursor){query.set('cursor',cursor);query.set('count','0')}more.disabled=true;
  $('search-error').textContent='';retry.hidden=true;searchStatus.textContent=append?'Weitere Treffer werden geladen …':'Suche läuft …';search.setAttribute('aria-busy','true');
  try{
   const data=await options.request('/api/search?'+query,request.signal);
   if(own!==generation||!request.current())return;
   retryInitial=false;
   const root=$('search-results');if(!append){root.replaceChildren();total=data.total;lastDay=''}
   for(const result of data.results){
    const day=berlinDate(new Date(result.start*1000));
    if(day!==lastDay){const heading=document.createElement('h3');heading.className='search-day';heading.textContent=day;root.append(heading);lastDay=day}
    const button=document.createElement('button');button.className='search-result';
    const thumb=document.createElement('img');thumb.className='search-preview';thumb.loading='lazy';thumb.alt='';thumb.src='/api/recordings/'+encodeURIComponent(result.id)+'/thumbnail';thumb.onerror=()=>{thumb.hidden=true};
    const label=document.createElement('span');label.textContent=`${result.cameraName} · ${clock(result.start)} · ${result.filename} · ${Math.round(result.duration)} s`;
    button.append(thumb,label);
    button.onclick=()=>void options.open(result).catch(e=>options.notify((e as Error).message));
    root.append(button);
   }
   offset+=data.results.length;cursor=data.nextCursor||'';
   $('search-count').hidden=total===0;
   $('search-count').textContent=total?`${offset} von ${total} Treffern angezeigt`:'';
   if(!total)root.textContent='Keine Treffer für diese Suche im bisherigen Index. Prüfe die Filter oder öffne weitere Tage beziehungsweise starte einen Kamerascan.';
   hasMore=data.hasMore;more.hidden=!hasMore;
   more.textContent=`Weitere ${Math.min(data.limit,total-offset)} laden`;
  }catch(e){if(own===generation&&request.current()&&(e as Error).name!=='AbortError'){retryInitial=false;retryAppend=append;$('search-error').textContent=(e as Error).message;retry.hidden=false}}
  finally{if(own===generation&&request.current()){more.disabled=false;$<HTMLButtonElement>('search-submit').disabled=false;search.removeAttribute('aria-busy');searchStatus.textContent=''}}
 }
 form.onsubmit=e=>{
  e.preventDefault();cancelSearch();retryInitial=false;retry.hidden=true;params=undefined;offset=0;cursor='';total=0;lastDay='';hasMore=false;
  try{
   const data=new FormData(form);params=new URLSearchParams();
   for(const [key,value] of data){if(!value)continue;params.set(key,(key==='from'||key==='to')?String(berlinEpoch(String(value).length===16?String(value)+':00':String(value))):String(value))}
   $('search-results').replaceChildren();$('search-count').hidden=true;more.hidden=true;
   void load();
  }catch(error){$('search-error').textContent=(error as Error).message}
 };
 more.addEventListener('click',()=>{if(hasMore&&!more.disabled)void load(true)});
 retry.addEventListener('click',()=>void load(retryAppend));
 window.addEventListener('pagehide',cancelSearch);
 window.addEventListener('pageshow',event=>{if(event.persisted&&!search.hidden&&retryInitial)void load()});
}
