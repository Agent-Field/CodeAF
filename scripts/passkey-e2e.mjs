// Passkeys end to end: a real `codeaf factory serve` over TLS on localhost,
// two real `codeaf app` engines (two machines), and real browsers driving the
// server's own ceremony page.
//
//   Chromium, with CDP virtual authenticators (WebAuthn.addVirtualAuthenticator,
//   user verification on): one per transport the owner asked for — internal
//   (Touch ID / Windows Hello), hybrid (a phone over QR) and usb
//   (a security key).
//   WebKit and Firefox: a smoke that the page loads, detects support and
//   reports cleanly. WebKit exposes no virtual authenticator to Playwright;
//   Firefox's soft token is tried for a whole ceremony and reported SKIP, not
//   passed, where the build refuses it.
//
// The scenario is the contract's: join by passkey (the operator link) → sign
// out → sign in by passkey → add a second passkey → sign in on another machine
// → remove one → the other machine's session is revoked → the last way in
// cannot be removed. Then the page with no WebAuthn at all, and at 320 px.
//
// Run it through scripts/passkey-e2e.sh, which builds nothing: run `make build`
// first. It needs openssl and a playwright-core whose browsers are installed;
// PLAYWRIGHT_CORE names that package's directory.
import { spawn, execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";
import https from "node:https";
import readline from "node:readline";

const bin = process.env.CODEAF_BIN;
const pwDir = process.env.PLAYWRIGHT_CORE;
if (!bin || !pwDir) {
  console.error("set CODEAF_BIN and PLAYWRIGHT_CORE");
  process.exit(2);
}
const require = createRequire(import.meta.url);
const { chromium, webkit, firefox } = require(pwDir);

const results = [];
function pass(name, detail = "") {
  results.push({ name, ok: true, detail });
  console.log(`PASS ${name}${detail ? " — " + detail : ""}`);
}
function fail(name, detail) {
  results.push({ name, ok: false, detail });
  console.log(`FAIL ${name} — ${detail}`);
}
// skip records what this machine cannot prove, so it is never read as proven.
function skip(name, detail) {
  results.push({ name, ok: true, skipped: true, detail });
  console.log(`SKIP ${name} — ${detail}`);
}
function check(name, cond, detail = "") {
  if (cond) pass(name, detail);
  else fail(name, detail || "condition false");
  return cond;
}

const work = mkdtempSync(join(tmpdir(), "codeaf-passkey-e2e-"));
const children = [];
function cleanup() {
  for (const c of children) {
    try { c.kill("SIGTERM"); } catch {}
  }
  rmSync(work, { recursive: true, force: true });
}
process.on("exit", cleanup);

async function freePort() {
  return new Promise((resolve) => {
    const s = net.createServer();
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });
}

// ── a certificate for localhost ──────────────────────────────────────────────
const certFile = join(work, "cert.pem");
const keyFile = join(work, "key.pem");
execFileSync("openssl", ["req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
  "-keyout", keyFile, "-out", certFile, "-days", "1", "-subj", "/CN=localhost",
  "-addext", "subjectAltName=DNS:localhost"], { stdio: "ignore" });
// Chrome trusts exactly this key, so the page is a real secure context with no
// certificate error (WebAuthn refuses a page whose certificate is in error).
const spki = execFileSync("sh", ["-c",
  `openssl x509 -in '${certFile}' -pubkey -noout | openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | base64`]).toString().trim();

// ── the organization server ─────────────────────────────────────────────────
const port = await freePort();
const origin = `https://localhost:${port}`;
const serverHome = join(work, "server-home");
const cfgPath = join(work, "config.yaml");
writeFileSync(cfgPath, [
  `listen: 127.0.0.1:${port}`,
  `name: Acme`,
  `access_token: e2e-host-${Date.now()}`,
  `tls: files`,
  `domain: localhost`,
  `cert_file: ${certFile}`,
  `key_file: ${keyFile}`,
  "",
].join("\n"));
const env = { ...process.env, CODEAF_HOME: serverHome, SSL_CERT_FILE: certFile, NO_COLOR: "1" };
const server = spawn(bin, ["factory", "serve", "--config", cfgPath], { env, stdio: ["ignore", "pipe", "pipe"] });
children.push(server);
let serverLog = "";
server.stderr.on("data", (d) => { serverLog += d; });
const operatorLink = await new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(new Error("no operator link from factory serve:\n" + serverLog)), 30000);
  readline.createInterface({ input: server.stdout }).on("line", (line) => {
    const m = line.match(/operator access link: (codeaf:\/\/\S+)/);
    if (m) {
      clearTimeout(timer);
      resolve(m[1]);
    }
  });
});
pass("factory serve over TLS on localhost", origin);

const about = await new Promise((resolve) => {
  https.get(origin + "/v1/about", { ca: readFileSync(certFile) }, (res) => {
    let body = "";
    res.on("data", (d) => { body += d; });
    res.on("end", () => { try { resolve(JSON.parse(body)); } catch { resolve({}); } });
  }).on("error", () => resolve({}));
});
check("about lists passkey first", JSON.stringify(about.sign_in) === JSON.stringify(["passkey", "password"]), JSON.stringify(about.sign_in));

// ── two engines, two machines ───────────────────────────────────────────────
class Engine {
  constructor(name) {
    this.name = name;
    this.home = join(work, name);
    this.next = 1;
    this.waiting = new Map();
    this.lines = [];
    this.proc = spawn(bin, ["app", "--home", this.home], {
      env: { ...process.env, CODEAF_HOME: this.home, SSL_CERT_FILE: certFile, NO_COLOR: "1" },
      stdio: ["pipe", "pipe", "pipe"],
    });
    children.push(this.proc);
    this.hello = new Promise((resolve) => { this.sayHello = resolve; });
    readline.createInterface({ input: this.proc.stdout }).on("line", (line) => {
      this.lines.push(line);
      let msg;
      try { msg = JSON.parse(line); } catch { return; }
      if (msg.event === "hello") this.sayHello(msg.data);
      if (msg.id && this.waiting.has(msg.id)) {
        const { resolve } = this.waiting.get(msg.id);
        this.waiting.delete(msg.id);
        resolve(msg);
      }
    });
  }
  async call(method, params = {}) {
    const id = this.next++;
    const answer = new Promise((resolve) => this.waiting.set(id, { resolve }));
    this.proc.stdin.write(JSON.stringify({ id, method, params }) + "\n");
    const msg = await answer;
    if (msg.error) {
      const e = new Error(`${method}: ${msg.error.code}: ${msg.error.message}`);
      e.code = msg.error.code;
      throw e;
    }
    return msg.result;
  }
  async pollUntilDone(flow, caller) {
    for (let i = 0; i < 60; i++) {
      const p = await this.call("factory.passkey_poll", { flow, caller });
      if (p.state !== "pending") return p;
      await new Promise((r) => setTimeout(r, 500));
    }
    throw new Error("poll never finished");
  }
}

const santosh = new Engine("santosh-mac");
const helloA = await santosh.hello;
check("hello announces factory_passkey", (helloA.features || []).includes("factory_passkey"));

// ── the browsers ────────────────────────────────────────────────────────────
const browser = await chromium.launch({ args: [`--ignore-certificate-errors-spki-list=${spki}`] });

// authenticatorFor is a fresh Chromium context with one virtual authenticator
// on transport, user verification on, presence answered automatically.
async function authenticatorFor(transport, width = 1024) {
  const context = await browser.newContext({ viewport: { width, height: 800 } });
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("WebAuthn.enable", { enableUI: false });
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2", transport, hasResidentKey: true, hasUserVerification: true,
      isUserVerified: true, automaticPresenceSimulation: true,
    },
  });
  return { context, page, cdp, authenticatorId, transport };
}

// ceremony opens url as the default browser would and presses the button.
async function ceremony(a, url, fill = {}) {
  await a.page.goto(url);
  const support = await a.page.locator("#ceremony").getAttribute("data-support");
  for (const [id, value] of Object.entries(fill)) {
    await a.page.fill("#" + id, value);
  }
  await a.page.click("#go");
  const status = await settled(a.page);
  return { support, status };
}

// settled is the page's status once the ceremony has answered: "Done" once
// the browser has reached the engine's loopback and its page says so, else
// the ceremony page's own words. It is read from outside: the page's CSP
// forbids the eval a waitForFunction needs.
async function settled(page) {
  for (let i = 0; i < 150; i++) {
    if (page.url().startsWith("http://127.0.0.1:")) {
      try {
        const h = (await page.textContent("h1")) || "";
        if (h) return h;
      } catch { /* still navigating */ }
    } else {
      let s = "";
      try { s = (await page.textContent("#status")) || ""; } catch { /* navigating */ }
      if (s && s !== "Checking…" && !s.startsWith("Finishing on the computer")) return s;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("the page never answered");
}

try {
  // 1. Join by passkey through the operator link, on the internal authenticator.
  const access = await santosh.call("factory.access_check", { link: operatorLink, caller: "e2e" });
  check("access card lists passkey first", access.factory.sign_in?.[0] === "passkey", JSON.stringify(access.factory.sign_in));
  const internal = await authenticatorFor("internal");
  let started = await santosh.call("factory.passkey_start", { link: operatorLink, caller: "e2e", purpose: "join" });
  check("start answers the server's own page", started.url.startsWith(origin + "/passkey/"), started.url);
  let pending = await santosh.call("factory.passkey_poll", { flow: started.flow, caller: "e2e" });
  check("poll is pending before the browser", pending.state === "pending");
  let page = await ceremony(internal, started.url, { username: "santosh", name: "Santosh" });
  check("chromium (internal) join page says done", page.status.startsWith("Done"), page.status);
  const creds = await internal.cdp.send("WebAuthn.getCredentials", { authenticatorId: internal.authenticatorId });
  check("the passkey is discoverable and on the authenticator", creds.credentials.length === 1 && creds.credentials[0].isResidentCredential);
  let polled = await santosh.pollUntilDone(started.flow, "e2e");
  check("join poll done with a connection", polled.state === "done" && !!polled.factory, JSON.stringify(polled));
  const factoryID = polled.factory;
  let me = await santosh.call("factory.me", { factory: factoryID });
  check("santosh is the operator", me.handle === "santosh" && me.operator === true, JSON.stringify({ handle: me.handle, operator: me.operator }));
  check("factory.me says a passkey sign-in, not GitHub", me.sign_in_kind === "passkey" && me.github_signed_in === false,
    JSON.stringify({ sign_in_kind: me.sign_in_kind, github_signed_in: me.github_signed_in }));

  // 2. Sign out, and sign in again with the same passkey (no username typed).
  await santosh.call("factory.sign_out", { factory: factoryID });
  let signedOut = false;
  try { await santosh.call("factory.me", { factory: factoryID }); } catch (e) { signedOut = e.code === "signed_out"; }
  check("signed out", signedOut);
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "sign_in" });
  page = await ceremony(internal, started.url);
  check("chromium (internal) sign-in page says done", page.status.startsWith("Done"), page.status);
  polled = await santosh.pollUntilDone(started.flow, "e2e");
  me = await santosh.call("factory.me", { factory: factoryID });
  check("signed in again by passkey", polled.state === "done" && me.handle === "santosh");
  let again = "";
  try { await santosh.call("factory.passkey_poll", { flow: started.flow, caller: "e2e" }); } catch (e) { again = e.code; }
  check("a done flow is answered once", again === "not_found", again);

  // 3. Add a second passkey on a phone (hybrid) and a third on a security key (usb).
  const hybrid = await authenticatorFor("hybrid");
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "add" });
  page = await ceremony(hybrid, started.url, { label: "Pixel phone" });
  check("chromium (hybrid) add page says done", page.status.startsWith("Done"), page.status);
  polled = await santosh.pollUntilDone(started.flow, "e2e");
  check("add poll done", polled.state === "done" && polled.factory === factoryID);
  const usb = await authenticatorFor("usb");
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "add" });
  page = await ceremony(usb, started.url, { label: "YubiKey" });
  check("chromium (usb) add page says done", page.status.startsWith("Done"), page.status);
  await santosh.pollUntilDone(started.flow, "e2e");
  let list = await santosh.call("factory.passkey_list", { factory: factoryID });
  check("three passkeys listed", list.passkeys.length === 3 && list.password === false,
    list.passkeys.map((p) => p.name).join(", "));

  // 4. Another machine attaches and signs in with the security key.
  const laptop = new Engine("santosh-linux");
  await laptop.hello;
  const attached = await laptop.call("factory.attach", { url: origin });
  started = await laptop.call("factory.passkey_start", { factory: attached.id, caller: "e2e2", purpose: "sign_in" });
  page = await ceremony(usb, started.url);
  check("chromium (usb) sign-in on another machine", page.status.startsWith("Done"), page.status);
  await laptop.pollUntilDone(started.flow, "e2e2");
  let laptopMe = await laptop.call("factory.me", { factory: attached.id });
  check("the other machine is signed in", laptopMe.handle === "santosh");
  // And the phone signs in too, on the first machine.
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "sign_in" });
  page = await ceremony(hybrid, started.url);
  check("chromium (hybrid) sign-in page says done", page.status.startsWith("Done"), page.status);
  await santosh.pollUntilDone(started.flow, "e2e");

  // 4b. C1: a sign-in started by someone else, opened on the victim's device.
  // The victim's browser cannot reach the starter's 127.0.0.1 (a different
  // computer), which this context stands for by refusing every loopback
  // navigation. The ceremony passes; the starter gets nothing, ever.
  const victim = await authenticatorFor("usb");
  await victim.context.route(/^http:\/\/127\.0\.0\.1:/, (route) => route.abort("connectionrefused"));
  const victimCreds = await usb.cdp.send("WebAuthn.getCredentials", { authenticatorId: usb.authenticatorId });
  for (const c of victimCreds.credentials) {
    await victim.cdp.send("WebAuthn.addCredential", { authenticatorId: victim.authenticatorId, credential: c });
  }
  await laptop.call("factory.sign_out", { factory: attached.id });
  started = await laptop.call("factory.passkey_start", { factory: attached.id, caller: "e2e2", purpose: "sign_in" });
  await victim.page.goto(started.url);
  await victim.page.click("#go");
  // The hand-over fails there (no codeaf at that 127.0.0.1); coming back to
  // the address, the page says where it finishes.
  for (let i = 0; i < 100 && victim.page.url().startsWith(origin); i++) {
    await new Promise((r) => setTimeout(r, 100));
  }
  await victim.page.goto(started.url);
  const phished = (await victim.page.textContent("h1", { timeout: 5000 })) || "";
  check("C1: the victim's page says to finish on the computer that started it", phished.startsWith("Finish on the computer"), phished);
  let stolen = { state: "pending" };
  for (let i = 0; i < 16 && stolen.state === "pending"; i++) {
    stolen = await laptop.call("factory.passkey_poll", { flow: started.flow, caller: "e2e2" });
    await new Promise((r) => setTimeout(r, 500));
  }
  check("C1: the starter's poll ends elsewhere, with no connection", stolen.state === "elsewhere" && !stolen.factory, JSON.stringify(stolen));
  let stillOut = "";
  try { await laptop.call("factory.me", { factory: attached.id }); } catch (e) { stillOut = e.code; }
  check("C1: the starter is still signed out", stillOut === "signed_out", stillOut);
  await laptop.call("factory.passkey_cancel", { flow: started.flow, caller: "e2e2" });
  await victim.context.close();
  // The same person on the starting computer finishes as usual.
  started = await laptop.call("factory.passkey_start", { factory: attached.id, caller: "e2e2", purpose: "sign_in" });
  page = await ceremony(usb, started.url);
  await laptop.pollUntilDone(started.flow, "e2e2");
  laptopMe = await laptop.call("factory.me", { factory: attached.id });
  check("C1: on the starting computer the same passkey signs in", page.status.startsWith("Done") && laptopMe.handle === "santosh", page.status);

  // 4c. H1: a step-up with a passkey is its own short ceremony.
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "confirm" });
  await hybrid.page.goto(started.url);
  const confirmHeading = (await hybrid.page.textContent("h1")) || "";
  page = await ceremony(hybrid, started.url);
  polled = await santosh.pollUntilDone(started.flow, "e2e");
  check("H1: confirm it's you, with a passkey", confirmHeading.includes("Confirm it") && page.status.startsWith("Done") &&
    polled.state === "done" && polled.factory === factoryID, `${confirmHeading} / ${page.status} / ${JSON.stringify(polled)}`);
  let noPassword = "";
  try { await santosh.call("factory.password_confirm", { factory: factoryID, password: "not the password at all" }); } catch (e) { noPassword = e.code; }
  check("H1: a passkey person has no password to confirm with", noPassword === "not_found", noPassword);

  // 5. Remove the internal passkey from the first machine: the other machine's session ends.
  const internalRow = list.passkeys.find((p) => p.name !== "Pixel phone" && p.name !== "YubiKey");
  await santosh.call("factory.passkey_remove", { factory: factoryID, id: internalRow.id });
  let laptopOut = "";
  try { await laptop.call("factory.me", { factory: attached.id }); } catch (e) { laptopOut = e.code; }
  check("removing a passkey revoked the other machine's session", laptopOut === "signed_out", laptopOut);
  me = await santosh.call("factory.me", { factory: factoryID });
  check("the machine that removed it stays signed in", me.handle === "santosh");
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "sign_in" });
  await internal.page.goto(started.url);
  await internal.page.click("#go");
  const removedSays = await settled(internal.page);
  check("the removed passkey no longer signs in", !removedSays.startsWith("Done"), removedSays);

  // 6. The last way in cannot go.
  list = await santosh.call("factory.passkey_list", { factory: factoryID });
  await santosh.call("factory.passkey_remove", { factory: factoryID, id: list.passkeys[0].id });
  let last = "";
  try { await santosh.call("factory.passkey_remove", { factory: factoryID, id: list.passkeys[1].id }); } catch (e) { last = e.message; }
  check("the last way in is refused", last.includes("only way to sign in"), last);

  // 7. A browser with no WebAuthn is told plainly, with the password named.
  started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "sign_in" });
  const bare = await browser.newContext();
  await bare.addInitScript(() => { delete window.PublicKeyCredential; });
  const barePage = await bare.newPage();
  await barePage.goto(started.url);
  const bareSays = await barePage.textContent("#status");
  const bareSupport = await barePage.locator("#ceremony").getAttribute("data-support");
  check("no WebAuthn: the page says so and offers the password", bareSupport === "no" &&
    bareSays.includes("can't use passkeys") && bareSays.includes("password") && !(await barePage.isVisible("#go")), bareSays);

  // 8. 320 px: nothing spills sideways (iOS Safari / Android Chrome widths).
  const narrow = await browser.newContext({ viewport: { width: 320, height: 640 }, isMobile: true, hasTouch: true });
  const narrowPage = await narrow.newPage();
  const joinAgain = await santosh.call("factory.link_create", { factory: factoryID }).catch(() => null);
  const invite = joinAgain?.entry?.url || "";
  started = invite
    ? await santosh.call("factory.passkey_start", { link: invite, caller: "e2e", purpose: "join" })
    : started;
  await narrowPage.goto(started.url);
  const overflow = await narrowPage.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  const buttonBox = await narrowPage.locator("#go").boundingBox();
  check("320 px: no horizontal scroll, button inside the screen", overflow <= 0 && buttonBox && buttonBox.x + buttonBox.width <= 320,
    `overflow ${overflow}, button ${buttonBox ? Math.round(buttonBox.x + buttonBox.width) : "none"}`);
  await narrowPage.screenshot({ path: join(process.env.SHOTS || work, "passkey-320.png"), fullPage: true });
  await internal.page.goto(started.url);
  await internal.page.screenshot({ path: join(process.env.SHOTS || work, "passkey-1024.png") });

  // 9. The engines never wrote a session, a verifier or a challenge.
  for (const e of [santosh, laptop]) {
    const leaked = e.lines.filter((l) => /access_token|verifier|code_challenge/.test(l));
    check(`${e.name}: no secret on the wire`, leaked.length === 0, leaked[0]?.slice(0, 120) || "");
  }

  // 10. WebKit and Firefox: the page loads, detects support and reports cleanly.
  for (const [name, engine] of [["webkit", webkit], ["firefox", firefox]]) {
    let other;
    try {
      // Firefox's own software token stands in for an authenticator there,
      // the nearest it has to a virtual one.
      other = await engine.launch(name === "firefox" ? { firefoxUserPrefs: {
        "security.webauth.webauthn_enable_softtoken": true, "security.webauth.webauthn_enable_usbtoken": false,
      } } : {});
    } catch (e) {
      fail(`${name} smoke`, "browser not installed: " + String(e.message).split("\n")[0]);
      continue;
    }
    try {
      const ctx = await other.newContext({ ignoreHTTPSErrors: true });
      const p = await ctx.newPage();
      const errors = [];
      p.on("pageerror", (err) => errors.push(String(err)));
      started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "sign_in" });
      await p.goto(started.url);
      const support = await p.locator("#ceremony").getAttribute("data-support");
      const says = await p.textContent("#status");
      const button = await p.isVisible("#go");
      const clean = errors.length === 0 && ((support === "yes" && button && says === "") ||
        (support === "no" && !button && says.includes("can't use passkeys")));
      check(`${name} smoke: loads, detects, reports cleanly`, clean,
        `support=${support} button=${button} status=${JSON.stringify(says)} errors=${errors.length}`);
      if (name === "firefox" && support === "yes") {
        // A whole ceremony on Firefox's soft token: add it, then sign in with it.
        started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "add" });
        await p.goto(started.url);
        await p.fill("#label", "Firefox soft token");
        await p.click("#go");
        const added = await settled(p);
        const addPoll = added.startsWith("Done") ? await santosh.pollUntilDone(started.flow, "e2e") : { state: "not done" };
        if (addPoll.state !== "done" && added.startsWith("Nothing was saved")) {
          // Playwright's headless Firefox refuses every create on its soft
          // token (NotAllowedError, even with no requirements at all), so a
          // whole Firefox ceremony is not provable here; the smoke above is.
          skip("firefox (soft token) ceremony", "this Firefox build's soft token refuses every create headless: " + added);
        } else {
          check("firefox (soft token) add", addPoll.state === "done", added);
        }
        if (addPoll.state === "done") {
          started = await santosh.call("factory.passkey_start", { factory: factoryID, caller: "e2e", purpose: "sign_in" });
          await p.goto(started.url);
          await p.click("#go");
          const signed = await settled(p);
          const inPoll = signed.startsWith("Done") ? await santosh.pollUntilDone(started.flow, "e2e") : { state: "not done" };
          check("firefox (soft token) sign-in", inPoll.state === "done", signed);
        }
      }
    } finally {
      await other.close();
    }
  }
} catch (e) {
  fail("scenario", e.stack || String(e));
} finally {
  await browser.close();
}

const failed = results.filter((r) => !r.ok);
const skipped = results.filter((r) => r.skipped);
console.log(`\n${results.length - failed.length - skipped.length}/${results.length} passed, ${skipped.length} skipped, ${failed.length} failed`);
process.exit(failed.length ? 1 : 0);
