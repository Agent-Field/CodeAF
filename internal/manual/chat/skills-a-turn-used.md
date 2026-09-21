# Which skills did it use — and did it use my skill

When a message carries skills, one dim line under your message names them:

```
skills carried: linter, release-check
```

That is the list of skills codeaf put into that turn for the model to read while
answering your message. A skill you attached comes first; skills your words name or
match can follow it. If your skill's name is on the line, that turn carried it. The
line reports the names that actually resolved from the active skill shelf, rather
than names guessed from the sentence on screen.

## Why is that line under my message

The skills belong to the message that selected them, not to the answer as a whole.
Two messages in the same conversation can carry different skills, so the line sits
under the message it describes. It is a quiet receipt, not a warning, a provider
retry, or something you need to act on.

The line appears only for a non-empty list. If there is no line, codeaf does not say
that the turn used no skills: an older remote peer that does not send skill details
looks the same as a turn with no list, so absence means **unknown**, not **none**.
An empty list is treated the same way and draws no skills sentence.
