import {berlinDate,clock} from './timeline.ts';
// Local Berlin date-time strings for search and audit fields, independent of the browser timezone.
// Kept apart from the administration pages so the search page does not load them.
export function berlinLocal(epoch:number):string{return berlinDate(new Date(epoch*1000))+'T'+clock(epoch)}
export function berlinEpoch(value:string):number{
 const base=Date.parse(value+'Z')/1000;if(!Number.isFinite(base))throw new Error('Datum und Uhrzeit fehlen');
 const matches=[base,base-3600,base-7200].filter(t=>berlinLocal(t)===value);
 if(matches.length!==1)throw new Error('Uhrzeit ist wegen Zeitumstellung mehrdeutig oder existiert nicht. Einen anderen Zeitpunkt wählen.');return matches[0];
}
