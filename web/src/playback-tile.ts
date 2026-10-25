import type Hls from 'hls.js';
import { recordingAt, clock, type Recording } from './timeline';
import type { ViewState } from './view-state';
interface PlaybackCamera {
  id: string;
  name: string;
}
let hlsModule:Promise<typeof import('hls.js')>|undefined;
function loadHls(){return hlsModule??=import('hls.js')}
export interface TileContext {
  state: ViewState;
  cursor: () => number;
  playing: () => boolean;
  speed: () => number;
  quality: () => string;
  recordings: () => Recording[];
  playableRecordings: () => Recording[];
  audioTile: () => Tile | undefined;
  setAudioTile: (tile: Tile | undefined) => void;
  selectHotspot: (id: string) => void;
  toggleFullscreen: (tile: HTMLElement) => Promise<void>;
  notify: (message: string) => void;
  log: (event: string, camera?: string, detail?: string) => void;
  request: <T>(url: string, options?: RequestInit) => Promise<T>;
}
function element<K extends keyof HTMLElementTagNameMap>(tag: K, className = '', text = ''): HTMLElementTagNameMap[K] { const e = document.createElement(tag); e.className = className; e.textContent = text; return e; }
type ControlIcon = 'expand' | 'collapse' | 'sound' | 'muted';
export function setControlIcon(button: Element, name: ControlIcon) {
  const paths: Record<ControlIcon, string> = { expand: 'M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5', collapse: 'M3 8h5V3m13 5h-5V3M8 21v-5H3m13 5v-5h5', sound: 'M11 5 6 9H3v6h3l5 4V5Zm4 3a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14', muted: 'M11 5 6 9H3v6h3l5 4V5Zm5 4 5 6m0-6-5 6' };
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '1.7');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  const path = document.createElementNS(svg.namespaceURI, 'path');
  path.setAttribute('d', paths[name]);
  svg.append(path);
  button.replaceChildren(svg);
}
export class Tile {
  setMessage(message: string, error = false) {
    if (this.messageState === message && this.messageError === error) {
      return;
    }
    this.messageState = message;
    this.messageError = error;
    this.message.replaceChildren();
    if (message) {
      this.message.append(element('span', 'symbol', error ? '⚠' : '▧'), element('span', '', message));
    }
    this.message.hidden = !message;
    this.message.classList.toggle('error', error);
    if (error) {
      const retry = element('button', 'tile-retry', 'Erneut versuchen');
      retry.type = 'button';
      retry.onclick = () => this.retry();
      this.message.append(retry);
    }
  }
  audioButton = element('button', 'tile-audio');
  frameCallback?: number;
  messageState?: string;
  messageError = false;
  playPending = false;
  video = element('video');
  container = element('div', 'camera-tile');
  message = element('div', 'tile-message');
  timestamp = element('span');
  codec = element('small');
  stream?: string;
  record?: Recording;
  loading = false;
  failed = false;
  epoch = 0;
  offset = 0;
  factor = 1;
  abort?: AbortController;
  hls?: Hls;
  loadedSpeed = 1;
  attempts = 0;
  recoveryStart = 0;
  recoveryTimer?: ReturnType<typeof setTimeout>;
  stallTimer?: ReturnType<typeof setTimeout>;
  pauseTimer?: ReturnType<typeof setTimeout>;
  constructor(public camera: PlaybackCamera, public view: string | undefined, private context: TileContext) {
    this.conceal();
    this.video.muted = true;
    this.video.preservesPitch = true;
    this.video.playsInline = true;
    this.video.preload = this.context.state.preload as 'metadata' | 'auto';
    const header = element('div', 'tile-header'), title = element('button', 'tile-title', camera.name + (view === 'wide' ? ' · Weitwinkel' : view === 'zoom' ? ' · Zoom' : ''));
    title.type = 'button';
    title.setAttribute('aria-label', title.textContent + ' · Als Hotspot wählen');
    title.title = title.textContent + ' · Als Hotspot wählen';
    title.onclick = () => { this.context.selectHotspot(camera.id); };
    header.append(title, this.codec);
    const footer = element('div', 'tile-footer');
    footer.append(this.timestamp, element('span', '', 'AUFNAHME'));
    this.container.append(this.video, header, this.message, footer);
    this.container.setAttribute('role', 'group');
    this.container.setAttribute('aria-label', title.textContent!);
    this.container.ondblclick = () => void this.context.toggleFullscreen(this.container);
    const expand = element('button', 'tile-expand');
    setControlIcon(expand, 'expand');
    expand.type = 'button';
    expand.setAttribute('aria-label', document.fullscreenEnabled ? 'Vollbild öffnen' : 'Ansicht vergrößern');
    expand.onclick = () => void this.context.toggleFullscreen(this.container);
    this.audioButton.type = 'button';
    this.setAudio(false);
    this.audioButton.onclick = () => {
      const enabled = this.context.audioTile() !== this;
      this.context.audioTile()?.setAudio(false);
      this.context.setAudioTile(enabled ? this : undefined);
      this.setAudio(enabled);
      if (enabled && this.context.playing()) {
        this.startPlayback();
      }
    };
    this.audioButton.ondblclick = e => e.stopPropagation();
    expand.ondblclick = e => e.stopPropagation();
    const actions = element('div', 'tile-actions');
    actions.append(this.audioButton, expand);
    footer.append(actions);
    this.video.addEventListener('error', () => {
      if (this.record && !this.loading) {
        if (this.context.quality() === 'native' && this.video.error?.code === 4) {
          this.fail('Originalformat im Browser nicht unterstützt. Bitte Ausgewogen oder Schnell wählen');
        }
        else {
          this.recover('Video konnte nicht geladen werden');
        }
      }
    });
    this.video.addEventListener('loadedmetadata', () => { this.applyAspect(); this.align(); });
    this.video.addEventListener('canplay', () => {
      if (!this.failed) {
        clearTimeout(this.stallTimer);
        clearTimeout(this.recoveryTimer);
        this.align();
        this.reveal();
        this.context.log('ready', camera.id);
      }
    });
    this.video.addEventListener('seeked', () => this.reveal());
    this.video.addEventListener('seeking', () => { this.conceal(); this.reveal(); });
    this.video.addEventListener('waiting', () => {
      if (this.context.playing() && !this.loading) {
        this.setMessage('Wird gepuffert …');
        this.context.log('buffering', camera.id);
        clearTimeout(this.stallTimer);
        this.stallTimer = setTimeout(() => this.recover('Pufferung dauert zu lange'), 10000);
      }
    });
    this.video.addEventListener('playing', () => { clearTimeout(this.stallTimer); this.context.log('playing', camera.id); });
    // WebKit/GStreamer may clear muted when changing playback speed.
    this.video.addEventListener('volumechange', () => {
      if (this.context.audioTile() !== this && !this.video.muted) {
        this.video.muted = true;
      }
    });
    this.video.addEventListener('ended', () => {
      if (this.record && this.context.cursor() < this.record.start + this.record.duration - 1) {
        this.release();
        this.update();
      }
    });
  }
  setAudio(enabled: boolean) { this.video.muted = !enabled; setControlIcon(this.audioButton, enabled ? 'sound' : 'muted'); this.audioButton.setAttribute('aria-pressed', String(enabled)); const label = `${enabled ? 'Ton ausschalten' : 'Ton einschalten'} · ${this.camera.name}${this.view === 'wide' ? ' · Weitwinkel' : this.view === 'zoom' ? ' · Zoom' : ''}`; this.audioButton.setAttribute('aria-label', label); this.audioButton.title = label; }
  startPlayback() {
    if (this.playPending) {
      return;
    }
    this.playPending = true;
    const epoch = this.epoch;
    void this.video.play().catch((error: Error) => {
      if (epoch !== this.epoch) {
        return;
      }
      if (error.name === 'NotAllowedError' && this.context.audioTile() === this) {
        this.context.setAudioTile(undefined);
        this.setAudio(false);
        this.context.notify('Der Browser hat die Tonwiedergabe blockiert. Bitte Ton erneut einschalten.');
      }
    }).finally(() => {
      if (epoch === this.epoch) {
        this.playPending = false;
      }
    });
  }
  applyAspect() { this.container.style.aspectRatio = this.context.state.aspect === 'auto' ? `${this.video.videoWidth || 16} / ${this.video.videoHeight || 9}` : this.context.state.aspect === 'fill' ? '16 / 9' : this.context.state.aspect; this.video.style.objectFit = this.context.state.aspect === 'fill' ? 'cover' : 'contain'; }
  fail(message: string) { const record = this.record; this.release(); this.record = record; this.failed = true; this.setMessage(message + ' · Bitte erneut versuchen.', true); this.context.log('failed', this.camera.id, message); }
  recover(message: string) {
    if (this.failed) {
      return;
    }
    if (!this.recoveryStart) {
      this.recoveryStart = performance.now();
    }
    if (this.attempts >= 3 || performance.now() - this.recoveryStart >= 30000) {
      this.fail(message);
      return;
    }
    const record = this.record;
    if (!record) {
      return;
    }
    this.attempts++;
    this.context.log('retry', this.camera.id, String(this.attempts));
    void this.load(record, true);
  }
  conceal() {
    if (this.frameCallback !== undefined) {
      this.video.cancelVideoFrameCallback(this.frameCallback);
    }
    this.frameCallback = undefined;
    this.container.classList.add('frame-pending');
  }
  reveal() {
    if (!this.record || this.failed || this.frameCallback !== undefined) {
      return;
    }
    const epoch = this.epoch;
    // currentTime can already reflect a seek while the compositor still holds
    // the previous frame. Reveal only after a matching frame has been decoded.
    this.frameCallback = this.video.requestVideoFrameCallback((_now, frame) => {
      this.frameCallback = undefined;
      if (epoch !== this.epoch || !this.record || this.failed) {
        return;
      }
      if (this.loading || this.video.seeking) {
        this.reveal();
        return;
      }
      const target = Math.max(0, (this.context.cursor() - this.record.start - this.offset) / this.factor);
      if (Math.abs(frame.mediaTime - target) > .65 / this.factor) {
        this.reveal();
        return;
      }
      this.container.classList.remove('frame-pending');
      this.setMessage('');
    });
  }
  release() {
    this.conceal();
    this.epoch++;
    clearTimeout(this.pauseTimer);
    clearTimeout(this.stallTimer);
    clearTimeout(this.recoveryTimer);
    this.abort?.abort();
    this.abort = undefined;
    this.hls?.destroy();
    this.hls = undefined;
    this.video.pause();
    this.video.removeAttribute('src');
    this.video.load();
    if (this.stream) {
      const id = this.stream;
      this.stream = undefined;
      void this.context.request('/api/streams/' + id, { method: 'DELETE', keepalive: true }).catch(() => { });
    }
    this.loading = false;
    this.playPending = false;
    this.record = undefined;
    this.failed = false;
    this.offset = 0;
    this.factor = 1;
  }
  destroy() {
    if (this.context.audioTile() === this) {
      this.context.setAudioTile(undefined);
      this.setAudio(false);
    }
    this.release();
    this.container.remove();
  }
  async load(record: Recording, recovering = false) {
    const different = this.record?.id !== record.id;
    this.release();
    if (different && !recovering) {
      this.attempts = 0;
      this.recoveryStart = 0;
    }
    this.record = record;
    const epoch = this.epoch;
    this.loading = true;
    this.reveal();
    this.setMessage('Aufnahme wird geladen …');
    this.codec.textContent = record.codec === 'hevc' ? 'H.265' : record.codec.toUpperCase();
    this.loadedSpeed = this.context.speed();
    if (recovering && this.recoveryStart) {
      this.recoveryTimer = setTimeout(() => this.fail('Wiederherstellung nach 30 Sekunden beendet'), Math.max(1, 30000 - (performance.now() - this.recoveryStart)));
    }
    const quality = this.context.quality();
    this.context.log('load', this.camera.id, quality);
    try {
      if (quality === 'native') {
        this.video.src = '/api/recordings/' + record.id + '/file';
        this.offset = 0;
      }
      else {
        this.abort = new AbortController();
        const transport = this.video.canPlayType('application/vnd.apple.mpegurl') ? 'hls' : 'mp4';
        const stream = await this.context.request<{
          id: string;
          url: string;
          offset: number;
          factor: number;
        }>('/api/streams', { method: 'POST', body: JSON.stringify({ recording: record.id, offset: Math.max(0, this.context.cursor() - record.start), profile: quality, speed: this.context.speed(), transport }), signal: this.abort.signal });
        if (epoch !== this.epoch) {
          void this.context.request('/api/streams/' + stream.id, { method: 'DELETE' }).catch(() => { });
          return;
        }
        this.stream = stream.id;
        this.offset = stream.offset;
        this.factor = stream.factor;
        if (transport === 'mp4') {
          this.video.src = stream.url;
        }
        // A growing EVENT playlist starts at the recording origin, not the live edge.
        else if (transport === 'hls') {
          const {default:Hls}=await loadHls();
          if(epoch!==this.epoch)return;
          if(!Hls.isSupported()){
            if(!this.video.canPlayType('application/vnd.apple.mpegurl'))throw new Error('HLS wird nicht unterstützt');
            this.video.src=stream.url;
          }
          else {
            this.hls = new Hls({ startPosition: 0, maxBufferLength: 20, maxMaxBufferLength: 40, backBufferLength: 10 });
            this.hls.on(Hls.Events.ERROR, (_event, data) => {
              if (data.fatal) {
                this.recover('Streaming unterbrochen');
              }
            });
            this.hls.loadSource(stream.url);
            this.hls.attachMedia(this.video);
          }
        }
        else {
          throw new Error('HLS wird nicht unterstützt');
        }
      }
      if (epoch !== this.epoch) {
        return;
      }
      this.loading = false;
      this.align();
    }
    catch (e) {
      if (epoch !== this.epoch) {
        return;
      }
      this.loading = false;
      if ((e as Error).name !== 'AbortError') {
        this.recover((e as Error).message);
      }
    }
  }
  align() {
    if (!this.record || this.loading || this.failed) {
      return;
    }
    const target = (this.context.cursor() - this.record.start - this.offset) / this.factor;
    if (this.stream && (target < -.1 || this.loadedSpeed !== this.context.speed() || target > this.video.currentTime + 3 && this.video.readyState >= 1 && !Array.from({ length: this.video.buffered.length }, (_, i) => [this.video.buffered.start(i), this.video.buffered.end(i)]).some(([a, b]) => target >= a && target < b))) {
      void this.load(this.record, true);
      return;
    }
    if (this.video.readyState >= 1 && Math.abs(this.video.currentTime - target) > .65 / this.factor) {
      try {
        this.conceal();
        this.reveal();
        this.video.currentTime = Math.max(0, target);
      }
      catch { }
    }
    const rate = this.context.speed() / this.factor;
    if (this.video.playbackRate !== rate) {
      this.video.playbackRate = rate;
    }
    if (this.context.playing()) {
      if (this.video.paused) {
        this.startPlayback();
      }
    }
    else if (!this.video.paused) {
      this.video.pause();
    }
  }
  current() { return recordingAt(this.context.playableRecordings(), this.camera.id, this.context.cursor(), this.view); }
  update(timestamp = clock(this.context.cursor())) {
    if (this.timestamp.textContent !== timestamp) {
      this.timestamp.textContent = timestamp;
    }
    const record = this.current();
    if (!record) {
      if (this.record) {
        this.release();
      }
      this.setMessage(this.view && recordingAt(this.context.recordings(), this.camera.id, this.context.cursor(), 'unknown') ? 'Kennung unklar · Erkennung in Kameraeinstellungen prüfen' : 'Keine Aufnahme zu diesem Zeitpunkt');
      return;
    }
    if (this.record?.id !== record.id) {
      void this.load(record);
      return;
    }
    this.align();
  }
  retry() {
    clearTimeout(this.pauseTimer);
    if (this.failed) {
      const record = this.record;
      this.attempts = 0;
      this.recoveryStart = 0;
      this.release();
      if (record) {
        void this.load(record);
      }
    }
  }
  paused() {
    this.video.pause();
    clearTimeout(this.stallTimer);
    if (this.stream) {
      const id = this.stream;
      this.pauseTimer = setTimeout(() => {
        void this.context.request<{
          state: string;
        }>('/api/playback-sessions/' + id).then(s => {
          if (!this.context.playing() && this.stream === id && s.state !== 'complete') {
            this.release();
          }
        }).catch(() => {
          if (!this.context.playing() && this.stream === id) {
            this.release();
          }
        });
      }, 2000);
    }
  }
}
