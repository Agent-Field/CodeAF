#!/usr/bin/env python3
"""Monthly cost of the hosted directory, three ways, from counts measured with the real client.

Measured inputs (local `wrangler dev`, real client, three sessions; see STAGE-1H-DECISION.md section 8):
  s1  55 s   6 sealed turns  6 flushes  21 requests  R2 A 20  B 14  176,611 B up
  s2 110 s   7 sealed turns  2 flushes  17 requests  R2 A 16  B 15  141,824 B up
  s3 takeover: 90 object GETs, 122 R2 B, 61,364 B down, 20 heartbeats over 3 min
Prices (Cloudflare docs fetched 2026-09-29): see PRICE below.
Everything else is a stated assumption and a parameter.
"""
from dataclasses import dataclass, replace

PRICE = dict(
    workers_base=5.0, req_free=10e6, req_per_m=0.30, cpu_free_ms=30e6, cpu_per_m_ms=0.02,
    do_req_free=1e6, do_req_per_m=0.15, do_gbs_free=400e3, do_gbs_per_m=12.50,
    do_rows_free=50e6, do_rows_per_m=1.0, r2_a_free=1e6, r2_a_per_m=4.50, r2_b_free=10e6, r2_b_per_m=0.36,
    r2_gb_free=10, r2_gb_month=0.015, d1_rows_free=50e6, d1_rows_per_m=1.0,
)
over = lambda used, free, per_m: max(0.0, used - free) / 1e6 * per_m


@dataclass(frozen=True)
class Shape:
    """One held session (30 minutes) as the client produces it."""
    heartbeats: float = 180      # client today: every 10 s. Renew-on-publish lease, TTL 90 s: 30 (a keepalive each 30 s, and only while nothing was published)
    flushes: float = 60          # one frame put plus one publish each; 5 s flush interval, one per 30 s on average
    fixed_writes: float = 3      # create, device record, release (measured: s1)
    fixed_reads: float = 4       # the reads that come with them (measured: s1)
    takeover: float = 0.2        # share of sessions that end in a takeover
    take_gets: float = 90        # object GETs per takeover (measured: s3)
    take_dir: float = 8          # directory requests per takeover (measured: s3)
    take_b: float = 33           # extra R2 reads per takeover beyond its GETs (measured: s3 122 - 90 + snapshot)
    flush_bytes: float = 43_200  # measured mean frame: 388,592 B over 9 flushes
    do_ms_dir: float = 5         # assumed active ms per directory request in a Durable Object
    do_ms_put: float = 100       # assumed active ms per frame put in a Durable Object (body receive + R2 put)
    cpu_ms: float = 3            # assumed billed CPU per Worker request
    sessions: float = 8          # per identity per month


def requests(s):   # Worker invocations per session
    return s.heartbeats + 2 * s.flushes + s.fixed_writes + s.fixed_reads / 2 + s.takeover * (s.take_gets + s.take_dir)


def dir_writes(s):
    return s.heartbeats + s.flushes + s.fixed_writes


def storage(s, n_sessions, months=12):
    gb = n_sessions * s.flush_bytes * s.flushes * months / 1e9
    return over(gb * 1e6, PRICE['r2_gb_free'] * 1e6, PRICE['r2_gb_month'])   # (gb-free) * $/GB


def common(s, n_sessions):
    """Worker requests, CPU, frames in R2, storage: the same for every option."""
    req = requests(s) * n_sessions
    frames_a = s.flushes * n_sessions + s.takeover * n_sessions   # frames plus one snapshot write per takeover
    gets_b = s.takeover * s.take_gets * n_sessions
    return dict(
        workers=PRICE['workers_base'] + over(req, PRICE['req_free'], PRICE['req_per_m'])
        + over(req * s.cpu_ms, PRICE['cpu_free_ms'], PRICE['cpu_per_m_ms']),
        r2_a=frames_a, r2_b=gets_b, storage=storage(s, n_sessions),
    )


def option_a(s, n):   # Durable Object per identity holds the directory (SQLite) and sees every put
    c = common(s, n)
    reqs = (dir_writes(s) + s.flushes + s.fixed_reads / 2) * n            # dir ops plus puts route to the object
    gbs = ((dir_writes(s) * s.do_ms_dir + s.flushes * s.do_ms_put) / 1000 * 0.128) * n
    rows = 2 * dir_writes(s) * n
    c['do'] = over(reqs, PRICE['do_req_free'], PRICE['do_req_per_m']) + over(gbs, PRICE['do_gbs_free'], PRICE['do_gbs_per_m']) \
        + over(rows, PRICE['do_rows_free'], PRICE['do_rows_per_m'])
    return c


def option_b(s, n):   # D1 shard holds the directory; frames still go through the stateless Worker
    c = common(s, n)
    c['d1'] = over(2 * dir_writes(s) * n, PRICE['d1_rows_free'], PRICE['d1_rows_per_m'])
    return c


def option_c(s, n):   # R2 only: each directory write is one conditional put, each read one get
    c = common(s, n)
    c['r2_a'] += dir_writes(s) * n + s.takeover * n * 2       # + acquire writes at takeover
    c['r2_b'] += (s.heartbeats + s.flushes + s.fixed_reads) * n + s.takeover * n * (s.take_dir + s.take_b)
    return c


def total(c):
    return c['workers'] + c.get('do', 0) + c.get('d1', 0) + c['storage'] \
        + over(c['r2_a'], PRICE['r2_a_free'], PRICE['r2_a_per_m']) + over(c['r2_b'], PRICE['r2_b_free'], PRICE['r2_b_per_m'])


OPTIONS = {'a  Durable Object': option_a, 'b  D1': option_b, 'c  R2 only': option_c}
CLIENTS = {'client today (beat 10 s)': {}, 'lease renews on publish': dict(heartbeats=30)}


def table(shape, label):
    print(f'\n{label}')
    print(f'{"":26}{"identities":>11}{"active":>8}' + ''.join(f'{k:>20}' for k in OPTIONS))
    for cname, tweak in CLIENTS.items():
        for n in (10_000, 100_000):
            for active in (1.0, 0.25):
                s = replace(shape, **tweak)
                sessions = n * active * s.sessions
                row = [total(f(s, sessions)) for f in OPTIONS.values()]
                print(f'{cname:26}{n:>11,}{active:>8.0%}' + ''.join(f'{v:>20,.0f}' for v in row))


if __name__ == '__main__':
    table(Shape(takeover=1.0), 'UPPER BOUND: every identity active, 8 sessions of 30 min, a takeover in every session (demo shape), a flush every 30 s')
    table(Shape(), 'REALISTIC SHAPE: same activity, takeover in 20% of sessions')
    table(Shape(flushes=360, takeover=1.0), 'STRESS: a flush every 5 s for all 30 minutes (never idle), takeover in every session')
    table(Shape(flushes=12), 'QUIET: a flush every 2.5 minutes')


def breakeven_resident_s(shape, n_identities, active=1.0):
    """Seconds of billed Durable Object residency per session at which option a costs as much as option c."""
    n = n_identities * active * shape.sessions
    a, c = option_a(shape, n), option_c(shape, n)
    gap = total(c) - total(a)
    modelled = ((dir_writes(shape) * shape.do_ms_dir + shape.flushes * shape.do_ms_put) / 1000 * 0.128) * n
    need = max(PRICE['do_gbs_free'], modelled) + gap / PRICE['do_gbs_per_m'] * 1e6
    return (need - modelled) / n / 0.128
