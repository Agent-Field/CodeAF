# If I lose every computer

## I lost all my devices — can I get my chats back

**No. There is no recovery.** Your chats are sealed under keys that only your computers hold. The relay has only ciphertext. Nobody, including the operator of the relay, can open it or hand you a key. There is no account, no email link and no reset.

If you still have one computer that holds your identity, pair a new one from it: `/pair`, or see "I got a new laptop". If you have **none**, the chats on the relay cannot be read by anybody. They stay there sealed until the relay is cleared.

Only two things can save you before that day:

- **An identity file.** `codeaf identity export` writes your identity under a passphrase, and `codeaf identity import` reads it on a new computer. It needs `CODEAF_CELLS=1` in this build. Keep the file and the passphrase in a safe place, apart from the computer.
- **One computer that you keep.** A second computer that is paired is a backup.

## Can I change my identity — identity rotate

**`codeaf identity rotate` is not built.** There is no way yet to make a new identity for all your computers. So revoking a stolen or lost computer cuts it off from the relay only. It does not make your chats unreadable to the person who has that computer. See "Devices and revoking" on the pairing page.

Until it is built, treat every chat on a lost computer as exposed, and do not pair new computers you do not trust.
