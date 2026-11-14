import {berlinDate,clock} from './timeline.ts';

export type SearchRange='today'|'24h'|'7d'|'all';

/** Values for the search's datetime-local fields (Europe/Berlin) of a quick time range. */
export function searchRange(range:SearchRange,now:number):{from:string;to:string}{
 const current=Math.floor(now),local=(time:number)=>berlinDate(new Date(time*1000))+'T'+clock(time);
 if(range==='all')return {from:'',to:''};
 if(range==='today')return {from:berlinDate(new Date(current*1000))+'T00:00:00',to:''};
 return {from:local(current-(range==='24h'?86400:7*86400)),to:''};
}
