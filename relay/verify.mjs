#!/usr/bin/env node
// Verifies the detached Ed25519 signature over a Model Pool index document.
//
//   node relay/verify.mjs index.json index.json.sig <public-key-base64>
//
// The signature is standard base64; whitespace around it is ignored. Exit 0
// with no output when it verifies over the exact bytes of the document file.
// Exit 1, naming the failure, when the signature does not verify, when an
// argument is missing, or when a file cannot be read.

import { readFileSync } from 'node:fs';

const [docPath, sigPath, publicB64] = process.argv.slice(2);
if (!docPath || !sigPath || !publicB64) {
  console.error('usage: node relay/verify.mjs index.json index.json.sig <public-key-base64>');
  process.exit(1);
}

let docBytes, signature;
try {
  docBytes = readFileSync(docPath);
  signature = Buffer.from(readFileSync(sigPath, 'utf8').trim(), 'base64');
} catch (err) {
  console.error(`cannot read input: ${err.message}`);
  process.exit(1);
}

let ok;
try {
  const jwk = {
    kty: 'OKP',
    crv: 'Ed25519',
    x: Buffer.from(publicB64, 'base64').toString('base64url'),
  };
  const key = await globalThis.crypto.subtle.importKey('jwk', jwk, { name: 'Ed25519' }, false, ['verify']);
  ok = await globalThis.crypto.subtle.verify({ name: 'Ed25519' }, key, signature, docBytes);
} catch (err) {
  console.error(`cannot verify: ${err.message}`);
  process.exit(1);
}

if (!ok) {
  console.error('signature does not verify');
  process.exit(1);
}
