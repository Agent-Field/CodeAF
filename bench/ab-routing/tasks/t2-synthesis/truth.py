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

# 08-vendor-pricing-2026-03.md, effective 2026-03-01, supersedes 07.
PRICE_CENTS_PER_10K = {"rabbit-legacy": 9, "kafka-shared": 4, "pulsar-edge": 6}
BILLING_DAYS = 30

SUPERSEDED_DOCS = ["05", "07"]

CONTRADICTIONS = [
    # 03 says svc-search is on pulsar-edge; the catalog owns service metadata.
    {"claim_doc": "03", "authoritative_doc": "01", "field": "queue_client"},
    # 09 says the 2026-01-14 auth impact was ~90 minutes; the report owns it.
    {"claim_doc": "09", "authoritative_doc": "02", "field": "incident_duration_minutes"},
    # 09 says team-platform owns svc-notify; the catalog says vacant.
    {"claim_doc": "09", "authoritative_doc": "01", "field": "owner"},
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


def monthly_cost_usd_cents():
    total = 0
    for meta in CATALOG.values():
        units, remainder = divmod(meta["msgs_per_day"] * BILLING_DAYS, 10_000)
        assert remainder == 0, "a volume that is not a whole number of 10k units"
        total += units * PRICE_CENTS_PER_10K[meta["queue"]]
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
