// The abuse breaker for a live stream. It is the relay's one answer to a surface that sends more than any live view ever
// does, and it is computed from the clock of the message that arrives: there is NO timer and NO alarm behind it, because
// a timer is exactly what would keep an idle object awake, and an idle live view must cost nothing (room.js).
//
// IT CUTS AND DOES NOT SHAPE. The relay cannot read a byte it carries, so it cannot batch, delay or fold anything: its
// only move is to end the stream, and the host and the viewer pace themselves long before this matters. The number is four
// times the pace a correct host holds, so a correct host never trips it; it exists for a modified one.
import { BREAKER_BYTES_PER_SECOND, BREAKER_WINDOW_MS } from './carrier.js';

/**
 * Breaker is a leaky bucket that holds BREAKER_WINDOW_MS worth of BREAKER_BYTES_PER_SECOND and drains at that rate, so
 * a stream is over its rate when it has sent more than that rate sustained across the window, whether as one burst or as a
 * steady flow a little above the line. Its state is two numbers, and it is lost with the object's memory, which only ever
 * forgives a stream.
 */
export class Breaker {
  #level = 0;
  #at = null;

  constructor(perSecond = BREAKER_BYTES_PER_SECOND, windowMs = BREAKER_WINDOW_MS) {
    this.perSecond = perSecond;
    this.capacity = (perSecond * windowMs) / 1000;
  }

  /** charge counts `bytes` arriving at `now` (milliseconds) and answers true once the stream is over its rate. */
  charge(bytes, now) {
    if (this.#at !== null && now > this.#at) {
      this.#level = Math.max(0, this.#level - ((now - this.#at) * this.perSecond) / 1000);
    }
    this.#at = this.#at === null ? now : Math.max(this.#at, now);
    this.#level += bytes;
    return this.#level > this.capacity;
  }
}
