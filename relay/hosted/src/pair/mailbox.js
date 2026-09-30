// Mailbox: one Durable Object per nameplate, holding the two sides of one pairing. It stores
// and forwards opaque bytes and compares two SHA-256 digests of keys the clients chose; it has no
// cryptography of its own and never sees the code. A side is claimed by its first write and
// owned by the key given with it. The box lives for one TTL, then its storage is deleted by an
// alarm, so it cannot be used to keep anything.
import { DurableObject } from 'cloudflare:workers';
import { limitsOf } from '../limits.js';
import { guarded, notFound, Wire } from '../wire.js';

const forbidden = () => new Wire('forbidden', 403);

export class Mailbox extends DurableObject {
  #box = null; // { expires, keys: {a, b}, msgs: {a: [bytes], b: [bytes]} }, or null when there is none
  #waiting = new Set(); // wake functions of the long polls now waiting

  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    ctx.blockConcurrencyWhile(async () => {
      this.#box = (await ctx.storage.get('box')) ?? null;
    });
  }

  /** open makes the box, claimed on side a by keyHash, that lives until `expires`. */
  open(keyHash, expires) {
    return guarded(async () => {
      this.#box = { expires, keys: { a: keyHash, b: null }, msgs: { a: [], b: [] } };
      await this.ctx.storage.put('box', this.#box);
      await this.ctx.storage.setAlarm(expires);
    });
  }

  /** post adds one message to a side, claiming the side first if nobody has, and answers its index. */
  post(side, keyHash, bytes) {
    return guarded(async () => {
      const box = this.#live();
      box.keys[side] ??= keyHash;
      if (box.keys[side] !== keyHash) throw forbidden();
      if (box.msgs[side].length >= this.limits.pairMaxMsgsPerSide) throw new Wire('full', 409);
      const n = box.msgs[side].push(bytes) - 1;
      await this.ctx.storage.put('box', box);
      this.#wake();
      return { n };
    });
  }

  /**
   * read answers the messages `side` wrote from index `after` on, and the next index to ask for.
   * With none yet it waits up to waitMs for one; it answers null when the wait ends empty.
   */
  read(side, after, waitMs) {
    return guarded(async () => {
      if (this.#live().msgs[side].length <= after && waitMs > 0) await this.#untilWoken(waitMs);
      const msgs = this.#live().msgs[side];
      return msgs.length > after ? { msgs: msgs.slice(after), next: msgs.length } : null;
    });
  }

  /** close deletes the box for a caller holding either side's key. */
  close(keyHash) {
    return guarded(async () => {
      const { keys } = this.#live();
      if (keyHash !== keys.a && keyHash !== keys.b) throw forbidden();
      await this.#end();
    });
  }

  async alarm() {
    await this.#end();
  }

  #live() {
    if (!this.#box || Date.now() >= this.#box.expires) throw notFound();
    return this.#box;
  }

  // #end forgets the box and wakes every poll on it, which then finds it gone.
  async #end() {
    this.#box = null;
    await this.ctx.storage.deleteAll();
    this.#wake();
  }

  #wake() {
    for (const wake of this.#waiting) wake();
  }

  #untilWoken(ms) {
    return new Promise((resolve) => {
      const done = () => (clearTimeout(timer), this.#waiting.delete(done), resolve());
      const timer = setTimeout(done, ms);
      this.#waiting.add(done);
    });
  }
}
