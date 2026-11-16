// The passkey ceremony page. It runs on the organization server's own origin,
// which is what lets the browser use a passkey for it, and talks only to that
// origin: it asks for a challenge, hands it to the browser's WebAuthn, and
// posts back what the authenticator signed. Nothing secret is kept here; the
// flow id in the url is the page's only handle, and the session it ends in is
// handed to codeaf, never to this page.
//
// THE HAND-OVER. When the server has verified the ceremony it answers a
// one-time address on 127.0.0.1, codeaf's own listener on the computer that
// started this; the page sends the browser there. A browser on any other
// device reaches its own 127.0.0.1, never codeaf's, so nothing finishes
// there: that is what keeps a sign-in started by someone else from becoming
// theirs.
"use strict";

(function () {
  var main = document.getElementById("ceremony");
  if (!main || main.dataset.ended === "true") {
    return;
  }
  var flow = main.dataset.flow;
  var host = main.dataset.host;
  var form = document.getElementById("form");
  var go = document.getElementById("go");
  var status = document.getElementById("status");
  var lede = document.getElementById("lede");

  function say(words, bad) {
    status.textContent = words;
    status.className = bad ? "status bad" : "status";
  }

  function finished(words, redirect) {
    if (form) {
      form.hidden = true;
    }
    lede.textContent = "";
    say(words, false);
    document.title = "Done · codeaf";
    // Only ever codeaf's own loopback listener, as the server spelled it.
    if (redirect && /^http:\/\/127\.0\.0\.1:[0-9]{4,5}\/[A-Za-z0-9_-]{43}\?code=[A-Za-z0-9_-]{43}$/.test(redirect)) {
      handedOver = true;
      // On another device there is no codeaf at that address: the browser
      // stays here or comes back here, and the page says where to finish.
      window.setTimeout(elsewhere, 4000);
      window.location.assign(redirect);
    }
  }

  var handedOver = false;
  function elsewhere() {
    if (handedOver) {
      say("Finish on the computer that started this. Nothing was signed in on this device.", true);
    }
  }
  window.addEventListener("pageshow", function (event) {
    if (event.persisted) {
      elsewhere();
    }
  });

  // FEATURE DETECTION FIRST: a browser without WebAuthn is told so plainly
  // and pointed at the password, never left with a button that fails.
  var supported = !!(window.PublicKeyCredential && navigator.credentials &&
    navigator.credentials.create && navigator.credentials.get && window.isSecureContext);
  main.dataset.support = supported ? "yes" : "no";
  if (!supported) {
    if (form) {
      form.hidden = true;
    }
    say("This browser can't use passkeys. Go back to codeaf and use your password instead.", true);
    return;
  }

  // Where there is no built-in authenticator (most Linux machines), the
  // browser will offer a phone or a security key; say so before it does.
  if (PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable) {
    PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable().then(function (has) {
      var kind = document.getElementById("kind");
      if (!has && kind) {
        kind.hidden = false;
      }
    }, function () {});
  }

  function b64urlToBytes(s) {
    var pad = s.length % 4 === 0 ? "" : "====".slice(s.length % 4);
    var raw = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
    var out = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) {
      out[i] = raw.charCodeAt(i);
    }
    return out.buffer;
  }

  function bytesToB64url(buf) {
    var bytes = new Uint8Array(buf);
    var s = "";
    for (var i = 0; i < bytes.length; i++) {
      s += String.fromCharCode(bytes[i]);
    }
    return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function creationOptions(json) {
    if (PublicKeyCredential.parseCreationOptionsFromJSON) {
      return PublicKeyCredential.parseCreationOptionsFromJSON(json);
    }
    var o = Object.assign({}, json);
    o.challenge = b64urlToBytes(json.challenge);
    o.user = Object.assign({}, json.user, { id: b64urlToBytes(json.user.id) });
    o.excludeCredentials = (json.excludeCredentials || []).map(function (c) {
      return Object.assign({}, c, { id: b64urlToBytes(c.id) });
    });
    return o;
  }

  function requestOptions(json) {
    if (PublicKeyCredential.parseRequestOptionsFromJSON) {
      return PublicKeyCredential.parseRequestOptionsFromJSON(json);
    }
    var o = Object.assign({}, json);
    o.challenge = b64urlToBytes(json.challenge);
    o.allowCredentials = (json.allowCredentials || []).map(function (c) {
      return Object.assign({}, c, { id: b64urlToBytes(c.id) });
    });
    return o;
  }

  function credentialJSON(cred) {
    if (typeof cred.toJSON === "function") {
      try {
        return cred.toJSON();
      } catch (e) {
        // Fall through to the hand-built form.
      }
    }
    var r = cred.response;
    var out = {
      id: cred.id,
      rawId: bytesToB64url(cred.rawId),
      type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
      response: { clientDataJSON: bytesToB64url(r.clientDataJSON) }
    };
    if (r.attestationObject) {
      out.response.attestationObject = bytesToB64url(r.attestationObject);
      out.response.transports = r.getTransports ? r.getTransports() : [];
    } else {
      out.response.authenticatorData = bytesToB64url(r.authenticatorData);
      out.response.signature = bytesToB64url(r.signature);
      if (r.userHandle) {
        out.response.userHandle = bytesToB64url(r.userHandle);
      }
    }
    return out;
  }

  function post(step, body) {
    return fetch("/passkey/" + encodeURIComponent(flow) + "/" + step, {
      method: "POST",
      credentials: "omit",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { ok: res.ok, status: res.status, data: data };
      });
    });
  }

  function value(id) {
    var el = document.getElementById(id);
    return el ? el.value.trim() : "";
  }

  // A passkey made for an account the server then could not make is taken
  // back out of the person's keychain where the browser allows it.
  function forget(cred) {
    if (cred && PublicKeyCredential.signalUnknownCredential) {
      PublicKeyCredential.signalUnknownCredential({ rpId: host, credentialId: bytesToB64url(cred.rawId) }).catch(function () {});
    }
  }

  function ceremonyError(err) {
    if (err && err.name === "NotAllowedError") {
      return "Nothing was saved. Try again when you're ready.";
    }
    if (err && err.name === "InvalidStateError") {
      return "This device already has a passkey here.";
    }
    return "Your browser could not use a passkey here. Try again, or go back to codeaf and use your password.";
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    go.disabled = true;
    say("", false);
    var ask = {};
    if (document.getElementById("username")) {
      ask.username = value("username").toLowerCase();
    }
    if (document.getElementById("name")) {
      ask.name = value("name");
    }
    // One ceremony: ask, let the browser sign, hand back. An add by someone
    // who already has passkeys asks one of them first ("get"), then makes
    // the new one ("create") in the same visit.
    function ceremony() {
      var made = null;
      return post("options", ask).then(function (opt) {
        if (!opt.ok) {
          throw { words: opt.data.error || "Something went wrong. Try again." };
        }
        if (opt.data.kind === "get") {
          return navigator.credentials.get({ publicKey: requestOptions(opt.data.publicKey) });
        }
        return navigator.credentials.create({ publicKey: creationOptions(opt.data.publicKey) }).then(function (cred) {
          made = cred;
          return cred;
        });
      }).then(function (cred) {
        if (!cred) {
          throw { words: "Nothing was saved. Try again when you're ready." };
        }
        say("Checking…", false);
        return post("finish", { credential: credentialJSON(cred), name: value("label") });
      }).then(function (fin) {
        if (!fin.ok) {
          if (made && fin.data.reason !== "passkey_taken") {
            forget(made);
          }
          throw { words: fin.data.error || "Something went wrong. Try again." };
        }
        if (fin.data.state === "continue") {
          say("Now create the new passkey.", false);
          return ceremony();
        }
        finished(fin.data.message || "Finishing on the computer that started this…", fin.data.redirect);
      });
    }
    ceremony().catch(function (err) {
      go.disabled = false;
      say(err && err.words ? err.words : ceremonyError(err), true);
    });
  });
})();
