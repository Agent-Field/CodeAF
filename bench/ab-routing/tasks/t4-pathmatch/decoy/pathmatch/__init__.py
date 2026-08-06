"""A competent gitignore-shaped implementation. Used only by the selfcheck.

This is the answer a model gets by recognising the shape of the problem and
reaching for what it already knows, rather than by reading the four divergences.
It is deliberately *good*: the wildcards, classes, escaping and anchoring are all
correct, so it should pass the eight baseline groups and fail the four
divergence groups.

That is the check. If this decoy scored 12/12 the divergences would not be
divergent; if it scored 0/12 the task would be measuring something other than
what it claims to. The selfcheck asserts 8 and 4 exactly.
"""

import re

__all__ = ["compile_pattern", "matches", "select", "PatternError"]


class PatternError(ValueError):
    pass


class _Pattern:
    __slots__ = ("negated", "dir_only", "anchored", "segments", "regexes", "source")

    def __init__(self, negated, dir_only, anchored, segments, source):
        self.negated = negated
        self.dir_only = dir_only
        self.anchored = anchored
        self.segments = segments
        self.source = source
        self.regexes = [None if s == "**" else _segment_regex(s) for s in segments]


def _strip_trailing_spaces(text):
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
    if not isinstance(pattern, str):
        raise PatternError("pattern must be a string")
    text = _strip_trailing_spaces(pattern)
    if text == "" or text.startswith("#"):
        return None
    if text.startswith("\\#"):
        text = text[1:]
    negated = False
    if text.startswith("!"):
        negated, text = True, text[1:]
    elif text.startswith("\\!"):
        text = text[1:]
    if text == "":
        raise PatternError("empty after prefix")
    dir_only = text.endswith("/")
    if dir_only:
        text = text[:-1]
    if text == "":
        raise PatternError("only a slash")
    anchored = "/" in text
    segments = text.split("/")
    if any(s == "" for s in segments):
        raise PatternError("empty segment")
    return _Pattern(negated, dir_only, anchored, segments, pattern)


def _segment_regex(segment):
    out = ["(?s)\\A"]
    index, length = 0, len(segment)
    while index < length:
        char = segment[index]
        if char == "\\":
            if index + 1 >= length:
                raise PatternError("dangling backslash")
            out.append(re.escape(segment[index + 1]))
            index += 2
            continue
        if char == "*":
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
    index += 1
    negate = False
    # DIVERGENCE 1 missed: treats `^` as a negation, the way every other glob does.
    if index < len(segment) and segment[index] in "!^":
        negate = True
        index += 1
    pieces, first = [], True
    while True:
        if index >= len(segment):
            raise PatternError("unterminated class")
        char = segment[index]
        if char == "]" and not first:
            index += 1
            break
        first = False
        if char == "\\":
            if index + 1 >= len(segment):
                raise PatternError("dangling backslash in class")
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
        raise PatternError("empty class")
    return f"[{'^/' if negate else ''}{''.join(pieces)}]", index


def _split_path(path):
    is_dir = path.endswith("/")
    body = path[:-1] if is_dir else path
    return [s for s in body.split("/") if s != ""], is_dir


def _match_segments(pattern_segments, path_segments, regexes):
    if not pattern_segments:
        return not path_segments
    if pattern_segments[0] == "**":
        rest, rest_regexes = pattern_segments[1:], regexes[1:]
        if not rest:
            return True
        return any(_match_segments(rest, path_segments[cut:], rest_regexes)
                   for cut in range(len(path_segments) + 1))
    if not path_segments or not regexes[0].match(path_segments[0]):
        return False
    return _match_segments(pattern_segments[1:], path_segments[1:], regexes[1:])


def _matches_compiled(compiled, path):
    segments, is_dir = _split_path(path)
    # DIVERGENCE 2 missed: gitignore's reading — a directory pattern takes
    # everything underneath it, so `build/` matches `build/out.o`.
    if compiled.dir_only and not is_dir:
        if compiled.anchored:
            head = compiled.segments
            return (len(segments) > len(head)
                    and _match_segments(head, segments[:len(head)], compiled.regexes))
        return any(compiled.regexes[0].match(s) for s in segments[:-1])
    if compiled.anchored:
        return _match_segments(compiled.segments, segments, compiled.regexes)
    if compiled.segments == ["**"]:
        return True
    return any(compiled.regexes[0].match(s) for s in segments)


def matches(pattern, path):
    compiled = compile_pattern(pattern)
    return False if compiled is None else _matches_compiled(compiled, path)


def select(patterns, paths):
    # DIVERGENCE 5 missed: no specificity ordering, just the list as written.
    compiled = [c for c in (compile_pattern(p) for p in patterns) if c is not None]
    out = []
    for path in paths:
        # DIVERGENCE 3 missed: last match wins, as gitignore does it.
        chosen = False
        for item in compiled:
            if _matches_compiled(item, path):
                chosen = not item.negated
        # DIVERGENCE 4 missed: no de-duplication.
        if chosen:
            out.append(path)
    return out
