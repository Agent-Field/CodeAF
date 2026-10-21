# If I lose every computer

## I lost all my devices — can I get my chats back

**No. There is no recovery.** Your chats are sealed under keys that only your computers hold. The relay has only ciphertext. Nobody, including the operator of the relay, can open it or hand you a key. There is no account, no email link and no reset.

If you still have one computer that holds your identity, pair a new one from it: `/pair`, or see "I got a new laptop". If you have **none**, the chats on the relay cannot be read by anybody. They stay there sealed until the relay is cleared.

Only two things can save you before that day:

- **An identity file.** `codeaf identity export` writes your identity under a passphrase, and `codeaf identity import` reads it on a new computer. Keep the file and the passphrase in a safe place, apart from the computer.
- **One computer that you keep.** A second computer that is paired is a backup.

## Can I change my identity — identity rotate

If one trusted computer remains, **`codeaf identity rotate` works now**. It makes a replacement identity, seals your chats and saved keys under it, and retires the relay's old copy after the grace period. Pair the computers you keep again from that replacement. Read *Locking out a lost computer for good* for the steps and limits.

Rotation needs the old identity on a surviving computer. It cannot recover an identity when every copy, including any exported identity file and its passphrase, is lost. Revoking or rotating cannot take back chats and keys already downloaded by a lost computer; change exposed provider keys at their providers.
