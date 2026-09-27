"""Duration parsing and humanizing helpers."""

import math
import re

_UNITS = (
    ("d", 86400),
    ("h", 3600),
    ("m", 60),
    ("s", 1),
)

# One or more <number><unit> components, units lowercase d/h/m/s, with
# optional whitespace allowed around each component.
_COMPONENT = re.compile(r"\s*(\d+)([dhms])\s*")
_FULL = re.compile(r"(?:\s*(\d+)([dhms])\s*)+")


def parse(s):
    """Parse a duration string such as ``"1h30m"`` into whole seconds.

    Components may appear in any order and may repeat; their values sum.
    Malformed input raises :class:`ValueError`.
    """
    if not isinstance(s, str):
        raise ValueError("duration must be a string")
    if _FULL.fullmatch(s) is None:
        raise ValueError("malformed duration: {!r}".format(s))
    total = 0
    for number, unit in _COMPONENT.findall(s):
        total += int(number) * dict(_UNITS)[unit]
    return total


def humanize(n):
    """Render a non-negative number of seconds in descending unit order."""
    if isinstance(n, float):
        n = math.floor(n)
    if not isinstance(n, (int, float)):
        raise ValueError("seconds must be a number")
    if n < 0:
        raise ValueError("cannot humanize a negative duration")
    if n == 0:
        return "0s"
    parts = []
    remaining = int(n)
    for unit, seconds in _UNITS:
        value, remaining = divmod(remaining, seconds)
        if value:
            parts.append("{}{}".format(value, unit))
    return "".join(parts)
