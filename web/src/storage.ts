export interface StorageDay {date:string;videos:number;bytes:number}
export interface StorageCamera {id:string;name:string;videos:number;bytes:number;oldest:number;newest:number;days:StorageDay[]}
export interface StorageOverview {cameras:StorageCamera[];videos:number;bytes:number;days:number}

const units=['B','KiB','MiB','GiB','TiB'];
const number=new Intl.NumberFormat('de-DE',{maximumFractionDigits:1});

/** Binary units with at most one decimal place, e.g. "1,5 GiB". */
export function formatBytes(bytes:number):string{
 if(!Number.isFinite(bytes)||bytes<=0)return '0 B';
 let unit=0,value=bytes;
 while(value>=1024&&unit<units.length-1){value/=1024;unit++}
 return number.format(value)+' '+units[unit];
}

/** Mean volume of the days that actually contain recordings; idle days do not dilute it. */
export function dailyAverage(days:StorageDay[]):number{
 const active=days.filter(day=>day.videos>0);
 return active.length?active.reduce((sum,day)=>sum+day.bytes,0)/active.length:0;
}
