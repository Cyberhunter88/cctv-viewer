import {navigate,registerPage} from './navigation';
import {berlinEpoch} from './management';
import {berlinDate,clock} from './timeline';

export interface SearchResult {id:string;cameraId:string;cameraName:string;filename:string;start:number;duration:number}
export interface SearchPage {results:SearchResult[];total:number;offset:number;limit:number;hasMore:boolean}
interface SearchOptions {
 cameras:()=>{id:string;name:string}[];
 request:(url:string)=>Promise<SearchPage>;
 open:(result:SearchResult)=>Promise<void>;
 notify:(message:string)=>void;
}

export function initSearchPage(options:SearchOptions){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 const search=$('search-dialog'),form=$<HTMLFormElement>('search-form'),more=$<HTMLButtonElement>('search-more');
 let generation=0,params:URLSearchParams|undefined,offset=0;
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
  query.set('offset',String(offset));more.disabled=true;
  if(!append){$('search-error').textContent='';$<HTMLButtonElement>('search-submit').disabled=true}
  try{
   const data=await options.request('/api/search?'+query);
   if(own!==generation)return;
   const root=$('search-results');if(!append)root.replaceChildren();
   for(const result of data.results){
    const button=document.createElement('button');button.className='search-result';
    button.textContent=`${result.cameraName} · ${berlinDate(new Date(result.start*1000))} ${clock(result.start)} · ${result.filename} · ${Math.round(result.duration)} s`;
    button.onclick=()=>void options.open(result).catch(e=>options.notify((e as Error).message));
    root.append(button);
   }
   offset=data.offset+data.results.length;
   $('search-count').hidden=data.total===0;
   $('search-count').textContent=data.total?`${offset} von ${data.total} Treffern angezeigt`:'';
   if(!data.total)root.textContent='Keine Treffer im bisherigen Index. Weitere Tage öffnen oder Kameras scannen.';
   more.hidden=!data.hasMore;
   more.textContent=`Weitere ${Math.min(data.limit,data.total-offset)} laden`;
  }catch(e){if(own===generation)$('search-error').textContent=(e as Error).message}
  finally{if(own===generation){more.disabled=false;$<HTMLButtonElement>('search-submit').disabled=false}}
 }
 form.onsubmit=e=>{
  e.preventDefault();generation++;offset=0;
  try{
   const data=new FormData(form);params=new URLSearchParams();
   for(const [key,value] of data){if(!value)continue;params.set(key,(key==='from'||key==='to')?String(berlinEpoch(String(value).length===16?String(value)+':00':String(value))):String(value))}
   $('search-results').replaceChildren();$('search-count').hidden=true;more.hidden=true;
   void load();
  }catch(error){$('search-error').textContent=(error as Error).message}
 };
 more.addEventListener('click',()=>void load(true));
}
