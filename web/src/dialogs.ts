/** Keep the heading and actions outside each dialog's scrolling surface. */
export function prepareDialogs(){
 for(const dialog of document.querySelectorAll<HTMLDialogElement>('dialog')){
  if(dialog.querySelector(':scope > .dialog-body'))continue;
  const heading=dialog.querySelector(':scope > .dialog-heading');
  const footer=dialog.querySelector<HTMLElement>('.preferences-footer');
  if(footer){
   for(const button of footer.querySelectorAll<HTMLButtonElement>('button')){
    const form=button.form;if(form)button.setAttribute('form',form.id);
   }
   footer.remove();
  }
  const body=document.createElement('div');body.className='dialog-body';
  body.append(...Array.from(dialog.childNodes).filter(node=>node!==heading));
  dialog.append(body);if(footer)dialog.append(footer);
  dialog.addEventListener('close',()=>{body.scrollTop=0});
 }
}
