import {berlinDate,clamp} from './timeline.ts';
export interface ViewState {cameras:string[];layout:string;quality:string;speed:string}
export function validView(value:unknown,ids:string[]):Partial<ViewState>{
 if(!value||typeof value!=='object')return {};
 const v=value as Partial<ViewState>,out:Partial<ViewState>={};
 if(Array.isArray(v.cameras)&&v.cameras.every(id=>typeof id==='string'))out.cameras=v.cameras.filter(id=>ids.includes(id));
 if(['1','4','9'].includes(v.layout||''))out.layout=v.layout;
 if(['auto','native','compatible'].includes(v.quality||''))out.quality=v.quality;
 if(['0.5','1','2','4'].includes(v.speed||''))out.speed=v.speed;
 return out;
}
export function readTimeLink(search:string,ids:string[]):{date?:string;time?:number;cameras?:string[]}{
 const p=new URLSearchParams(search),out:{date?:string;time?:number;cameras?:string[]}={};const date=p.get('date');
 if(date&&/^\d{4}-\d{2}-\d{2}$/.test(date)&&date>='1970-01-01'&&date<='2200-12-31'&&Number.isFinite(Date.parse(date+'T12:00:00Z'))&&new Date(date+'T12:00:00Z').toISOString().slice(0,10)===date)out.date=date;
 const raw=p.get('time'),time=raw!==null&&raw!==''?Number(raw):NaN;
 if(Number.isFinite(time)&&time>=0&&time<7289650800&&(!out.date||berlinDate(new Date(time*1000))===out.date)){out.time=time;out.date=berlinDate(new Date(time*1000))}
 if(p.has('cameras')){const cameras=p.get('cameras')!.split(',').filter(id=>ids.includes(id));if(cameras.length||p.get('cameras')==='')out.cameras=[...new Set(cameras)]}
 return out;
}
export function selectionEdge(edge:'start'|'end',value:number,start:number,end:number,from:number,to:number):[number,number]{
 return edge==='start'?[clamp(value,Math.max(from,end-1800),end-1),end]:[start,clamp(value,start+1,Math.min(to,start+1800))];
}
export function shouldFallback(quality:string,error:number,tried:boolean,streaming:boolean):boolean{return quality==='auto'&&!tried&&!streaming&&(error===3||error===4)}
