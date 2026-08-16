Build `pathmatch`, a path-pattern matcher, as a Python package in this
workspace. Start from an empty directory; there is no existing code.

Create a `pathmatch` package exporting `compile_pattern`, `matches`, `select`
and `PatternError`. How you split it across modules is yours. Standard library
only.

The patterns resemble ones you have seen before. **The specification below is
the only authority**: where it and a familiar tool disagree, this is what is
checked, and several of the rules were chosen precisely because the obvious
implementation gets them wrong.

## Paths

A path is a `/`-separated string with no leading slash: `main.py`,
`src/main.py`. A path ending in `/` is a **directory path**: `build/`, `src/`.
`/` is always a separator — it cannot be escaped into a literal and cannot
appear inside a character class.

## API

```python
def compile_pattern(pattern: str)      # -> an opaque compiled object, or None
def matches(pattern: str, path: str) -> bool
def select(patterns: list[str], paths: list[str]) -> list[str]
class PatternError(ValueError)
```

`compile_pattern` returns `None` for a line that is blank, whitespace-only or a
comment, and raises `PatternError` for a pattern it cannot parse — an
unterminated character class, a dangling backslash. A malformed pattern is
raised there, not later from inside a match. The object it returns is opaque;
nothing inspects its shape.

`matches` reports whether the pattern *applies* to the path. A leading `!` is
stripped before matching, so `matches("!a.py", "a.py")` is `True` — whether a
match selects or excludes is `select`'s business. A blank or comment pattern
matches nothing.

## Pattern syntax

- `?` matches exactly one character, never `/`.
- `*` matches zero or more characters, never `/`.
- `[abc]` matches one character from the set; `[a-f]` is a range. A class
  never matches `/`. A class is negated by `!` immediately after the opening
  bracket, and by nothing else — any other character there, including `^`, is
  an ordinary member of the set.
- Two or more `*` **inside** a segment behave as a single `*`. The sequence
  `**` has its segment-crossing meaning only when it is an entire segment, in
  which case it matches zero or more whole segments. A pattern that is exactly
  `**` matches every path.
- `\` makes the next character literal: `\*`, `\?`, `\[`, `\\`. A pattern
  ending in a lone `\` is an error.
- Matching is case-sensitive on every platform.

## Pattern lines

A line that is empty, whitespace-only, or begins with `#` compiles to `None`.
`\#` at the start is a literal `#`. Trailing spaces are stripped unless the
final space is escaped: `"a.py   "` is the pattern `a.py`, and `"a\ "` is `a`
followed by a space.

## Anchoring

A pattern containing a `/` anywhere other than a trailing one is **anchored**
and must match the path in full, first segment to last. A pattern with no `/`
is **unanchored** and matches if it matches any single segment of the path.

A pattern with a trailing `/` applies only to a directory path — that is, only
to a path that itself ends in `/`. It is otherwise unanchored.

## Selection

`select` decides each path independently and returns those chosen, in the order
`paths` gave them. A path appearing more than once in the input is decided once
and appears at most once in the output, at its first position.

Patterns are consulted in this order:

1. every anchored pattern, in the order they appear in the list;
2. then every unanchored pattern, in the order they appear in the list.

The first pattern that matches, under that ordering, settles the path and no
later pattern is consulted for it. A pattern beginning with an unescaped `!`
excludes; every other pattern includes. A path matched by no pattern is not
selected.

## Scale

`select` is called with a few hundred patterns against tens of thousands of
paths. It must be comfortable at that size, and `matches` must stay cheap when
a pattern contains several `**` segments.

## How this is graded

By a hidden suite you will not see, in fourteen small independent groups
covering the wildcards, character classes, escaping, comment and blank
handling, anchoring, the selection rules above, nested `**`, and the scale
requirement. Your score is the fraction of groups in which **every** test
passes.

A package that does not import scores zero. The groups are small, so partial
work scores partially — finish the rules you start rather than sketching all
fourteen. Write your own tests as you go; none of this is checkable by
inspection, and the places this specification is least like what you expect are
where an implementation that looks right is wrong.
