"""The T2 answer key, derived from the corpus rather than asserted.

Everything below is recomputed from the numbers as they appear in the corpus
files, and `selfcheck.py` re-reads those files and checks that the figures this
module starts from are the figures actually written down. That is the whole
point: both Phase A labs found ground-truth errors that would have read as
"the models are weak" (26 between them), and every one was a number someone had
typed twice.
"""

# From 01-service-catalog.md. (owner is None where the catalog says vacant.)
CATALOG = {
    "svc-auth":     {"owner": "team-identity",  "tier": 1, "queue": "rabbit-legacy", "msgs_per_day": 1_200_000},
    "svc-billing":  {"owner": "team-payments",  "tier": 1, "queue": "rabbit-legacy", "msgs_per_day":   400_000},
    "svc-catalog":  {"owner": "team-commerce",  "tier": 2, "queue": "kafka-shared",  "msgs_per_day": 2_500_000},
    "svc-notify":   {"owner": None,             "tier": 2, "queue": "rabbit-legacy", "msgs_per_day":   900_000},
    "svc-search":   {"owner": "team-discovery", "tier": 2, "queue": "kafka-shared",  "msgs_per_day": 5_000_000},
    "svc-media":    {"owner": None,             "tier": 3, "queue": "pulsar-edge",   "msgs_per_day":   150_000},
    "svc-reports":  {"owner": "team-analytics", "tier": 3, "queue": "rabbit-legacy", "msgs_per_day":    60_000},
    "svc-gateway":  {"owner": "team-platform",  "tier": 1, "queue": "kafka-shared",  "msgs_per_day": 8_000_000},
}

# ADR-0011 supersedes ADR-0007 in full and deprecates kafka-shared as well.
DEPRECATED_QUEUES = {"rabbit-legacy", "kafka-shared"}
CURRENT_QUEUE = "pulsar-edge"

# Customer-impact windows, exactly as each incident report defines them.
INCIDENTS = {
    # doc 02: "3 hours and 40 minutes", 02:41 -> 05:40 on 2026-01-14
    "02": {"service": "svc-auth",   "minutes": 3 * 60 + 40},
    # doc 03: 14:07 -> 15:52 on 2026-02-03
    "03": {"service": "svc-search", "minutes": (15 * 60 + 52) - (14 * 60 + 7)},
    # doc 04: 22:40 on 2026-02-27 -> 01:25 on 2026-02-28, across midnight
    "04": {"service": "svc-auth",   "minutes": (24 * 60 - (22 * 60 + 40)) + (1 * 60 + 25)},
}

# ROUND 2. doc 10 is the same 2026-02-27 certificate expiry re-filed by the
# other team three days later, with a differently-measured window. Rule 2 makes
# one incident of the two and gives it to the earlier filing (doc 04, filed
# 2026-02-28 03:10, against doc 10's 2026-03-02 09:20). It is therefore
# superseded, it contradicts doc 04, and it is NOT a fourth incident. A run
# that counts it reports 675 minutes instead of 490.
REFILINGS = {"10": {"of": "04", "claimed_minutes": 3 * 60 + 5}}

# 08-vendor-pricing-2026-03.md, effective 2026-03-01, supersedes 07.
#
# ROUND 2. kafka-shared moved to an account-level volume tier at renewal: the
# first 300M messages in the billing month at 4 cents/10k, everything above at
# 2 cents/10k, assessed on the combined volume of every service on the
# technology rather than per service. Round 1 was flat pricing throughout and
# arm A scored full marks on it.
PRICE_CENTS_PER_10K = {"rabbit-legacy": 9, "kafka-shared": 4, "pulsar-edge": 6}
TIERS = {
    "kafka-shared": {"threshold_messages": 300_000_000, "above_cents_per_10k": 2},
}
BILLING_DAYS = 30

SUPERSEDED_DOCS = ["05", "07", "10"]

CONTRADICTIONS = [
    # 03 says svc-search is on pulsar-edge; the catalog owns service metadata.
    {"claim_doc": "03", "authoritative_doc": "01", "field": "queue_client"},
    # 09 says the 2026-01-14 auth impact was ~90 minutes; the report owns it.
    {"claim_doc": "09", "authoritative_doc": "02", "field": "incident_duration_minutes"},
    # 09 says team-platform owns svc-notify; the catalog says vacant.
    {"claim_doc": "09", "authoritative_doc": "01", "field": "owner"},
    # 10 re-files the 2026-02-27 incident and puts its impact at 3h05 against
    # doc 04's 2h45; doc 04 is the earlier filing and owns the fact.
    {"claim_doc": "10", "authoritative_doc": "04", "field": "incident_duration_minutes"},
    # 07 prices rabbit-legacy at 12 cents/10k and kafka-shared at 7; 08 is the
    # schedule in force and says 9 and a tier.
    #
    # ROUND 2. This entry was missing from round 1's key, and the round-1
    # calibration run found it: arm A returned it, was marked wrong, and was
    # right. Under the brief's own definition -- a disagreement between two
    # documents about the same fact, decided by RULES.md -- doc 07 states a
    # value for a field doc 08 owns under rule 4, so it qualifies exactly as
    # 03 and 09 do. Round 1 had assumed "superseded" and "contradicting" were
    # exclusive; they are not, and `unit_price` was in the field vocabulary
    # with nothing pointing at it, which made the omission look deliberate.
    {"claim_doc": "07", "authoritative_doc": "08", "field": "unit_price"},
]

FIELD_VOCABULARY = ["incident_duration_minutes", "owner", "queue_client", "unit_price"]


def services_on_deprecated_queue():
    return sorted(s for s, m in CATALOG.items() if m["queue"] in DEPRECATED_QUEUES)


def total_incident_minutes():
    return sum(i["minutes"] for i in INCIDENTS.values())


def minutes_by_service():
    out = {}
    for i in INCIDENTS.values():
        out[i["service"]] = out.get(i["service"], 0) + i["minutes"]
    return out


def most_impacted_service():
    by = minutes_by_service()
    best = max(by.values())
    winners = sorted(s for s, m in by.items() if m == best)
    assert len(winners) == 1, f"most_impacted_service is not unique: {winners}"
    return winners[0]


def monthly_messages_by_queue():
    out = {}
    for meta in CATALOG.values():
        out[meta["queue"]] = out.get(meta["queue"], 0) + meta["msgs_per_day"] * BILLING_DAYS
    return out


def monthly_cost_usd_cents():
    """Billed per technology, because that is the level the tier is assessed at.

    Round 1 summed per service at a flat rate. With an account-level tier that
    is wrong even when every rate is right: splitting 465M messages across three
    services and pricing each one separately never reaches the threshold.
    """
    total = 0
    for queue, messages in monthly_messages_by_queue().items():
        tier = TIERS.get(queue)
        base_rate = PRICE_CENTS_PER_10K[queue]
        if tier is None:
            units, remainder = divmod(messages, 10_000)
            assert remainder == 0, "a volume that is not a whole number of 10k units"
            total += units * base_rate
            continue
        threshold = tier["threshold_messages"]
        below = min(messages, threshold)
        above = max(0, messages - threshold)
        for amount, rate in ((below, base_rate), (above, tier["above_cents_per_10k"])):
            units, remainder = divmod(amount, 10_000)
            assert remainder == 0, "a tier boundary that is not a whole 10k unit"
            total += units * rate
    return total


def unowned_services():
    return sorted(s for s, m in CATALOG.items() if m["owner"] is None)


def answer():
    return {
        "services_on_deprecated_queue": services_on_deprecated_queue(),
        "total_incident_minutes": total_incident_minutes(),
        "most_impacted_service": most_impacted_service(),
        "monthly_cost_usd_cents": monthly_cost_usd_cents(),
        "unowned_services": unowned_services(),
        "superseded_docs": sorted(SUPERSEDED_DOCS),
        "contradictions": CONTRADICTIONS,
    }


if __name__ == "__main__":
    import json
    print(json.dumps(answer(), indent=2))
