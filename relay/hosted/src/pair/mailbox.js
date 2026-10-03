// Mailbox: one Durable Object per nameplate, holding the two sides of one pairing. It stores
// and forwards opaque bytes and compares two SHA-256 digests of keys the clients chose; it has no
// cryptography of its own and never sees the code. A side is claimed by its first write and
// owned by the key given with it. The box lives for one TTL, then its storage is deleted by an
// alarm, so it cannot be used to keep anything.
import { DurableObject } from 'cloudflare:workers';
import { limitsOf } from '../limits.js';
import { guarded } from '../wire.js';
import { fence } from './generation.js';
import { forbidden, gone, sideFull } from './refusals.js';

export class Mailbox extends DurableObject {
  #box = null; // { expires, gen, keys: {a, b}, msgs: {a: [bytes], b: [bytes]} }, or null when there is none
  #waiting = new Set(); // wake functions of the long polls now waiting

  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    ctx.blockConcurrencyWhile(async () => {
      this.#box = (await ctx.storage.get('box')) ?? null;
    });
  }

  /** open makes the box, claimed on side a by keyHash, that lives until `expires`; `gen` names this opening. */
  open(keyHash, expires, gen) {
    return guarded(async () => {
      this.#box = { expires, gen, keys: { a: keyHash, b: null }, msgs: { a: [], b: [] } };
      await this.ctx.storage.put('box', this.#box);
      await this.ctx.storage.setAlarm(expires);
    });
  }

  /** post adds one message to a side, claiming the side first if nobody has, and answers its index and the box's generation. */
  post(side, keyHash, bytes, gen) {
    return guarded(async () => {
      const box = this.#live(gen);
      box.keys[side] ??= keyHash;
      if (box.keys[side] !== keyHash) throw forbidden();
      if (box.msgs[side].length >= this.limits.pairMaxMsgsPerSide) throw sideFull();
      const n = box.msgs[side].push(bytes) - 1;
      await this.ctx.storage.put('box', box);
      this.#wake();
      return { n, gen: box.gen ?? null };
    });
  }

  /**
   * read answers the box's generation and the messages `side` wrote from index `after` on, with the next index to ask for.
   * With none yet it waits up to waitMs for one; the messages are null when the wait ends empty.
   */
  read(side, after, waitMs, gen) {
    return guarded(async () => {
      if (this.#live(gen).msgs[side].length <= after && waitMs > 0) await this.#untilWoken(waitMs);
      const box = this.#live(gen);
      const msgs = box.msgs[side];
      return { gen: box.gen ?? null, ...(msgs.length > after && { msgs: msgs.slice(after), next: msgs.length }) };
    });
  }

  /** close deletes the box for a caller holding either side's key. */
  close(keyHash, gen) {
    return guarded(async () => {
      const { keys } = this.#live(gen);
      if (keyHash !== keys.a && keyHash !== keys.b) throw forbidden();
      await this.#end();
    });
  }

  async alarm() {
    await this.#end();
  }

  // #live is the box, or gone when there is none, it has expired, or `gen` names an earlier opening of this nameplate.
  #live(gen) {
    if (!this.#box || Date.now() >= this.#box.expires) throw gone();
    fence(this.#box, gen);
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
