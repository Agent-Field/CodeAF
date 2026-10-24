// Single-flight: many callers that need one slow thing share one attempt at it.
// A success is kept for good; a failure is forgotten, so the next caller tries again.
export class Lazy {
  #make;
  #promise = null;

  constructor(make) {
    this.#make = make;
  }

  /** get answers the value, starting the attempt (with these arguments) only when none is running or done. */
  get(...args) {
    this.#promise ??= this.#make(...args).catch((e) => {
      this.#promise = null;
      throw e;
    });
    return this.#promise;
  }
}
