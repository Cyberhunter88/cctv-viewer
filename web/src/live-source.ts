export const liveKeys=['fast','high','zoomFast','zoomHigh'] as const;
export type LiveKey=typeof liveKeys[number];
export function importLiveURL(raw:string){
 const url=new URL(raw);
 if(url.protocol!=='https:')return {url:raw};
 const username=url.searchParams.get('user'),password=url.searchParams.get('password');
 url.searchParams.delete('user');url.searchParams.delete('password');
 return {url:url.toString(),username,password};
}
