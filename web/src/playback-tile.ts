import type Hls from 'hls.js/light';
import { recordingAt, clock, type Recording } from './timeline';
import type { ViewState } from './view-state';
import { WRITING_RETRY_MS, classifyLoadIssue, conversionTooSlow, nativeErrorConverts, nextWritingStep, prefetchTarget, stallTimeoutMs, streamQuality } from './playback-state';
// Linux WebKit's GStreamer MediaSource can fail with multiple HLS audio streams.
// Fragmented MP4 avoids that backend path and still supports compressed playback.
const useProgressiveOnLinuxWebKit = navigator.platform.startsWith('Linux') && navigator.vendor === 'Apple Computer, Inc.';
interface PlaybackCamera {
  id: string;
  name: string;
}
// The light build omits subtitles, DRM, CMCD and alternate audio, none of which
// the server's own single-rendition streams use.
let hlsModule:Promise<typeof import('hls.js/light')>|undefined;
function loadHls(){return hlsModule??=import('hls.js/light')}
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
  togglePlay: () => void;
  seekBy: (seconds: number) => void;
  notify: (message: string) => void;
  request: <T>(url: string, options?: RequestInit) => Promise<T>;
}
function element<K extends keyof HTMLElementTagNameMap>(tag: K, className = '', text = ''): HTMLElementTagNameMap[K] { const e = document.createElement(tag); e.className = className; e.textContent = text; return e; }
export type ControlIcon = 'expand' | 'collapse' | 'sound' | 'muted' | 'play' | 'pause' | 'back' | 'forward' | 'reconnect' | 'pip' | 'target';
export function setControlIcon(button: Element, name: ControlIcon) {
  const paths: Record<ControlIcon, string> = { expand: 'M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5', collapse: 'M3 8h5V3m13 5h-5V3M8 21v-5H3m13 5v-5h5', sound: 'M11 5 6 9H3v6h3l5 4V5Zm4 3a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14', muted: 'M11 5 6 9H3v6h3l5 4V5Zm5 4 5 6m0-6-5 6', play: 'M7 5v14l12-7L7 5Z', pause: 'M8 5v14m8-14v14', back: 'M4 12a8 8 0 1 0 2.3-5.7M4 4v4h4', forward: 'M20 12a8 8 0 1 1-2.3-5.7M20 4v4h-4', reconnect: 'M20 11a8 8 0 0 0-14.6-4M4 4v4h4m-4 5a8 8 0 0 0 14.6 4M20 20v-4h-4', pip: 'M3 5h18v14H3zM12 12h7v5h-7z', target: 'M12 3v4m0 10v4M3 12h4m10 0h4M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z' };
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
// Codecs the browser refused in this session; their originals are converted at full resolution.
const unsupportedCodecs = new Set<string>();
// Set once full-resolution conversion could not keep up; later loads use the balanced profile.
let fullTooSlow = false;
let slowNoted = false;
let conversionNoted = false;
function noteConversion(context: TileContext) {
  if (conversionNoted) return;
  conversionNoted = true;
  context.notify('Der Browser kann das Originalformat nicht abspielen. Es wird in voller Auflösung umgewandelt.');
}
export class Tile {
  setMessage(message: string, error = false, busy = true) {
    if (this.messageState === message && this.messageError === error) {
      return;
    }
    this.messageState = message;
    this.messageError = error;
    this.message.replaceChildren();
    if (message) {
      const symbol = element('span', error ? 'symbol' : busy ? 'symbol spinner' : 'symbol', error ? '⚠' : busy ? '' : '▧');
      symbol.setAttribute('aria-hidden', 'true');
      this.message.append(symbol, element('span', 'tile-message-text', message));
    }
    this.message.hidden = !message;
    // Only errors are announced: loading states of every tile would flood screen readers.
    if (error) this.message.setAttribute('role', 'alert');
    else this.message.removeAttribute('role');
    this.message.classList.toggle('error', error);
    if (error) {
      const retry = element('button', 'tile-retry', 'Erneut versuchen');
      retry.type = 'button';
      retry.onclick = () => this.retry();
      this.message.append(retry);
    }
  }
  audioButton = element('button', 'tile-audio');
  playButton = element('button', 'tile-play');
  flash = element('div', 'tile-flash');
  clickTimer?: ReturnType<typeof setTimeout>;
  /** Profile of the current source: native, full, balanced or fast. */
  streamProfile = 'native';
  stalls: number[] = [];
  lastSeek = 0;
  lastCursor?: number;
  /**
   * Counts stalls of a full-resolution conversion. When the server cannot keep up, the tile
   * switches to the balanced profile instead of restarting the same conversion again and again.
   */
  noteStall(): boolean {
    if (this.streamProfile !== 'full' || !this.record || !conversionTooSlow(this.stalls, performance.now(), this.lastSeek)) return false;
    fullTooSlow = true;
    this.stalls = [];
    if (!slowNoted) {
      slowNoted = true;
      this.context.notify('Die Umwandlung in voller Auflösung ist für diesen Server zu langsam. Es wird „Ausgewogen“ verwendet.');
    }
    void this.load(this.record, true);
    return true;
  }
  /** Position a paused picture was last moved to; reset whenever a new source loads. */
  pausedTarget?: number;
  flashTimer?: ReturnType<typeof setTimeout>;
  /** True while the tile is shown in native fullscreen or the expanded fallback overlay. */
  enlarged() { return document.fullscreenElement === this.container || this.container.classList.contains('expanded'); }
  pipButton = element('button', 'tile-pip');
  idleTimer?: ReturnType<typeof setTimeout>;
  /** Shows the controls again and hides them after 2.5 s while an enlarged tile plays. */
  wake() {
    this.container.classList.remove('idle');
    clearTimeout(this.idleTimer);
    if (this.enlarged() && this.context.playing()) this.idleTimer = setTimeout(() => { if (this.enlarged() && this.context.playing()) this.container.classList.add('idle'); }, 2500);
  }
  setPlaying(playing: boolean) {
    this.wake();
    setControlIcon(this.playButton, playing ? 'pause' : 'play');
    const label = playing ? 'Pause' : 'Abspielen';
    this.playButton.setAttribute('aria-label', label);
    this.playButton.title = label;
  }
  /** Briefly shows the new playback state in the middle of the picture. */
  flashPlayback() {
    setControlIcon(this.flash, this.context.playing() ? 'play' : 'pause');
    this.flash.classList.remove('visible');
    void this.flash.offsetWidth;
    this.flash.classList.add('visible');
    clearTimeout(this.flashTimer);
    this.flashTimer = setTimeout(() => this.flash.classList.remove('visible'), 600);
  }
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
  writingTimer?: ReturnType<typeof setTimeout>;
  writingRetries = 0;
  prefetched = new Set<string>();
  stallTimer?: ReturnType<typeof setTimeout>;
  reloadTimer?: ReturnType<typeof setTimeout>;
  progress = element('div', 'tile-progress');
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
    // In the hotspot layout a visible marker shows that the name selects the large tile.
    const marker = element('span', 'tile-hotspot-marker');
    setControlIcon(marker, 'target');
    title.prepend(marker);
    header.append(title, this.codec);
    const footer = element('div', 'tile-footer');
    footer.append(this.timestamp, element('span', 'tile-status', 'AUFNAHME'));
    this.progress.setAttribute('aria-hidden', 'true');
    this.container.append(this.video, header, this.message, this.progress, footer);
    this.container.setAttribute('role', 'group');
    this.container.setAttribute('aria-label', title.textContent!);
    // Controls of an enlarged tile fade out while the picture plays and nothing moves.
    for (const type of ['pointermove', 'pointerdown', 'keydown'] as const) this.container.addEventListener(type, () => this.wake());
    this.container.ondblclick = () => { clearTimeout(this.clickTimer); void this.context.toggleFullscreen(this.container); };
    // In fullscreen a single click on the picture toggles playback; the delay keeps double-click for closing.
    this.video.addEventListener('click', () => {
      if (!this.enlarged()) return;
      clearTimeout(this.clickTimer);
      this.clickTimer = setTimeout(() => { this.context.togglePlay(); this.flashPlayback(); }, 250);
    });
    const transport = element('div', 'tile-transport');
    transport.setAttribute('role', 'group');
    transport.setAttribute('aria-label', 'Wiedergabe');
    const back = element('button', 'tile-seek'), forward = element('button', 'tile-seek');
    for (const [button, icon, label, seconds] of [[back, 'back', '10 Sekunden zurück', -10], [forward, 'forward', '10 Sekunden vor', 10]] as const) {
      button.type = 'button';
      setControlIcon(button, icon);
      button.setAttribute('aria-label', label);
      button.title = label;
      button.onclick = () => this.context.seekBy(seconds);
      button.ondblclick = e => e.stopPropagation();
    }
    this.playButton.type = 'button';
    this.playButton.onclick = () => this.context.togglePlay();
    this.playButton.ondblclick = e => e.stopPropagation();
    this.setPlaying(this.context.playing());
    this.pipButton.type = 'button';
    this.pipButton.hidden = !document.pictureInPictureEnabled;
    setControlIcon(this.pipButton, 'pip');
    this.pipButton.setAttribute('aria-label', 'Bild-in-Bild · ' + camera.name);
    this.pipButton.title = 'Bild-in-Bild';
    this.pipButton.ondblclick = e => e.stopPropagation();
    this.pipButton.onclick = () => {
      const request = document.pictureInPictureElement === this.video ? document.exitPictureInPicture() : this.video.requestPictureInPicture();
      void request.catch(() => this.context.notify('Bild-in-Bild ist für dieses Video nicht verfügbar.'));
    };
    const syncPip = () => this.pipButton.setAttribute('aria-pressed', String(document.pictureInPictureElement === this.video));
    this.video.addEventListener('enterpictureinpicture', syncPip);
    this.video.addEventListener('leavepictureinpicture', syncPip);
    syncPip();
    transport.append(back, this.playButton, forward);
    this.flash.setAttribute('aria-hidden', 'true');
    this.container.append(this.flash);
    footer.append(transport);
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
    actions.append(this.audioButton, this.pipButton, expand);
    footer.append(actions);
    this.video.addEventListener('error', () => {
      if (this.record && !this.loading) {
        if (this.context.quality() === 'native' && !this.stream && this.video.error?.code === 4 && nativeErrorConverts(this.record.codec, type => this.video.canPlayType(type))) {
          // The browser cannot decode this codec: continue with a full-resolution conversion.
          unsupportedCodecs.add(this.record.codec);
          noteConversion(this.context);
          void this.load(this.record, true);
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
      }
    });
    this.video.addEventListener('seeked', () => this.reveal());
    this.video.addEventListener('seeking', () => { this.conceal(); this.reveal(); });
    this.video.addEventListener('waiting', () => {
      if (this.context.playing() && !this.loading && this.noteStall()) return;
      if (this.context.playing() && !this.loading) {
        this.setMessage('Wird gepuffert …');
        clearTimeout(this.stallTimer);
        this.stallTimer = setTimeout(() => this.recover('Pufferung dauert zu lange'), stallTimeoutMs(this.context.speed()));
      }
    });
    this.video.addEventListener('playing', () => { clearTimeout(this.stallTimer); });
    this.video.addEventListener('timeupdate', () => { this.updateProgress(); this.prefetchNext(); });
    // WebKit/GStreamer may clear muted when changing playback speed.
    this.video.addEventListener('volumechange', () => {
      if (this.context.audioTile() !== this && !this.video.muted) {
        this.video.muted = true;
      }
    });
    this.video.addEventListener('ended', () => {
      if (this.record && this.context.cursor() < this.record.start + this.record.duration - 1) {
        void this.restartAfterEnd();
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
  fail(message: string) { const record = this.record; this.release(); this.record = record; this.failed = true; this.setMessage(message + ' · Bitte erneut versuchen.', true); }
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
    clearTimeout(this.writingTimer);
    clearTimeout(this.reloadTimer);
    this.reloadTimer = undefined;
    this.progress.style.transform = 'scaleX(0)';
    this.abort?.abort();
    this.abort = undefined;
    this.hls?.destroy();
    this.hls = undefined;
    this.video.pause();
    this.video.removeAttribute('src');
    this.pausedTarget = undefined;
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
    this.pausedTarget = undefined;
    const different = this.record?.id !== record.id;
    this.release();
    if (different && !recovering) {
      this.attempts = 0;
      this.recoveryStart = 0;
      this.writingRetries = 0;
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
    const quality = streamQuality(this.context.quality(), record.codec, type => this.video.canPlayType(type), unsupportedCodecs, fullTooSlow);
    this.streamProfile = quality;
    if (quality === 'full') noteConversion(this.context);
    this.codec.textContent += quality === 'full' ? ' · umgewandelt' : '';
    try {
      if (quality === 'native') {
        // A HEAD request reveals a recording that is still being written before
        // the video element turns the HTTP error into a misleading codec error.
        // Checked on every load: the file can start growing at any time, and the
        // video element would report that as a codec error.
        this.abort = new AbortController();
        await this.context.request('/api/recordings/' + record.id + '/file', { method: 'HEAD', signal: this.abort.signal });
        if (epoch !== this.epoch) {
          return;
        }
        this.writingRetries = 0;
        this.video.src = '/api/recordings/' + record.id + '/file';
        this.offset = 0;
      }
      else {
        this.abort = new AbortController();
        const transport = !useProgressiveOnLinuxWebKit && this.video.canPlayType('application/vnd.apple.mpegurl') ? 'hls' : 'mp4';
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
      if ((e as Error).name === 'AbortError') {
        this.loading = false;
        return;
      }
      const issue = classifyLoadIssue((e as { status?: number }).status, (e as Error).message);
      if (issue.kind === 'writing') {
        // The recorder still writes this file. Wait for it instead of burning recovery attempts.
        const step = nextWritingStep(this.writingRetries);
        if (step.retry) {
          this.writingRetries = step.attempt;
          // Stays loading while waiting, so video events cannot replace this message.
          this.setMessage(step.message);
          this.writingTimer = setTimeout(() => { if (epoch === this.epoch && this.record) { void this.load(this.record, true); } }, WRITING_RETRY_MS);
        }
        else {
          this.fail(step.message);
        }
        return;
      }
      this.loading = false;
      this.recover(issue.message);
    }
  }
  align() {
    if (!this.record || this.loading || this.failed) {
      return;
    }
    const target = this.target(this.record);
    // A cursor jump is a seek; stalls shortly after it do not count against the conversion.
    const cursor = this.context.cursor();
    if (this.lastCursor === undefined || Math.abs(cursor - this.lastCursor) > 2) this.lastSeek = performance.now();
    this.lastCursor = cursor;
    const behind = this.stream !== undefined && this.context.playing() && target > this.video.currentTime + 3 && this.loadedSpeed === this.context.speed() && target >= -.1;
    if (behind && this.reloadTimer === undefined && this.noteStall()) return;
    if (this.stream && (target < -.1 || this.loadedSpeed !== this.context.speed() || target > this.video.currentTime + 3 && this.video.readyState >= 1 && !Array.from({ length: this.video.buffered.length }, (_, i) => [this.video.buffered.start(i), this.video.buffered.end(i)]).some(([a, b]) => target >= a && target < b))) {
      // Scrubbing moves the cursor many times; only the last position starts an encoder.
      if (this.reloadTimer === undefined) {
        const epoch = this.epoch;
        this.reloadTimer = setTimeout(() => {
          this.reloadTimer = undefined;
          if (epoch === this.epoch && this.record && !this.failed) {
            void this.load(this.record, true);
          }
        }, 250);
      }
      return;
    }
    // While playing, small drift is corrected by the playback rate. A paused picture follows
    // every new cursor position exactly, so single-frame steps (, and .) are visible; it seeks
    // once per position, because browsers may report a time rounded to the nearest frame.
    // paused() records where the picture stopped, so the first step after pausing is seen as a move.
    const paused = !this.context.playing(), drift = Math.abs(this.video.currentTime - target) > .65 / this.factor;
    const moved = paused && this.pausedTarget !== undefined ? Math.abs(target - this.pausedTarget) > .001 : drift;
    this.pausedTarget = paused && this.video.readyState >= 1 ? target : undefined;
    if (this.video.readyState >= 1 && moved && Math.abs(this.video.currentTime - target) > .001) {
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
  // The stream ended before the recording did. If the file grew meanwhile, say
  // so: the reload then waits for the reindexed recording instead of looping.
  async restartAfterEnd() {
    const id = this.stream, epoch = this.epoch;
    let grown = false;
    if (id) {
      try {
        grown = (await this.context.request<{ sourceChanged?: boolean }>('/api/playback-sessions/' + id)).sourceChanged === true;
      }
      catch { }
    }
    if (epoch !== this.epoch) {
      return;
    }
    this.release();
    this.update();
    // load() has already shown its own loading text; replace it with the reason.
    if (grown && this.loading) {
      this.setMessage('Aufnahme wurde während der Wiedergabe erweitert …');
    }
  }
  // Reads the first bytes of the next clip: this warms the file cache, so the
  // switch at the clip boundary starts faster, and it surfaces a clip that is
  // still being written (409) early, which also queues its reindexing.
  prefetchNext() {
    if (!this.record || this.loading || this.failed || !this.context.playing()) {
      return;
    }
    const next = prefetchTarget(this.context.playableRecordings(), this.record, this.context.cursor(), this.context.speed());
    if (!next || this.prefetched.has(next.id)) {
      return;
    }
    if (this.prefetched.size > 50) {
      this.prefetched.clear();
    }
    this.prefetched.add(next.id);
    void this.context.request('/api/recordings/' + next.id + '/file', { headers: { Range: 'bytes=0-262143' }, keepalive: false }).catch(() => { });
  }
  updateProgress() {
    if (!this.record || !this.record.duration) {
      return;
    }
    const position = (this.offset + this.video.currentTime * this.factor) / this.record.duration;
    this.progress.style.transform = `scaleX(${Math.min(1, Math.max(0, position))})`;
  }
  current() { return recordingAt(this.context.playableRecordings(), this.camera.id, this.context.cursor(), this.view); }
  update(timestamp = clock(this.context.cursor())) {
    if (this.timestamp.textContent !== timestamp) {
      this.timestamp.textContent = timestamp;
    }
    const record = this.current();
    // Without a recording the footer status and codec badge would describe nothing.
    this.container.classList.toggle('no-recording', !record);
    if (!record) {
      if (this.record) {
        this.release();
      }
      if (this.codec.textContent) this.codec.textContent = '';
      this.setMessage(this.view && recordingAt(this.context.recordings(), this.camera.id, this.context.cursor(), 'unknown') ? 'Kennung unklar · Erkennung in Kameraeinstellungen prüfen' : 'Keine Aufnahme zu diesem Zeitpunkt', false, false);
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
      this.writingRetries = 0;
      this.release();
      if (record) {
        void this.load(record);
      }
    }
  }
  /** Position in the loaded media that matches the shared cursor. */
  target(record: Recording) { return (this.context.cursor() - record.start - this.offset) / this.factor; }
  paused() {
    this.video.pause();
    // The stopped picture is the starting point for frame steps (, and .) while paused.
    if (this.record && !this.loading && this.video.readyState >= 1) this.pausedTarget = this.target(this.record);
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
