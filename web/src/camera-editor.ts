export interface CameraEditorCamera {
 id:string;name:string;folder:string;pattern:string;timezone:string;offset:number;
 format?:string;indexingMode?:string;directoryPattern?:string;viewMode?:string;
 wideMarker?:string;zoomMarker?:string;zoomOffset?:number;supportsZoom?:boolean;
}

export function initCameraEditor(form:HTMLFormElement,dialog:HTMLElement,onSaved:()=>void,saving:()=>boolean){
 const $=<T extends HTMLElement=HTMLElement>(id:string)=>document.getElementById(id) as T;
 let baseline='';
 function cameraFormatChanged(){
  $('dual-fields').hidden=$<HTMLSelectElement>('view-mode').value!=='dual';
  const automatic=$<HTMLSelectElement>('camera-format').value==='automatic';
  $('pattern-label').hidden=automatic;
  (form.elements.namedItem('pattern') as HTMLInputElement).required=!automatic;
  $('directory-pattern-label').hidden=$<HTMLSelectElement>('indexing-mode').value==='full';
 }
 function cameraInput():CameraEditorCamera{
  const value=(name:string)=>(form.elements.namedItem(name) as HTMLInputElement|HTMLSelectElement)?.value;
  return {id:String(value('id')||''),name:String(value('name')),folder:String(value('folder')),pattern:String(value('pattern')),timezone:String(value('timezone')),offset:Number(value('offset')),format:String(value('format')),indexingMode:String(value('indexingMode')),directoryPattern:String(value('directoryPattern')),viewMode:String(value('viewMode')),wideMarker:String(value('wideMarker')),zoomMarker:String(value('zoomMarker')),zoomOffset:Number(value('zoomOffset')),supportsZoom:(form.elements.namedItem('supportsZoom') as HTMLInputElement)?.checked ?? true};
 }
 const snapshot=()=>JSON.stringify(cameraInput());
 const cameraDirty=()=>snapshot()!==baseline;
 const discardCamera=()=>!cameraDirty()||confirm('Ungespeicherte Kameraänderungen verwerfen?');
 function markCameraSaved(){baseline=snapshot();form.dataset.dirty='false';onSaved()}
 function resetCamera(){
  form.reset();(form.elements.namedItem('id') as HTMLInputElement).value='';
  $('preview-result').hidden=true;$('camera-error').textContent='';$('folder-browser').hidden=true;
  $('camera-editor-title').textContent='Neue Kamera';$<HTMLDetailsElement>('camera-advanced').open=false;
  cameraFormatChanged();markCameraSaved();
 }
 function showCameraEditor(){dialog.classList.add('camera-editing');dialog.scrollTop=0;$('camera-editor-title').focus({preventScroll:true})}
 function editCamera(camera:CameraEditorCamera){
  form.reset();
  for(const [key,value] of Object.entries(camera)){
   const input=form.elements.namedItem(key);
   if(input instanceof HTMLInputElement){if(input.type==='checkbox')input.checked=value==='true'||value===true;else input.value=String(value)}
   else if(input instanceof HTMLSelectElement)input.value=String(value);
  }
  $('preview-result').hidden=true;$('camera-error').textContent='';$('folder-browser').hidden=true;
  $('camera-editor-title').textContent=camera.name+' bearbeiten';
  $<HTMLDetailsElement>('camera-advanced').open=!!(camera.offset||camera.format&&camera.format!=='automatic'||camera.timezone!=='Europe/Berlin');
  cameraFormatChanged();markCameraSaved();
 }
 function leaveCameraEditor(){if(saving()||!discardCamera())return;resetCamera();dialog.classList.remove('camera-editing');$('new-camera').focus()}
 form.addEventListener('input',()=>form.dataset.dirty=String(cameraDirty()));
 form.addEventListener('change',()=>form.dataset.dirty=String(cameraDirty()));
 $<HTMLSelectElement>('camera-format').addEventListener('change',cameraFormatChanged);
 $<HTMLSelectElement>('indexing-mode').onchange=cameraFormatChanged;
 $<HTMLSelectElement>('view-mode').onchange=cameraFormatChanged;
 return {cameraFormatChanged,cameraInput,cameraDirty,discardCamera,markCameraSaved,resetCamera,showCameraEditor,editCamera,leaveCameraEditor};
}
