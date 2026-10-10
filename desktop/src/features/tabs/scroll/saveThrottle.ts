/** Scroll writes share one trailing throttle so continuous scrolling cannot postpone persistence forever. */
export const SCROLL_SAVE_MS = 500;

export function createScrollSaveThrottle(save: () => void) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const flush = () => {
    if (timer === undefined) return;
    clearTimeout(timer);
    timer = undefined;
    save();
  };
  return {
    schedule() {
      if (timer === undefined) timer = setTimeout(flush, SCROLL_SAVE_MS);
    },
    flush,
  };
}
