import {navigate,registerPage} from './navigation';
import {berlinEpoch} from './management';
import {berlinDate,clock} from './timeline';

export interface SearchResult {id:string;cameraId:string;cameraName:string;filename:string;start:number;duration:number}
export interface SearchPage {results:SearchResult[];total:number;offset:number;limit:number;hasMore:boolean;nextCursor?:string}
interface SearchOptions {
 cameras:()=>{id:string;name:string}[];
 request:(url:string)=>Promise<SearchPage>;
 open:(result:SearchResult)=>Promise<void>;
 notify:(message:string)=>void;
}

export function initSearchPage(options:SearchOptions){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const search=$('search-dialog'),form=$<HTMLFormElement>('search-form'),more=$<HTMLButtonElement>('search-more');
 let generation=0,params:URLSearchParams|undefined,offset=0,cursor='',total=0,lastDay='';
 const sortLabel=document.createElement('label');sortLabel.textContent='Sortierung';
 const sort=document.createElement('select');sort.name='order';sort.add(new Option('Neueste zuerst','desc'));sort.add(new Option('Älteste zuerst','asc'));sortLabel.append(sort);
 form.querySelector('#search-submit')?.before(sortLabel);
 registerPage('search',{element:search,title:'Suche',enter:()=>{
  const select=$<HTMLSelectElement>('search-camera'),previous=select.value;
  select.replaceChildren(new Option('Alle Kameras',''));
  for(const camera of options.cameras())select.add(new Option(camera.name,camera.id));
  select.value=previous;
 }});
 $('search-close').addEventListener('click',()=>navigate('recordings'));
 async function load(append=false){
  if(!params)return;
  const own=generation,query=new URLSearchParams(params);
  query.set('offset',String(offset));if(cursor){query.set('cursor',cursor);query.set('count','0')}more.disabled=true;
  if(!append){$('search-error').textContent='';$<HTMLButtonElement>('search-submit').disabled=true}
  try{
   const data=await options.request('/api/search?'+query);
   if(own!==generation)return;
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
   if(!total)root.textContent='Keine Treffer im bisherigen Index. Weitere Tage öffnen oder Kameras scannen.';
   more.hidden=!data.hasMore;
   more.textContent=`Weitere ${Math.min(data.limit,total-offset)} laden`;
  }catch(e){if(own===generation)$('search-error').textContent=(e as Error).message}
  finally{if(own===generation){more.disabled=false;$<HTMLButtonElement>('search-submit').disabled=false}}
 }
 form.onsubmit=e=>{
  e.preventDefault();generation++;offset=0;cursor='';total=0;lastDay='';
  try{
   const data=new FormData(form);params=new URLSearchParams();
   for(const [key,value] of data){if(!value)continue;params.set(key,(key==='from'||key==='to')?String(berlinEpoch(String(value).length===16?String(value)+':00':String(value))):String(value))}
   $('search-results').replaceChildren();$('search-count').hidden=true;more.hidden=true;
   void load();
  }catch(error){$('search-error').textContent=(error as Error).message}
 };
 more.addEventListener('click',()=>void load(true));
}
