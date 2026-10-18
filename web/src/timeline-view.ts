import {clamp,clock,type Recording} from './timeline';

interface CameraTrack {id:string;name:string;viewMode?:string}
interface TimelineFrame {
 from:number;to:number;cursor:number;viewStart:number;viewEnd:number;
 cameras:CameraTrack[];records:Recording[];height:number;
}

export function createTimelineView(canvas:HTMLCanvasElement,overview:HTMLCanvasElement){
 const timelineLayer=document.createElement('canvas'),overviewLayer=document.createElement('canvas');
 let timelineKey='',overviewKey='',timelineRecords:Recording[]|undefined,overviewRecords:Recording[]|undefined;
 function colors(){
  const css=getComputedStyle(document.documentElement);
  return Object.fromEntries(['line','muted','track','record','cursor','panel'].map(key=>[key,css.getPropertyValue('--timeline-'+key).trim()]));
 }
 function description(frame:TimelineFrame){
  const root=document.getElementById('timeline-description')!,list=document.createElement('ul');
  root.replaceChildren();
  const heading=document.createElement('p');heading.textContent=`Aufnahmebereiche zwischen ${clock(frame.viewStart)} und ${clock(frame.viewEnd)}:`;root.append(heading);
  for(const camera of frame.cameras){
   const ranges=frame.records.filter(record=>record.cameraId===camera.id&&record.start<frame.viewEnd&&record.start+record.duration>frame.viewStart);
   if(!ranges.length){const item=document.createElement('li');item.textContent=`${camera.name}: keine Aufnahme in diesem Zeitfenster`;list.append(item);continue}
   for(const record of ranges){
    const item=document.createElement('li'),start=Math.max(frame.viewStart,record.start),end=Math.min(frame.viewEnd,record.start+record.duration);
    const view=record.view==='wide'?' · Weitwinkel':record.view==='zoom'?' · Zoom':'';
    item.textContent=`${camera.name}${view}: ${clock(start)} bis ${clock(end)}`;list.append(item);
   }
  }
  if(!frame.cameras.length){const item=document.createElement('li');item.textContent='Keine Kameras sichtbar.';list.append(item)}
  root.append(list);
 }
 function drawOverview(frame:TimelineFrame,palette:Record<string,string>){
  const {from,to,cursor,viewStart,viewEnd,cameras,records}=frame;
  const width=Math.max(100,overview.parentElement!.clientWidth),dpr=devicePixelRatio||1;
  if(overview.width!==width*dpr){overview.width=width*dpr;overview.height=34*dpr}
  const ctx=overview.getContext('2d')!;ctx.setTransform(dpr,0,0,dpr,0,0);
  const ids=cameras.map(camera=>camera.id),key=[from,to,width,dpr,...ids].join('/');
  if(overviewKey!==key||overviewRecords!==records){
   ctx.fillStyle=palette.track;ctx.fillRect(0,0,width,34);ctx.fillStyle=palette.record;
   for(const record of records)if(ids.includes(record.cameraId)){
    const start=clamp(record.start,from,to);
    ctx.fillRect((start-from)/(to-from)*width,10,Math.max(1,(Math.min(to,record.start+record.duration)-start)/(to-from)*width),14);
   }
   overviewLayer.width=overview.width;overviewLayer.height=overview.height;
   overviewLayer.getContext('2d')!.drawImage(overview,0,0);overviewKey=key;overviewRecords=records;
  }else ctx.drawImage(overviewLayer,0,0,width,34);
  ctx.strokeStyle=palette.cursor;ctx.strokeRect((viewStart-from)/(to-from)*width,1,(viewEnd-viewStart)/(to-from)*width,32);
  ctx.fillStyle=palette.cursor;ctx.fillRect((cursor-from)/(to-from)*width,0,1,34);
 }
 function draw(frame:TimelineFrame){
  const {from,to,cursor,viewStart,viewEnd,cameras,records,height:preferredHeight}=frame;
  if(!from)return;
  const palette=colors(),width=canvas.parentElement!.clientWidth,height=48+Math.max(1,cameras.length)*34,dpr=devicePixelRatio||1;
  if(width<=0)return;
  document.getElementById('timeline-viewport')!.style.height=(preferredHeight===240?Math.min(240,Math.max(100,height+12)):preferredHeight)+'px';
  if(canvas.width!==Math.round(width*dpr)||canvas.height!==Math.round(height*dpr)){canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);canvas.style.height=height+'px'}
  const ctx=canvas.getContext('2d')!;ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,width,height);
  const left=85,right=width-15,area=right-left,span=viewEnd-viewStart,x=(time:number)=>left+(time-viewStart)/span*area;
  const intervals=[60,300,600,1800,3600,7200,14400,21600],interval=intervals.find(seconds=>span/seconds<area/65)||21600;
  const key=[width,height,dpr,viewStart,viewEnd,...cameras.map(camera=>camera.id+':'+camera.name+':'+camera.viewMode)].join('/');
  if(timelineKey!==key||timelineRecords!==records){
   ctx.font='11px system-ui';ctx.textAlign='center';
   for(let time=Math.ceil(viewStart/interval)*interval;time<=viewEnd;time+=interval){
    const position=x(time);ctx.strokeStyle=palette.line;ctx.beginPath();ctx.moveTo(position,30);ctx.lineTo(position,height-10);ctx.stroke();ctx.fillStyle=palette.muted;ctx.fillText(clock(time).slice(0,5),position,19);
   }
   cameras.forEach((camera,index)=>{
    const y=39+index*34;ctx.fillStyle=palette.muted;ctx.textAlign='left';
    ctx.fillText(camera.name.length>11?camera.name.slice(0,10)+'…':camera.name,12,y+14);
    ctx.fillStyle=palette.track;ctx.fillRect(left,y,area,23);ctx.save();ctx.beginPath();ctx.rect(left,y,area,23);ctx.clip();
    for(const record of records.filter(item=>item.cameraId===camera.id&&item.start<viewEnd&&(item.kind==='image'?item.start>=viewStart:item.start+item.duration>viewStart))){
     ctx.fillStyle=palette.record;ctx.fillRect(x(record.start),y+3,record.kind==='image'?4:Math.max(2,record.duration/span*area),17);
    }
    ctx.restore();
   });
   timelineLayer.width=canvas.width;timelineLayer.height=canvas.height;timelineLayer.getContext('2d')!.drawImage(canvas,0,0);
   timelineKey=key;timelineRecords=records;description(frame);
  }else ctx.drawImage(timelineLayer,0,0,width,height);
  if(cursor>=viewStart&&cursor<=viewEnd){
   const position=x(cursor);ctx.strokeStyle=palette.cursor;ctx.lineWidth=1.5;ctx.beginPath();ctx.moveTo(position,29);ctx.lineTo(position,height-8);ctx.stroke();
   ctx.fillStyle=palette.cursor;ctx.beginPath();ctx.moveTo(position-4,27);ctx.lineTo(position+4,27);ctx.lineTo(position,33);ctx.fill();
  }
  drawOverview(frame,palette);
  const hours=span/3600;document.getElementById('zoom-label')!.textContent=hours>=1?`${Math.round(hours*10)/10} Std.`:`${Math.round(span/60)} Min.`;
 }
 return {draw,invalidate:()=>{timelineKey='';overviewKey=''}};
}
