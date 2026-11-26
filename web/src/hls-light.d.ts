// hls.js ships no declarations for its light build; it exposes the same API
// without the controllers this viewer does not use.
declare module 'hls.js/light' {
  export * from 'hls.js';
  export { default } from 'hls.js';
}
