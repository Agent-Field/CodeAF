// Port of internal/blobstore/flight.go: puts of one identity that have arrived and not finished.
// Has never answers no while one is in flight: it waits for them, or fails, but never says no.
export class Flight {
  n = 0;
  waiters = [];

  enter() {
    this.n++;
    let left = false;
    return () => {
      if (left) return;
      left = true;
      if (--this.n === 0) this.waiters.splice(0).forEach((wake) => wake());
    };
  }

  /** idle resolves true when no put is in flight, false after timeoutMs. */
  idle(timeoutMs) {
    if (this.n === 0) return Promise.resolve(true);
    return new Promise((resolve) => {
      const timer = setTimeout(() => resolve(false), timeoutMs);
      this.waiters.push(() => (clearTimeout(timer), resolve(true)));
    });
  }
}
