// Serial runs async jobs one after another, in the order they were asked for, whether or not an
// earlier one failed. A job that reads state and then writes it cannot then be overtaken by another.
export class Serial {
  #tail = Promise.resolve();

  run(job) {
    const result = this.#tail.then(job);
    this.#tail = result.catch(() => {});
    return result;
  }
}
