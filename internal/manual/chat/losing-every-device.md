# If I lose every computer

## I lost all my devices — can I get my chats back

**No. There is no recovery.** Your chats are sealed under keys that only your computers hold. If storage was enabled and uploads finished, the relay has only ciphertext. With hosted storage off, your new chats never reached it. Nobody, including the operator of the relay, can open it or hand you a key. There is no account, no email link and no reset.

If you still have one computer that holds your identity, pair a new one from it: `/pair`, or see "I got a new laptop". If you have **none**, the chats on the relay cannot be read by anybody. They stay there sealed until the relay is cleared.

Only two things can save you before that day:

- **An identity file.** `codeaf identity export` writes your identity under a passphrase, and `codeaf identity import` reads it on a new computer. Keep the file and the passphrase in a safe place, apart from the computer.
- **One computer that you keep.** A second computer is a backup only for content that actually reached it; pairing alone makes no chat backup while hosted storage is off.

## Can I change my identity — identity rotate

If one trusted computer remains, **`codeaf identity rotate` works now**. It makes a replacement identity and reseals saved keys locally. With hosted sync storage off, it finishes with no resealing uploads and chats stay on your computers; with storage on, it reseals and uploads chats and saved keys. It retires the relay's old copy after the grace period. Pair the computers you keep again from that replacement. Read *Locking out a lost computer for good* for the steps and limits.

Rotation needs the old identity on a surviving computer. It cannot recover an identity when every copy, including any exported identity file and its passphrase, is lost. Revoking or rotating cannot take back chats and keys already downloaded by a lost computer; change exposed provider keys at their providers.

## Why pairing alone is not a backup — sync storage and completed uploads

Hosted sync storage is off by default while its privacy terms are being finished. Pairing alone does not enable it: new chats, files, saved keys, preferences, shared memories, teams and task copies wait. Pairing, device presence and live `--at` still work. After pairing, press enter on **Settings → Devices → sync storage** to ask for approval. A self-hosted sync service with `--store` needs no request. Approval cannot recover turns that never left a lost computer. Keep a surviving computer with the content, or an exported identity and an accessible encrypted copy.
