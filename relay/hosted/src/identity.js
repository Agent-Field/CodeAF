// IdentityDO: one Durable Object per identity. It is the single writer of that identity's
// directory (the compare-and-swap the lease rules need), the counter of its puts in flight, and
// the place its caps are kept, because it already sees every request the identity makes.
//
// The front Worker forwards a request here on the identity named in the unsigned header, before
// the body is read. The order inside is then: prove the caller holds a cert of this identity
// (needs no body), count a put as in flight, read the body under its cap, verify the request
// signature over it, and only then spend the caller's rate budget and serve.
import { DurableObject } from 'cloudflare:workers';
import { readBody } from './body.js';
import { dropTables, erasePage } from './erasure.js';
import { Flight } from './flight.js';
import { Lazy } from './lazy.js';
import { limitsOf, RateLimit } from './limits.js';
import { matchRoute } from './routes.js';
import { policyOf } from './rules.js';
import { Tenant } from './tenant.js';
import { checkCert, checkRequest, Refusal } from './verify.js';
import { gone, ipOf, rateLimited, respond, unwrap } from './wire.js';

const SIGNED = { identity: 'codeaf-identity', cert: 'codeaf-cert', time: 'codeaf-time', sig: 'codeaf-sig' };
const signedHeaders = (request) => Object.fromEntries(Object.entries(SIGNED).map(([k, h]) => [k, request.headers.get(h)]));

export class IdentityDO extends DurableObject {
  flight = new Flight();
  #tenant = null;
  #gone = null; // whether the tombstone is written, read once from storage
  #admitted = new Lazy((ip) => this.#admitNewcomer(ip)); // single-flight: a burst of first requests is one admission

  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    this.policy = policyOf(env.LEASE_POLICY);
    this.identityRate = new RateLimit(this.limits.requestsPerMinute);
    this.deviceRate = new RateLimit(this.limits.requestsPerMinutePerDevice);
  }

  fetch(request) {
    return respond(() => this.#serve(request));
  }

  async #serve(request) {
    const url = new URL(request.url);
    const route = matchRoute(request.method, url.pathname);
    const caller = await checkCert(signedHeaders(request));
    const leave = route.frames ? this.#enterPut() : () => {};
    try {
      const body = await readBody(request, route.limit, route.over);
      const who = await checkRequest(caller, { method: request.method, uri: url.pathname + url.search }, body, Date.now());
      await this.#refuseGone();
      await this.#admitted.get(ipOf(request, this.env));
      const tenant = this.#tenantOf(who.identity);
      this.#admit(tenant, who.device);
      if (route.write) tenant.assertWritable();
      return await route.handler({ tenant, device: who.device, body }, route.args);
    } finally {
      leave();
    }
  }

  /** #enterPut counts a put from its arrival, and turns away one more than the isolate can hold in memory. */
  #enterPut() {
    if (this.flight.count >= this.limits.concurrentPuts) throw rateLimited(1);
    return this.flight.enter();
  }

  // The object serves one identity for its whole life, and the front Worker names it from the same
  // header the cert was just verified under, so the first verified identity is the only one.
  #tenantOf(identity) {
    if (!this.#tenant) this.ctx.storage.put('identity', identity); // the alarm wakes with no request to name it
    this.#tenant ??= new Tenant({
      identity,
      sql: this.ctx.storage.sql,
      bucket: this.env.FRAMES,
      clock: Date.now,
      policy: this.policy,
      limits: this.limits,
      flight: this.flight,
      arm: (at) => this.ctx.waitUntil(this.#arm(at)),
    });
    return this.#tenant;
  }

  /**
   * alarm does the two things an object is woken for: write the counts that changed since the last
   * write (stats.js), and delete the identity once its retirement is due. It always ends by arming
   * the retirement deadline again, because #arm only ever moves the alarm earlier and a stats alarm
   * may have stood in front of it.
   */
  async alarm() {
    const identity = await this.ctx.storage.get('identity');
    if (!identity) return;
    if (await this.#isGone()) return this.#erase(identity);
    const tenant = this.#tenantOf(identity);
    tenant.stats.flush();
    const at = tenant.retireAt();
    if (at === undefined) return;
    return at <= Date.now() ? this.#erase(identity) : this.#arm(at);
  }

  /** #arm wakes the object at `at` unless it is already set to wake sooner, so no caller can push another's alarm later. */
  async #arm(at) {
    const set = await this.ctx.storage.getAlarm();
    if (set === null || at < set) await this.ctx.storage.setAlarm(at);
  }

  async #isGone() {
    this.#gone ??= Boolean(await this.ctx.storage.get('gone'));
    return this.#gone;
  }

  async #refuseGone() {
    if (await this.#isGone()) throw gone();
  }

  /**
   * #erase deletes a retired identity. The tombstone goes first, so a crash half way leaves an
   * identity that already answers "gone" and that the next alarm finishes erasing; frames then go
   * a page per turn, and only when none are left do the tables, so the tombstone is all that stays.
   */
  async #erase(identity) {
    await this.ctx.storage.put('gone', Date.now());
    this.#gone = true;
    this.#tenant = null;
    if (await erasePage(this.env.FRAMES, `${identity}/`, this.limits.sweepPageSize)) return this.#arm(Date.now());
    dropTables(this.ctx.storage.sql);
    await this.ctx.storage.delete(['identity', 'admitted']);
  }

  /**
   * #admit turns away a revoked device, then charges the request to the device's rate and the
   * identity's. The device goes first, so one noisy device that is refused spends none of the
   * identity's share, which its other devices still need.
   */
  #admit(tenant, device) {
    if (tenant.dir.revoked(device)) throw new Refusal('revoked', 'device revoked');
    const now = Date.now();
    this.deviceRate.admit(device, now);
    this.identityRate.admit('', now);
  }

  // An identity is new until its first admitted request, and admission happens before the tenant
  // exists, so a refused identity has created no table and written nothing: only an admitted one
  // is ever stored. Admitted identities are remembered, so the count never touches an existing
  // identity, whatever address it comes from.
  async #admitNewcomer(ip) {
    if (await this.ctx.storage.get('admitted')) return;
    const gate = this.env.NEWCOMERS;
    unwrap(await gate.get(gate.idFromName('gate')).admit(ip));
    await this.ctx.storage.put('admitted', true);
  }
}
