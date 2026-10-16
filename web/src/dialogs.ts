/** Keep scrollbars inside the rounded dialog surface, including dynamic dialogs. */
export function prepareDialogs(){
 for(const dialog of document.querySelectorAll<HTMLDialogElement>('dialog')){
  if(dialog.querySelector(':scope > .dialog-body'))continue;
  const body=document.createElement('div');body.className='dialog-body';
  body.append(...Array.from(dialog.childNodes));dialog.append(body);
  dialog.addEventListener('close',()=>{body.scrollTop=0});
 }
}
