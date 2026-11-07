// codeaf-trigger.ts — the TypeScript client for a codeaf factory's Trigger API.
//
// One file, no dependencies: it uses fetch and Web Crypto, so it runs on
// Node 18+, Deno, Bun, Cloudflare Workers and in a browser. It wraps only the
// four calls another tool needs:
//
//   file(entry)                    POST /api/intake
//   message(source, id, text)      POST /api/intake/{source}/{id}/messages
//   status(source, id)             GET  /api/intake/{source}/{id}
//   verifyWebhook(secret, body, header)  check a delivery's X-Codeaf-Signature
//
// A source's token and signing secret come from
// `codeaf factory source add NAME --callback URL` on the factory. The token
// files, reads and messages only work whose source is NAME.
//
// The Go client is pkg/codeaftrigger; the two sign and verify the same way.

export const SIGNATURE_HEADER = "X-Codeaf-Signature";
export const DELIVERY_HEADER = "X-Codeaf-Delivery";
export const EVENT_HEADER = "X-Codeaf-Event";

/** How far a delivery's timestamp may be from this clock, in seconds. */
export const TOLERANCE_SECONDS = 300;

export type TriggerEvent =
  | "job.filed"
  | "job.started"
  | "job.question"
  | "job.pr_opened"
  | "job.merged"
  | "job.stuck"
  | "job.done"
  | "job.message";

/** One piece of outside work. source + external_id is its identity. */
export interface Entry {
  source: string;
  external_id: string;
  title: string;
  body?: string;
  url?: string;
  author?: string;
  labels?: string[];
  /** p0 (urgent) to p3 (low). */
  priority?: string;
  /** owner/name of the repository, when the sender knows it. */
  repo?: string;
  /** collaborator or stranger; never above the source's own trust. */
  trust?: string;
}

export interface Filed {
  id: number;
  created: boolean;
}

export interface Accepted {
  job: number;
  accepted: boolean;
}

export interface Status {
  job: number;
  state: string;
  steps_done: number;
  steps_total: number;
  route?: string;
  pr_url?: string;
  question?: string;
  updated: string;
}

export interface Webhook {
  id: string;
  event: TriggerEvent;
  at: string;
  source: string;
  external_id: string;
  job: number;
  text: string;
  url?: string;
}

/** A refusal in the factory's own words. */
export class TriggerError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message || `the factory refused the call (HTTP ${status})`);
  }
}

export class Client {
  constructor(
    readonly baseURL: string,
    readonly token: string,
  ) {}

  /** Files entry, or updates the job the same source and id made before. */
  file(entry: Entry): Promise<Filed> {
    return this.call("POST", "/api/intake", entry);
  }

  /** Says text about the job filed as (source, id); author names who said it. */
  message(source: string, id: string, text: string, author?: string): Promise<Accepted> {
    return this.call("POST", jobPath(source, id) + "/messages", { text, author });
  }

  /** Reads how far the job filed as (source, id) is. */
  status(source: string, id: string): Promise<Status> {
    return this.call("GET", jobPath(source, id));
  }

  private async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (body !== undefined) headers["Content-Type"] = "application/json";
    const resp = await fetch(this.baseURL.replace(/\/+$/, "") + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await resp.text();
    if (!resp.ok) {
      let words = "";
      try {
        words = JSON.parse(text).error ?? "";
      } catch {
        // not JSON: the status says it
      }
      throw new TriggerError(resp.status, words);
    }
    return JSON.parse(text) as T;
  }
}

function jobPath(source: string, id: string): string {
  return `/api/intake/${encodeURIComponent(source)}/${encodeURIComponent(id)}`;
}

/**
 * Checks a delivery: header is its X-Codeaf-Signature value
 * (`t=<unix>,sha256=<hex>`, the HMAC-SHA256 of `<t>.<body>` keyed by the
 * secret), body the raw bytes or text exactly as received. Resolves true only
 * for a matching signature whose timestamp is within TOLERANCE_SECONDS.
 */
export async function verifyWebhook(
  secret: string,
  body: string | Uint8Array,
  header: string,
  nowSeconds: number = Math.floor(Date.now() / 1000),
): Promise<boolean> {
  if (!secret || !header) return false;
  let t = "";
  let sum = "";
  for (const part of header.split(",")) {
    const [k, v] = part.trim().split("=", 2);
    if (k === "t") t = v ?? "";
    if (k === "sha256") sum = v ?? "";
  }
  const at = Number(t);
  if (!t || !sum || !Number.isInteger(at) || Math.abs(nowSeconds - at) > TOLERANCE_SECONDS) {
    return false;
  }
  const enc = new TextEncoder();
  const bytes = typeof body === "string" ? enc.encode(body) : body;
  const signed = new Uint8Array(t.length + 1 + bytes.length);
  signed.set(enc.encode(t + "."), 0);
  signed.set(bytes, t.length + 1);
  const key = await crypto.subtle.importKey("raw", enc.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const mac = new Uint8Array(await crypto.subtle.sign("HMAC", key, signed));
  const want = Array.from(mac, (b) => b.toString(16).padStart(2, "0")).join("");
  if (want.length !== sum.length) return false;
  let diff = 0;
  for (let i = 0; i < want.length; i++) diff |= want.charCodeAt(i) ^ sum.charCodeAt(i);
  return diff === 0;
}
