import {liveBufferTarget} from './live-state';
const codecs=['avc1.640029','avc1.64002A','avc1.640033','avc1.42E01F','hvc1.1.6.L153.B0','mp4a.40.2','opus'];
export class LivePlayer {
 private socket?:WebSocket;
 private peer?:RTCPeerConnection;
 private source?:MediaSource;
 private buffer?:SourceBuffer;
 private queue:ArrayBuffer[]=[];
 private objectURL?:string;
 private timer?:ReturnType<typeof setTimeout>;
 private fallback?:ReturnType<typeof setTimeout>;
 private disposed=false;
 private attempt=0;
 private generation=0;
 private mode:'webrtc'|'mse'='webrtc';
 private candidates:RTCIceCandidateInit[]=[];
 private watchdog:ReturnType<typeof setInterval>;
 private progress=Date.now();
 private position=-1;
 constructor(readonly video:HTMLVideoElement,private camera:string,private view:string,private quality:string,private status:(message:string)=>void,private unsupported:()=>void){
  video.autoplay=true;video.muted=true;video.playsInline=true;
  this.watchdog=setInterval(()=>{if(this.disposed||document.hidden)return;if(this.video.currentTime!==this.position){this.position=this.video.currentTime;this.progress=Date.now()}else if(Date.now()-this.progress>12000){this.progress=Date.now();if(this.mode==='webrtc')this.useMSE();else this.retry()}},2000);
  this.connect();
 }
 private connect(){
  if(this.disposed)return;
  this.clear();this.progress=Date.now();this.position=-1;const generation=this.generation;
  this.status('Verbindung wird aufgebaut …');
  const url=new URL('/api/live/'+encodeURIComponent(this.camera)+'/ws',location.href);url.protocol=location.protocol==='https:'?'wss:':'ws:';url.search=new URLSearchParams({view:this.view,quality:this.quality,mode:this.mode}).toString();
  const socket=new WebSocket(url);socket.binaryType='arraybuffer';this.socket=socket;
  socket.onopen=()=>{
   if(generation!==this.generation)return;
   if(this.mode==='webrtc')void this.startRTC(socket,generation).catch(()=>this.useMSE());
   else this.startMSE(socket,generation);
  };
  socket.onmessage=event=>{
   if(generation!==this.generation)return;
   if(event.data instanceof ArrayBuffer){this.queue.push(event.data);if(this.queue.length>120){this.retry();return}this.append();return}
   void this.message(String(event.data),generation).catch(()=>this.retry());
  };
  socket.onerror=()=>{};
  socket.onclose=()=>{if(generation!==this.generation||this.disposed)return;if(this.mode==='webrtc')this.useMSE();else this.retry()};
  this.fallback=setTimeout(()=>{if(generation!==this.generation)return;if(this.mode==='webrtc')this.useMSE();else this.retry()},this.mode==='webrtc'?2000:12000);
  this.video.onplaying=()=>{if(generation!==this.generation)return;clearTimeout(this.fallback);this.attempt=0;this.status(this.mode==='webrtc'?'Live · WebRTC':'Live · HTTPS')};
  this.video.onerror=()=>{if(generation===this.generation)this.incompatible()};
 }
 private async startRTC(socket:WebSocket,generation:number){
  const peer=new RTCPeerConnection({iceServers:[],bundlePolicy:'max-bundle'});this.peer=peer;
  peer.addTransceiver('video',{direction:'recvonly'});peer.addTransceiver('audio',{direction:'recvonly'});
  peer.ontrack=event=>{if(generation!==this.generation)return;this.video.srcObject=event.streams[0]||new MediaStream(peer.getReceivers().map(r=>r.track));void this.video.play().catch(()=>this.status('Zum Starten auf das Bild tippen'))};
  peer.onicecandidate=event=>{if(generation===this.generation&&event.candidate&&socket.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'webrtc/candidate',value:event.candidate.candidate}))};
  peer.onconnectionstatechange=()=>{if(generation===this.generation&&['failed','disconnected'].includes(peer.connectionState))this.useMSE()};
  await peer.setLocalDescription(await peer.createOffer());
  if(generation===this.generation&&socket.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'webrtc/offer',value:peer.localDescription!.sdp}));
 }
 private startMSE(socket:WebSocket,generation:number){
  const Managed=(window as unknown as {ManagedMediaSource?:typeof MediaSource}).ManagedMediaSource;
  const Source=Managed||window.MediaSource;
  if(!Source){this.status('Dieser Browser unterstützt den HTTPS-Livestream nicht. Bitte einen aktuellen Browser verwenden.');this.clear();return}
  this.source=new Source();
  if(Managed){this.video.disableRemotePlayback=true;this.video.srcObject=this.source}else{this.objectURL=URL.createObjectURL(this.source);this.video.src=this.objectURL}
  this.source.addEventListener('sourceopen',()=>{if(generation!==this.generation||socket.readyState!==WebSocket.OPEN)return;const supported=codecs.filter(codec=>Source.isTypeSupported('video/mp4; codecs="'+codec+'"'));socket.send(JSON.stringify({type:'mse',value:supported.join(',')}))},{once:true});
  void this.video.play().catch(()=>{if(generation===this.generation)this.status('Zum Starten auf das Bild tippen')});
 }
 private async message(raw:string,generation:number){
  const message=JSON.parse(raw) as {type:string;value:string};
  if(message.type==='error'){if(this.mode==='webrtc')this.useMSE();else this.incompatible();return}
  if(message.type==='webrtc/answer'&&this.peer){await this.peer.setRemoteDescription({type:'answer',sdp:message.value});if(generation!==this.generation)return;for(const candidate of this.candidates)await this.peer.addIceCandidate(candidate);this.candidates=[]}
  if(message.type==='webrtc/candidate'&&this.peer&&message.value){const candidate={candidate:message.value,sdpMid:'0'};if(this.peer.remoteDescription)await this.peer.addIceCandidate(candidate);else this.candidates.push(candidate)}
  if(message.type==='mse'&&this.source?.readyState==='open'){
   if(!/\b(?:avc1|hvc1|hev1)\./.test(message.value)){this.incompatible();return}
   this.buffer=this.source.addSourceBuffer(message.value);this.buffer.mode='segments';
   this.buffer.addEventListener('updateend',()=>{if(generation!==this.generation)return;this.append();this.catchUp();});
   this.buffer.addEventListener('error',()=>{if(generation===this.generation)this.retry()});this.append();
   void this.video.play().catch(()=>this.status('Zum Starten auf das Bild tippen'));
  }
 }
 private append(){
  const buffer=this.buffer;if(!buffer||buffer.updating||!this.queue.length)return;
  try{
   const ranges=buffer.buffered;if(ranges.length&&ranges.end(ranges.length-1)-ranges.start(0)>8&&this.video.currentTime>3){buffer.remove(ranges.start(0),this.video.currentTime-2);return}
   buffer.appendBuffer(this.queue.shift()!);
  }catch{this.retry()}
 }
 private catchUp(){const ranges=this.video.buffered;if(!ranges.length)return;const end=ranges.end(ranges.length-1);if(end-this.video.currentTime>1.25||this.video.currentTime<ranges.start(0))this.video.currentTime=Math.max(ranges.start(0),liveBufferTarget(end))}
 private incompatible(){if(this.quality==='high'){this.dispose();this.unsupported()}else this.retry()}
 private useMSE(){if(this.disposed)return;this.mode='mse';this.connect()}
 private retry(){if(this.disposed)return;this.clear();this.status('Verbindung unterbrochen · neuer Versuch …');this.timer=setTimeout(()=>this.connect(),Math.min(15000,1000*2**Math.min(this.attempt++,4)))}
 private clear(){
  this.generation++;clearTimeout(this.timer);clearTimeout(this.fallback);this.video.onplaying=null;this.video.onerror=null;
  if(this.socket){this.socket.onclose=null;this.socket.close();this.socket=undefined}
  this.peer?.close();this.peer=undefined;this.video.pause();this.video.srcObject=null;this.video.removeAttribute('src');this.video.load();
  if(this.objectURL)URL.revokeObjectURL(this.objectURL);this.objectURL=undefined;this.source=undefined;this.buffer=undefined;this.queue=[];this.candidates=[];
 }
 dispose(){this.disposed=true;clearInterval(this.watchdog);this.clear()}
}
