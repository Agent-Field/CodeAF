"""The nouns: workers, shifts, and the assignment that pairs them."""

from dataclasses import dataclass, field

from .interval import Interval


@dataclass(frozen=True)
class Worker:
    id: str
    name: str
    # Pay in whole cents per hour. Money is never a float in this package.
    rate_cents_per_hour: int
    skills: frozenset = field(default_factory=frozenset)

    @staticmethod
    def from_dict(raw):
        return Worker(
            id=raw["id"],
            name=raw["name"],
            rate_cents_per_hour=int(raw["rate_cents_per_hour"]),
            skills=frozenset(raw.get("skills", [])),
        )


@dataclass(frozen=True)
class Shift:
    id: str
    date: str            # ISO date, "2026-03-04"
    window: Interval
    required_skill: str

    @staticmethod
    def from_dict(raw):
        return Shift(
            id=raw["id"],
            date=raw["date"],
            window=Interval(int(raw["start_minute"]), int(raw["end_minute"])),
            required_skill=raw["required_skill"],
        )


@dataclass(frozen=True)
class Assignment:
    shift_id: str
    worker_id: str


class ShiftPlanError(Exception):
    """Raised when input cannot be understood. Never swallowed."""
