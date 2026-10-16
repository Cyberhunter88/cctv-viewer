// Serialize writes and coalesce changes while a request is in progress. Snapshots
// represent this device's edits, never a replacement for another device's state.
export class PreferenceWriter {
 private baseline:Record<string,unknown>;
 private pending:Record<string,unknown>={};
 private running?:Promise<void>;
 private write:(changes:Record<string,unknown>)=>Promise<unknown>;
 private status:(saving:boolean,error?:unknown)=>void;
 constructor(initial:Record<string,unknown>,write:(changes:Record<string,unknown>)=>Promise<unknown>,status:(saving:boolean,error?:unknown)=>void){this.baseline=structuredClone(initial);this.write=write;this.status=status}
 save(snapshot:Record<string,unknown>):Promise<void>{
  for(const [key,value] of Object.entries(snapshot))if(JSON.stringify(value)!==JSON.stringify(this.baseline[key]))this.pending[key]=structuredClone(value);
  this.baseline=structuredClone(snapshot);
  return this.flush();
 }
 flush():Promise<void>{
  if(this.running)return this.running;
  if(!Object.keys(this.pending).length)return Promise.resolve();
  this.status(true);
  this.running=this.drain().finally(()=>{this.running=undefined});
  return this.running;
 }
 private async drain(){
  while(Object.keys(this.pending).length){
   const changes=this.pending;this.pending={};
   try{await this.write(changes)}catch(error){this.pending={...changes,...this.pending};this.status(false,error);throw error}
  }
  this.status(false);
 }
}
