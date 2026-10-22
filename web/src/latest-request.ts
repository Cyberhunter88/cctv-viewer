// Cancellation saves server work; identity checks also cover transports which
// finish despite cancellation and must never update a newer view.
export function createLatestRequest() {
  let controller: AbortController | undefined;
  return {
    begin() {
      controller?.abort();
      const own = controller = new AbortController();
      return {signal: own.signal, current: () => controller === own && !own.signal.aborted};
    },
    cancel() {
      controller?.abort();
      controller = undefined;
    },
  };
}
