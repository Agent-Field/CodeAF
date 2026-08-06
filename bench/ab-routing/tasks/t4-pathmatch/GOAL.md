Build `pathmatch`, a path-pattern matcher, as a Python package in this
workspace. Start from an empty directory; there is no existing code.

Create a `pathmatch` package exporting `compile_pattern`, `matches`, `select`
and `PatternError`. How you split it across modules is yours. Standard library
only.

The patterns look like `.gitignore` patterns and **the rules below are not
`.gitignore`'s.** Four of them differ deliberately. Implement what is written
here, not what the familiar tool does; every rule below is checked.

## Paths

A path is a `/`-separated string with no leading slash: `main.py`,
`src/main.py`. A path that ends in `/` is a **directory path**: `build/`,
`src/`. `/` is always a separator — it can never be escaped into a literal, and
it can never appear inside a character class.

## API

```python
def compile_pattern(pattern: str)      # -> an opaque compiled object, or None
def matches(pattern: str, path: str) -> bool
def select(patterns: list[str], paths: list[str]) -> list[str]
class PatternError(ValueError)
```

- **`compile_pattern`** returns `None` for a line that is blank, whitespace-only
  or a comment. It raises `PatternError` for a pattern it cannot parse — an
  unterminated character class, a dangling backslash. **A malformed pattern is
  raised here, not later from inside a match.** The returned object is opaque;
  nothing inspects its shape.
- **`matches`** reports whether the pattern *applies* to the path. A leading `!`
  is stripped before matching, so `matches("!a.py", "a.py")` is `True` — whether
  a match selects or excludes is `select`'s business, not this function's. A
  blank or comment pattern matches nothing.
- **`select`** returns the chosen paths. See "precedence" below.

## Matching rules

**Wildcards**, none of which ever match `/`:

- `?` — exactly one character.
- `*` — zero or more characters, within a single segment.
- `[abc]` — one character from the set. `[a-f]` is a range.
- A run of two or more `*` **inside** a segment (`a**b`) is just `*`. `**` means
  "cross segments" only when it is an entire segment.

**`**` as a whole segment** matches zero or more whole segments, so
`src/**/main.py` matches `src/main.py`, `src/a/main.py` and `src/a/b/c/main.py`.
A pattern that is exactly `**` matches every path.

**Escaping.** `\` makes the next character literal: `\*`, `\?`, `\[`, `\\`. A
pattern ending in a lone `\` is an error.

**Comments and blanks.** A line that is empty, whitespace-only, or begins with
`#` compiles to `None`. `\#` at the start is a literal `#`. **Trailing spaces
are stripped** unless the final space is escaped — `"a.py   "` is the pattern
`a.py`, and `"a\ "` is the two-character pattern `a` followed by a space.

**Anchoring.** If the pattern contains a `/` anywhere other than a trailing one,
it is **anchored**: it must match the whole path from the first segment to the
last. Otherwise it is **unanchored**: it matches if it matches *any single
segment* of the path. So `src/a` does not match `src/a/b.py`, but `main.py`
matches `src/pkg/main.py`.

**Case** is always significant, on every platform.

## The four divergences — read these twice

1. **`[!…]` negates a character class. `[^…]` does not.** In `[^abc]` the caret
   is an ordinary member of the set, so `[^abc]` matches `^`, `a`, `b` or `c`
   and nothing else. Only `!` immediately after `[` negates.

2. **A trailing `/` means the *path* must be a directory path.** `build/`
   matches `build/` and does not match `build`, and — unlike the familiar tool —
   it does **not** match `build/out.o`. It is still unanchored, so it matches
   `a/build/`.

3. **The first matching pattern wins.** `select` walks the pattern list in
   order; the first pattern that matches a path decides it, and later patterns
   are not consulted for that path. A pattern starting with an unescaped `!` is
   a negation and excludes; any other pattern includes. A path no pattern
   matches is not selected. So `select(["*.py", "!keep.py"], ["keep.py"])`
   returns `["keep.py"]` — the `!` never gets a say.

4. **`select` returns paths in the order they were given**, not in pattern
   order, and **de-duplicates**: a path appearing twice in the input appears at
   most once in the output, at its first position, and is decided once.

## How this is graded

By a hidden suite you will not see, in twelve small independent groups: the four
wildcard behaviours, character classes, escaping, anchoring, compile-time
handling of blanks/comments/errors, and one group for each of the four
divergences above. Your score is the fraction of groups in which **every** test
passes. A package that does not import scores zero.

The groups are deliberately small, so partial work scores partially — finish the
rules you start rather than sketching all twelve. Write your own tests as you
go; they are not graded and will not be read, but none of this is checkable by
inspection, and the divergences are exactly where an implementation that "looks
right" is wrong.
