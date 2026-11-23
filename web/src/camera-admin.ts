// Camera administration page: wires the list, the editor, the save/preview requests and the
// page registration. app.ts only passes in the shared state it owns.
import {initCameraEditor} from './camera-editor';
import {initCameraList} from './camera-list';
import {initCameraSettings} from './camera-settings';
import {notifyError} from './toast';
import {registerPage, navigate} from './navigation';
import type {initLiveAdmin} from './live-admin';
import type {Camera, Scan, ViewDetection} from './types';

export interface CameraAdminContext {
  api: <T>(url: string, options?: RequestInit) => Promise<T>;
  notify: (message: string) => void;
  cameras: () => Camera[];
  order: () => string[];
  setOrder: (order: string[]) => void;
  date: () => string;
  setScan: (scan: Scan) => void;
  liveAdmin: ReturnType<typeof initLiveAdmin>;
  renderCameras: () => void;
  saveView: () => Promise<void>;
  refreshCameras: () => Promise<void>;
  loadDay: () => Promise<void>;
}

interface PreviewResult {
  examples: {file: string; time?: string; error?: string; view?: string}[];
  limited: boolean;
  detection?: ViewDetection;
  wideMarker: string;
  zoomMarker: string;
}

const viewNames: Record<string, string> = {wide: 'Weitwinkel', zoom: 'Zoom', unknown: 'Kennung unklar'};

// Text shown below "Erkennung prüfen": the detection summary followed by one entry per example file.
function describePreview(result: PreviewResult): string {
  const summary = result.detection
    ? `${result.detection.message}\nKennungen: Weitwinkel ${result.wideMarker}, Zoom ${result.zoomMarker}\n\n`
    : '';
  const examples = result.examples.length
    ? result.examples
        .map(example => {
          const view = example.view ? ` · ${viewNames[example.view] || example.view}` : '';
          return `${example.file}${view}\n→ ${example.error || example.time}`;
        })
        .join('\n\n')
    : 'Keine passenden Videodateien gefunden.';
  return summary + examples + (result.limited ? '\nSuche auf 10.000 Einträge begrenzt.' : '');
}

export function initCameraAdmin(ctx: CameraAdminContext) {
  const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;
  const on = (id: string, action: () => void | Promise<void>) =>
    $(id).addEventListener('click', () => {
      Promise.resolve(action()).catch(error => notifyError(error.message));
    });
  const dialog = $('camera-dialog');
  const form = $<HTMLFormElement>('camera-form');
  let saving = false;

  const list = initCameraList({
    api: ctx.api, notify: ctx.notify, cameras: ctx.cameras, order: ctx.order, setOrder: ctx.setOrder,
    saving: () => saving, date: ctx.date, dialog, form, setScan: ctx.setScan,
    confirmDiscard: () => editor.confirmDiscard(),
    canChange: () => settings.canChange(),
    clearSettings: () => settings.clear(),
    editCamera: camera => editor.editCamera(camera),
    showCameraEditor: () => editor.showCameraEditor(),
    resetCamera: () => editor.resetCamera(),
    openLive: id => ctx.liveAdmin.open(id),
    renderCameras: ctx.renderCameras, saveView: ctx.saveView,
    refreshCameras: ctx.refreshCameras, loadDay: ctx.loadDay,
  });
  const editor = initCameraEditor(form, dialog, list.renderCameraList, () => saving);
  const settings = initCameraSettings(
    form, dialog, ctx.liveAdmin, () => ctx.cameras()[0]?.id || '',
    id => {
      const camera = ctx.cameras().find(candidate => candidate.id === id);
      if (!camera || saving || !editor.discardCamera()) return false;
      editor.editCamera(camera);
      editor.showCameraEditor();
      return true;
    },
    () => !saving && editor.discardCamera(),
    editor.resetCamera,
  );
  form.addEventListener('cameraeditorclosed', () => settings.clear());
  list.mountSearch();
  editor.resetCamera();

  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (saving) return;
    saving = true;
    const submit = form.querySelector<HTMLButtonElement>('button[type=submit]')!;
    submit.disabled = true;
    list.renderCameraList();
    // Everything that could change the camera while it is being saved stays disabled.
    const controls = [
      ...form.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLButtonElement>('input,select,button'),
      ...['close-dialog', 'new-camera', 'batch-open'].map(id => $<HTMLButtonElement>(id)),
    ];
    const wasDisabled = controls.map(control => control.disabled);
    const payload = editor.cameraInput();
    controls.forEach(control => (control.disabled = true));
    try {
      $('camera-error').textContent = '';
      const saved = await ctx.api<Camera & {detection?: ViewDetection}>('/api/cameras', {method: 'POST', body: JSON.stringify(payload)});
      // The POST committed successfully even if refreshing recordings subsequently fails.
      (form.elements.namedItem('id') as HTMLInputElement).value = saved.id;
      editor.markCameraSaved();
      await ctx.refreshCameras();
      editor.editCamera(ctx.cameras().find(camera => camera.id === saved.id) || saved);
      await ctx.loadDay();
      ctx.notify(saved.detection ? `Kamera gespeichert. ${saved.detection.message}` : 'Kamera gespeichert. Aufnahmen werden eingelesen.');
    } catch (error) {
      $('camera-error').textContent = (error as Error).message;
    } finally {
      controls.forEach((control, index) => (control.disabled = wasDisabled[index]));
      saving = false;
      submit.disabled = false;
      list.renderCameraList();
    }
  });

  on('preview', async () => {
    try {
      const result = await ctx.api<PreviewResult>('/api/cameras/preview', {method: 'POST', body: JSON.stringify(editor.cameraInput())});
      $('preview-result').hidden = false;
      $('preview-result').textContent = describePreview(result);
    } catch (error) {
      $('camera-error').textContent = (error as Error).message;
    }
  });

  on('new-camera', async () => {
    if (saving || !await editor.confirmDiscard() || !settings.canChange()) return;
    editor.resetCamera();
    settings.clear();
    editor.showCameraEditor();
  });
  for (const id of ['camera-back', 'camera-cancel']) on(id, () => { if (settings.canChange()) return editor.leaveCameraEditor(); });
  on('close-dialog', () => navigate('recordings'));
  on('add-first', () => {
    navigate('admin/cameras');
    editor.resetCamera();
    editor.showCameraEditor();
  });

  registerPage('admin/cameras', {
    element: dialog, title: 'Kameras', admin: true,
    enter: () => {
      void list.loadScanHistory();
      void list.loadCameraLiveStatus();
      void list.loadStorage();
      settings.showSection('list');
      settings.show('general');
    },
    leave: next => {
      if (saving) return false;
      if (next === 'admin/cameras/live' || next === 'admin/cameras/batch') return true;
      if (!editor.discardCamera() || !settings.leave()) return false;
      editor.resetCamera();
      dialog.classList.remove('camera-editing');
      return true;
    },
  });

  return {
    dialog,
    cameraInput: editor.cameraInput,
    discardCamera: editor.discardCamera,
    resetCamera: editor.resetCamera,
    renderCameraList: list.renderCameraList,
    loadScanHistory: list.loadScanHistory,
    loadCameraLiveStatus: list.loadCameraLiveStatus,
    loadStorage: list.loadStorage,
  };
}
