type Page={element:HTMLElement;title:string;admin?:boolean;group?:string;enter?:()=>void|Promise<void>;leave?:(next:string)=>boolean};
const pages=new Map<string,Page>();
let current='',historyIndex=0,isAdmin=()=>false,onLeaveRecordings=()=>{};
export const currentPage=()=>current;
export function registerPage(path:string,page:Page){pages.set(path,page);page.element.hidden=true}
export function navigate(path:string){switchPage(path,false)}
function switchPage(path:string,fromHistory:boolean){
 if(!pages.has(path)||pages.get(path)!.admin&&!isAdmin())path='recordings';
 if(path===current){if(fromHistory&&location.hash!=='#'+path)history.replaceState({...history.state,cctvPageIndex:historyIndex},'','#'+path);return;}
 const previous=pages.get(current),next=pages.get(path)!;
 const targetIndex=typeof history.state?.cctvPageIndex==='number'?history.state.cctvPageIndex:historyIndex+1;
 if(previous?.leave&&(!previous.group||previous.group!==next.group)&&!previous.leave(path)){
  if(fromHistory){const delta=historyIndex-targetIndex;if(delta)history.go(delta);else history.replaceState({...history.state,cctvPageIndex:historyIndex},'','#'+current)}return;
 }
 if(current==='recordings')onLeaveRecordings();
 for(const page of pages.values())page.element.hidden=page.element!==next.element;
 const initial=!current;current=path;
 if(!fromHistory){historyIndex++;history.pushState({...history.state,cctvPageIndex:historyIndex},'','#'+path)}else{historyIndex=initial?(typeof history.state?.cctvPageIndex==='number'?history.state.cctvPageIndex:0):targetIndex;history.replaceState({...history.state,cctvPageIndex:historyIndex},'','#'+path)}
 document.title='CCTV Viewer · '+next.title;
 for(const link of document.querySelectorAll<HTMLAnchorElement>('[data-page]')){
  const active=link.dataset.page===path||link.dataset.page==='admin/cameras'&&path.startsWith('admin/cameras/');link.classList.toggle('active',active);
  if(active)link.setAttribute('aria-current','page');else link.removeAttribute('aria-current');
  if(active){const group=link.closest('details');if(group)group.open=true}
 }
 document.documentElement.classList.remove('mobile-nav-open');
 document.getElementById('mobile-nav-toggle')?.setAttribute('aria-expanded','false');
 document.dispatchEvent(new CustomEvent('pagechange',{detail:path}));
 void Promise.resolve(next.enter?.()).catch(e=>{document.dispatchEvent(new CustomEvent('pageerror',{detail:(e as Error).message}))});
 if(!initial){const heading=next.element.querySelector<HTMLElement>('h1,h2,h3');if(heading){heading.tabIndex=-1;heading.focus({preventScroll:true})}}
 window.scrollTo(0,0);
}
export function startNavigation(admin:()=>boolean,pause:()=>void){
 isAdmin=admin;onLeaveRecordings=pause;
 for(const node of document.querySelectorAll<HTMLElement>('[data-admin]'))node.hidden=!isAdmin();
 document.addEventListener('click',event=>{const link=(event.target as Element).closest<HTMLAnchorElement>('a[data-page]');if(!link||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey||event.button!==0)return;event.preventDefault();navigate(link.dataset.page!)});
 window.addEventListener('hashchange',()=>switchPage(location.hash.slice(1)||'recordings',true));
 window.addEventListener('beforeunload',event=>{if(document.querySelector('[data-dirty="true"]')){event.preventDefault();event.returnValue=''}});
 switchPage(location.hash.slice(1)||'recordings',true);
}
export function formGuard(form:HTMLFormElement){
 const fields=()=>Array.from(form.elements).filter((e):e is HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement=>e instanceof HTMLInputElement||e instanceof HTMLSelectElement||e instanceof HTMLTextAreaElement);
 const value=(e:HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement)=>e instanceof HTMLInputElement&&(e.type==='checkbox'||e.type==='radio')?e.checked:e.value;
 let baseline:ReturnType<typeof value>[]=[];
 const dirty=()=>fields().some((e,i)=>value(e)!==baseline[i]);
 const update=()=>form.dataset.dirty=String(dirty());
 form.addEventListener('input',update);form.addEventListener('change',update);
 return {clean(){baseline=fields().map(value);update()},leave(){if(!dirty())return true;if(!confirm('Ungespeicherte Änderungen verwerfen?'))return false;fields().forEach((e,i)=>{if(e instanceof HTMLInputElement&&(e.type==='checkbox'||e.type==='radio'))e.checked=!!baseline[i];else e.value=String(baseline[i]??'')});update();return true}};
}
