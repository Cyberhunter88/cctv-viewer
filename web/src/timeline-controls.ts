import {clamp,clock} from './timeline.ts';
type Context={shortcuts:()=>boolean;seekStep:()=>number;bounds:()=>{from:number;to:number;cursor:number;viewStart:number;viewEnd:number};seek:(time:number)=>void;zoom:(factor:number,anchor?:number)=>void;pan:(start:number,end:number)=>void};
export function attachTimelineControls(canvas:HTMLCanvasElement,c:Context){
 const timeAt=(x:number)=>{const rect=canvas.getBoundingClientRect(),b=c.bounds();return b.viewStart+clamp((x-rect.left-85)/Math.max(1,rect.width-100),0,1)*(b.viewEnd-b.viewStart)};
 const tooltip=document.getElementById('timeline-tooltip');
 const pointers=new Map<number,{x:number;y:number}>();let initialX=0,initialStart=0,initialEnd=0,distance=0,moved=false;
 canvas.addEventListener('pointerdown',e=>{if(tooltip)tooltip.hidden=true;canvas.focus();pointers.set(e.pointerId,{x:e.clientX,y:e.clientY});canvas.setPointerCapture(e.pointerId);const b=c.bounds();initialX=e.clientX;initialStart=b.viewStart;initialEnd=b.viewEnd;distance=0;moved=false;if(e.pointerType!=='touch'&&!e.shiftKey)c.seek(timeAt(e.clientX))});
 canvas.addEventListener('pointermove',e=>{if(!pointers.has(e.pointerId)){if(pointers.size===0&&e.pointerType!=='touch'&&tooltip){const rect=canvas.getBoundingClientRect();if(e.clientX>=rect.left+85&&e.clientX<=rect.right-15){tooltip.hidden=false;tooltip.textContent=clock(timeAt(e.clientX));tooltip.style.left=Math.round(e.clientX-rect.left)+'px'}else tooltip.hidden=true}return}pointers.set(e.pointerId,{x:e.clientX,y:e.clientY});moved=moved||Math.abs(e.clientX-initialX)>5;
  if(pointers.size===2){moved=true;const [a,b]=[...pointers.values()];const next=Math.hypot(a.x-b.x,a.y-b.y);if(distance>0&&next>0)c.zoom(distance/next,timeAt((a.x+b.x)/2));distance=next;initialStart=c.bounds().viewStart;initialEnd=c.bounds().viewEnd;initialX=e.clientX;return}
  if(e.pointerType==='touch'||e.shiftKey){const b=c.bounds(),span=initialEnd-initialStart;const delta=(e.clientX-initialX)/Math.max(1,canvas.clientWidth-100)*span;const start=clamp(initialStart-delta,b.from,b.to-span);c.pan(start,start+span)}else c.seek(timeAt(e.clientX));
 });
 const finish=(e:PointerEvent)=>{if(tooltip)tooltip.hidden=true;if(e.type==='pointerup'&&e.pointerType==='touch'&&!moved&&pointers.size===1)c.seek(timeAt(e.clientX));pointers.delete(e.pointerId);distance=0;if(pointers.size===1){initialX=[...pointers.values()][0].x;initialStart=c.bounds().viewStart;initialEnd=c.bounds().viewEnd;moved=true}};
 for(const event of ['pointerup','pointercancel','lostpointercapture'])canvas.addEventListener(event,e=>finish(e as PointerEvent));
 canvas.addEventListener('pointerleave',()=>{if(tooltip)tooltip.hidden=true});
 canvas.addEventListener('wheel',e=>{e.preventDefault();if(tooltip)tooltip.hidden=true;c.zoom(e.deltaY>0?1.5:1/1.5,timeAt(e.clientX))},{passive:false});
 canvas.addEventListener('keydown',e=>{if(!c.shortcuts()||e.ctrlKey||e.metaKey||e.altKey)return;if(e.key==='ArrowRight'||e.key==='ArrowLeft'){e.preventDefault();c.seek(c.bounds().cursor+(e.key==='ArrowRight'?1:-1)*(e.shiftKey?60:c.seekStep()))}if(e.key==='+'||e.key==='='){e.preventDefault();c.zoom(.5)}if(e.key==='-'){e.preventDefault();c.zoom(2)}});
 return ()=>{};
}
