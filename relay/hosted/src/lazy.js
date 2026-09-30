// Single-flight: many callers that need one slow thing share one attempt at it.
// A success is kept for good; a failure is forgotten, so the next caller tries again.
export class Lazy {
  #make;
  #promise = null;

  constructor(make) {
    this.#make = make;
  }

  /** get answers the value, starting the attempt only when none is running or done. */
  get() {
    this.#promise ??= this.#make().catch((e) => {
      this.#promise = null;
      throw e;
    });
    return this.#promise;
  }

  /** started answers the attempt's promise, or null when nobody has asked yet. */
  started() {
    return this.#promise;
  }
}
