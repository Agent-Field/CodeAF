"""pathmatch reference implementation. Never shipped to an agent.

t4 exists because t1 and t3 both sat at 0/3 in both arms: a suite whose hard
tasks are failed identically by everything can detect harm and nothing else.
This task is built on the axis arm A's failures actually identified — leaf-level
conformance to a spec stated once in prose — but with twelve small independent
rules instead of three large ones, so partial credit is fine-grained and the
gradient is climbable rather than a cliff.

The rules deliberately diverge from gitignore in four places (first-match-wins,
`[^…]` as a literal caret, directory-only meaning the *path* ends in a slash,
and anchoring on any `/`). A submission that pattern-matches its way to
gitignore scores most of the easy groups and loses every hard one, which is
exactly the separation this task is for.
"""

import re

__all__ = ["compile_pattern", "matches", "select", "PatternError"]


class PatternError(ValueError):
    """Raised for a pattern that cannot be understood."""


class _Pattern:
    __slots__ = ("negated", "dir_only", "anchored", "segments", "regexes", "source")

    def __init__(self, negated, dir_only, anchored, segments, source):
        self.negated = negated
        self.dir_only = dir_only
        self.anchored = anchored
        self.segments = segments
        self.source = source
        # Compiled here rather than lazily, so a malformed class or a dangling
        # backslash is raised by compile_pattern where a caller can act on it,
        # instead of surfacing much later from inside a match.
        self.regexes = [None if seg == "**" else _segment_regex(seg)
                        for seg in segments]


def _strip_trailing_spaces(text):
    """Trailing spaces go, unless the last one is escaped.

    Walking back over the escapes is the only way to tell ``a\\ `` (a literal
    trailing space) from ``a `` (an accident), because whether a space is
    escaped depends on the parity of the backslashes before it.
    """
    end = len(text)
    while end > 0 and text[end - 1] == " ":
        backslashes = 0
        index = end - 2
        while index >= 0 and text[index] == "\\":
            backslashes += 1
            index -= 1
        if backslashes % 2 == 1:
            break
        end -= 1
    return text[:end]


def compile_pattern(pattern):
    """Compile one pattern line. Returns None for a blank or comment line."""
    if not isinstance(pattern, str):
        raise PatternError("pattern must be a string")
    text = _strip_trailing_spaces(pattern)
    if text == "" or text.startswith("#"):
        return None
    if text.startswith("\\#"):
        text = text[1:]

    negated = False
    if text.startswith("!"):
        negated = True
        text = text[1:]
    elif text.startswith("\\!"):
        text = text[1:]
    if text == "":
        raise PatternError("pattern is empty after its prefix")

    dir_only = text.endswith("/")
    if dir_only:
        text = text[:-1]
    if text == "":
        raise PatternError("pattern is only a slash")

    anchored = "/" in text
    segments = text.split("/")
    if any(segment == "" for segment in segments):
        raise PatternError(f"empty path segment in {pattern!r}")
    return _Pattern(negated, dir_only, anchored, segments, pattern)


def _segment_regex(segment):
    """One pattern segment as an anchored regex over a single path segment."""
    out = ["(?s)\\A"]
    index = 0
    length = len(segment)
    while index < length:
        char = segment[index]
        if char == "\\":
            if index + 1 >= length:
                raise PatternError("pattern ends with a dangling backslash")
            out.append(re.escape(segment[index + 1]))
            index += 2
            continue
        if char == "*":
            # A run of stars inside a segment is one star: `**` only means
            # "cross segments" when it is the whole segment, and that case is
            # handled by the segment walker rather than here.
            while index < length and segment[index] == "*":
                index += 1
            out.append("[^/]*")
            continue
        if char == "?":
            out.append("[^/]")
            index += 1
            continue
        if char == "[":
            body, index = _class_body(segment, index)
            out.append(body)
            continue
        out.append(re.escape(char))
        index += 1
    out.append("\\Z")
    return re.compile("".join(out))


def _class_body(segment, index):
    """Translate a `[...]` class. `[!…]` negates; `[^…]` does not."""
    index += 1  # past '['
    negate = False
    if index < len(segment) and segment[index] == "!":
        negate = True
        index += 1
    pieces = []
    first = True
    while True:
        if index >= len(segment):
            raise PatternError("unterminated character class")
        char = segment[index]
        if char == "]" and not first:
            index += 1
            break
        first = False
        if char == "\\":
            if index + 1 >= len(segment):
                raise PatternError("dangling backslash in character class")
            pieces.append(re.escape(segment[index + 1]))
            index += 2
            continue
        if (char != "]" and index + 2 < len(segment)
                and segment[index + 1] == "-" and segment[index + 2] != "]"):
            pieces.append(f"{re.escape(char)}-{re.escape(segment[index + 2])}")
            index += 3
            continue
        pieces.append(re.escape(char))
        index += 1
    if not pieces:
        raise PatternError("empty character class")
    # `/` is never matched by a class, negated or not.
    return f"[{'^/' if negate else ''}{''.join(pieces)}]", index


def _split_path(path):
    if not isinstance(path, str):
        raise PatternError("path must be a string")
    is_dir = path.endswith("/")
    body = path[:-1] if is_dir else path
    segments = [s for s in body.split("/") if s != ""]
    return segments, is_dir


def _match_segments(pattern_segments, path_segments, regexes):
    """Walk both lists, letting a whole-segment `**` absorb zero or more."""
    if not pattern_segments:
        return not path_segments
    head = pattern_segments[0]
    if head == "**":
        rest, rest_regexes = pattern_segments[1:], regexes[1:]
        if not rest:
            return True
        for cut in range(len(path_segments) + 1):
            if _match_segments(rest, path_segments[cut:], rest_regexes):
                return True
        return False
    if not path_segments:
        return False
    if not regexes[0].match(path_segments[0]):
        return False
    return _match_segments(pattern_segments[1:], path_segments[1:], regexes[1:])


def _matches_compiled(compiled, path):
    segments, is_dir = _split_path(path)
    if compiled.dir_only and not is_dir:
        return False
    if compiled.anchored:
        return _match_segments(compiled.segments, segments, compiled.regexes)
    if compiled.segments == ["**"]:
        return True
    regex = compiled.regexes[0]
    return any(regex.match(segment) for segment in segments)


def matches(pattern, path):
    """True if `pattern` matches `path`. A blank or comment pattern matches
    nothing, and a negation is reported on its own terms — `matches` answers
    "does this pattern apply", not "is this path selected"."""
    compiled = compile_pattern(pattern)
    if compiled is None:
        return False
    return _matches_compiled(compiled, path)


def select(patterns, paths):
    """Paths chosen by the pattern list, in the order `paths` gave them.

    Two ordering rules stack, and the second is the one a single pass over the
    list gets wrong: every anchored pattern is consulted before any unanchored
    one, and only within a group does list order decide. The first pattern to
    match under that ordering settles the path.
    """
    compiled = []
    for pattern in patterns:
        item = compile_pattern(pattern)
        if item is not None:
            compiled.append(item)
    # Stable, so list order survives inside each group.
    compiled.sort(key=lambda item: 0 if item.anchored else 1)

    # Duplicates collapse to their first appearance before anything is decided,
    # so a repeated path cannot appear twice in the output whatever it matches.
    ordered = []
    seen = set()
    for path in paths:
        if path not in seen:
            seen.add(path)
            ordered.append(path)

    out = []
    for path in ordered:
        for item in compiled:
            if _matches_compiled(item, path):
                if not item.negated:
                    out.append(path)
                break
    return out
