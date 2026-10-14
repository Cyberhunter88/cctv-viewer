import {clamp} from './timeline.ts';
export function attachTileZoom(container:HTMLElement,video:HTMLVideoElement,name:string):()=>void {
 let scale=1,x=0,y=0,expanded=false,oldFocus:HTMLElement|null=null;
 const controls=document.createElement('div');controls.className='tile-controls';const buttons:HTMLButtonElement[]=[];
 const apply=()=>{const width=container.clientWidth,height=container.clientHeight;x=clamp(x,-width*(scale-1)/2,width*(scale-1)/2);y=clamp(y,-height*(scale-1)/2,height*(scale-1)/2);video.style.transform=`translate(${x}px,${y}px) scale(${scale})`;buttons[3].textContent=`${Math.round(scale*100)} %`};
 const zoom=(factor:number)=>{scale=clamp(scale*factor,1,8);apply()};
 const close=()=>{expanded=false;container.classList.remove('expanded');buttons[0].textContent='⛶';buttons[0].setAttribute('aria-label',name+' vergrößern');if(document.fullscreenElement===container)void document.exitFullscreen().catch(()=>{});oldFocus?.focus()};
 const toggle=async()=>{if(expanded){close();return}oldFocus=document.activeElement as HTMLElement;expanded=true;container.classList.add('expanded');buttons[0].textContent='✕';buttons[0].setAttribute('aria-label',name+' Vollbild schließen');container.focus();try{await container.requestFullscreen?.()}catch{/* The fixed overlay remains usable. */}apply()};
 const add=(label:string,text:string,fn:()=>void)=>{const b=document.createElement('button');b.type='button';b.className='icon-button';b.setAttribute('aria-label',name+' '+label);b.title=label;b.textContent=text;b.onclick=fn;buttons.push(b);controls.append(b)};
 add('vergrößern','⛶',()=>void toggle());add('herauszoomen','−',()=>zoom(1/1.25));add('hineinzoomen','+',()=>zoom(1.25));add('Zoom zurücksetzen','100 %',()=>{scale=1;x=y=0;apply()});container.append(controls);container.tabIndex=0;
 const pointers=new Map<number,{x:number;y:number}>();let distance=0;
 video.addEventListener('pointerdown',e=>{pointers.set(e.pointerId,{x:e.clientX,y:e.clientY});video.setPointerCapture(e.pointerId);distance=0});
 video.addEventListener('pointermove',e=>{const prev=pointers.get(e.pointerId);if(!prev)return;pointers.set(e.pointerId,{x:e.clientX,y:e.clientY});if(pointers.size===2){const [a,b]=[...pointers.values()];const next=Math.hypot(a.x-b.x,a.y-b.y);if(distance>0)zoom(next/distance);distance=next}else{distance=0;if(scale>1){x+=e.clientX-prev.x;y+=e.clientY-prev.y;apply()}}});
 for(const event of ['pointerup','pointercancel','lostpointercapture'])video.addEventListener(event,e=>{pointers.delete((e as PointerEvent).pointerId);distance=0});
 video.addEventListener('wheel',e=>{e.preventDefault();zoom(e.deltaY<0?1.25:1/1.25)},{passive:false});
 container.addEventListener('keydown',e=>{if(e.target instanceof HTMLButtonElement&&e.key!=='Escape')return;if(e.key==='Escape'){if(expanded){e.preventDefault();close()}return}if(!['+','=','-','0','ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key))return;e.preventDefault();if(e.key==='+'||e.key==='=')zoom(1.25);if(e.key==='-')zoom(1/1.25);if(e.key==='0'){scale=1;x=y=0}if(e.key==='ArrowLeft')x+=25;if(e.key==='ArrowRight')x-=25;if(e.key==='ArrowUp')y+=25;if(e.key==='ArrowDown')y-=25;apply()});
 const escape=(e:KeyboardEvent)=>{if(e.key==='Escape'&&expanded)close()};const fullscreen=()=>{if(!document.fullscreenElement&&expanded)close()};document.addEventListener('keydown',escape);document.addEventListener('fullscreenchange',fullscreen);
 const resize=new ResizeObserver(apply);resize.observe(container);
 return ()=>{if(expanded)close();resize.disconnect();document.removeEventListener('keydown',escape);document.removeEventListener('fullscreenchange',fullscreen)};
}
