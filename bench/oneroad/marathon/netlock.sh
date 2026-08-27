#!/usr/bin/env bash
# marathon/netlock.sh — the task's own egress allowlist, applied to a live
# container's network namespace from the host.
#
# Usage: netlock.sh apply <container>            install the rules once
#        netlock.sh watch <container> [seconds]  install, then keep re-resolving
#        netlock.sh show  <container>            print the rules that are live
#
# WHY THIS EXISTS. `task.toml` declares, for BOTH the agent and the verifier
# phase, `network_mode = "allowlist"` over five hosts, and `instruction.md` tells
# the agent in so many words that "only the package registries needed to fetch
# your crate dependencies are reachable; all other internet egress is blocked".
# Cells before this file ran on docker's default bridge with FULL egress, which
# made that sentence in the prompt false.
#
# WHY IT IS DONE FROM THE HOST, IN THE CONTAINER'S NETNS. Three mechanisms were
# available and two were rejected:
#
#   * an HTTP proxy on an `--internal` docker network — enforces perfectly, but
#     only for clients that honour `HTTPS_PROXY`. node's undici (pi) and
#     opencode's runtime do not by default, so the peer arms would simply lose
#     their model endpoint and die. A network policy that silently kills two of
#     the four arms is not a policy, it is a bias.
#   * iptables INSIDE the container with `--cap-add NET_ADMIN` — the image is
#     plain ubuntu:24.04 and ships no iptables, so this means `apt-get install`
#     into the image under test. The whole point of this runner is that the
#     dataset's image is the one measured: nothing derived, nothing installed.
#
# So the rules are written with the HOST's iptables into the CONTAINER's network
# namespace via `nsenter -t <pid> -n`. The container is untouched — no capability
# added, no package installed, no proxy variable in its environment — and every
# process in it, whatever language it is written in, is filtered by the kernel.
#
# WHAT AN IP ALLOWLIST CAN AND CANNOT SAY. `index.crates.io`, `static.crates.io`
# and `static.rust-lang.org` all resolve to the SAME Fastly address
# (151.101.138.137 as measured here). No address-based filter can allow the crate
# registries and refuse the toolchain server; separating them needs SNI
# inspection. That costs nothing here because `static.rust-lang.org` is on the
# benchmark's own allowlist anyway — and because cell.sh pins the agent's
# compiler to the image's toolchain regardless of what gets installed.
#
# THE RE-RESOLUTION LOOP, AND WHY IT ONLY EVER ADDS. The allowlisted names are
# CDN names whose answers rotate. The watcher re-resolves every interval and
# appends addresses it has not seen; it never removes one. A removal would open a
# window in which a live `cargo` connection is cut for no reason, and the set is
# small — six names over a ten-hour cell measured a handful of addresses each.
# The last rule is REJECT rather than DROP so a blocked connection fails in
# milliseconds and the agent's tool reports a refused connection instead of
# hanging until some timeout it cannot see.
set -uo pipefail

CMD="${1:?usage: netlock.sh apply|watch|show <container> [interval]}"
CONTAINER="${2:?usage: netlock.sh apply|watch|show <container> [interval]}"
INTERVAL="${3:-30}"

# The five hosts are task.toml's [agent].allowed_hosts verbatim. openrouter.ai is
# the model endpoint, which the benchmark's own agent phase also has to add
# ("Oddish adds only the selected model API endpoint to the agent phase").
NETLOCK_HOSTS="${NETLOCK_HOSTS:-index.crates.io static.crates.io crates.io github.com static.rust-lang.org openrouter.ai}"
NETLOCK_PORTS="${NETLOCK_PORTS:-443 80}"
# github.com ROUND-ROBINS ACROSS A BLOCK, and a single resolved address is not
# the host. Three lookups a minute apart here returned 140.82.112.3, .113.4 and
# .114.3; a cargo git dependency that resolves to an address the last refresh did
# not see would be refused for no reason the agent could understand. 140.82.112.0/20
# is GitHub's own git/codeload range (their meta API publishes it), so allowing
# the block widens nothing beyond the host task.toml already names. The Fastly
# and Cloudflare names in the list resolve to one or two stable addresses each
# and are allowed by address, because 151.101.0.0/16 is shared Fastly space that
# would let through half the internet.
NETLOCK_CIDRS="${NETLOCK_CIDRS:-140.82.112.0/20}"
CHAIN=ONEROAD_ALLOW
IPT=/usr/sbin/iptables
IPT6=/usr/sbin/ip6tables

log() { printf '[%s] netlock: %s\n' "$(date +%H:%M:%S)" "$*"; }

cpid() { docker inspect -f '{{.State.Pid}}' "$CONTAINER" 2>/dev/null | tr -d '\r'; }

# Every iptables call goes through here: host root, container netns, host binary.
ns() {
  local pid; pid="$(cpid)"
  [ -n "$pid" ] && [ "$pid" != "0" ] || return 9
  sudo -n nsenter -t "$pid" -n "$@"
}

resolved() {  # every A record for the allowlist, resolved AS THE CONTAINER sees it
  local h
  for h in $NETLOCK_HOSTS; do
    docker exec "$CONTAINER" getent ahostsv4 "$h" 2>/dev/null | awk '{print $1}'
    getent ahostsv4 "$h" 2>/dev/null | awk '{print $1}'
  done | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | sort -u
}

nameservers() {
  docker exec "$CONTAINER" sh -c 'grep "^nameserver" /etc/resolv.conf 2>/dev/null' \
    | awk '{print $2}' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | sort -u
}

allow_ip() {  # idempotent: an address already in the chain is not added twice
  local ip="$1" port
  for port in $NETLOCK_PORTS; do
    ns "$IPT" -C "$CHAIN" -d "$ip" -p tcp --dport "$port" -j ACCEPT 2>/dev/null && continue
    ns "$IPT" -A "$CHAIN" -d "$ip" -p tcp --dport "$port" -j ACCEPT 2>/dev/null || return 1
  done
}

apply() {
  local pid ns_ip ip n=0
  pid="$(cpid)"
  [ -n "$pid" ] && [ "$pid" != "0" ] || { log "container $CONTAINER is not running"; return 1; }
  sudo -n true 2>/dev/null || { log "no passwordless sudo — cannot write the netns firewall"; return 1; }

  ns "$IPT" -N "$CHAIN" 2>/dev/null
  ns "$IPT" -F "$CHAIN" || { log "iptables unusable in netns of $CONTAINER"; return 1; }
  ns "$IPT" -F OUTPUT

  ns "$IPT" -A OUTPUT -o lo -j ACCEPT
  ns "$IPT" -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  for ns_ip in $(nameservers); do
    ns "$IPT" -A OUTPUT -d "$ns_ip" -p udp --dport 53 -j ACCEPT
    ns "$IPT" -A OUTPUT -d "$ns_ip" -p tcp --dport 53 -j ACCEPT
  done
  ns "$IPT" -A OUTPUT -j "$CHAIN"
  ns "$IPT" -A OUTPUT -p tcp -j REJECT --reject-with tcp-reset
  ns "$IPT" -A OUTPUT -j REJECT --reject-with icmp-port-unreachable

  for ip in $NETLOCK_CIDRS $(resolved); do allow_ip "$ip" && n=$((n + 1)); done

  # IPv6 is refused outright. Nothing in the allowlist needs it here, and an
  # unfiltered v6 path beside a filtered v4 one is not an allowlist at all.
  if ns "$IPT6" -L -n >/dev/null 2>&1; then
    ns "$IPT6" -F OUTPUT
    ns "$IPT6" -A OUTPUT -o lo -j ACCEPT
    ns "$IPT6" -A OUTPUT -j REJECT
  fi
  log "$CONTAINER locked: $n address(es) for [$NETLOCK_HOSTS] on ports [$NETLOCK_PORTS], everything else REJECTed"
}

case "$CMD" in
  apply) apply ;;
  show)  ns "$IPT" -L OUTPUT -n --line-numbers; ns "$IPT" -L "$CHAIN" -n ;;
  watch)
    # A `watch` started by cell.sh right after its own `apply` must NOT flush and
    # rebuild the chain: between the flush and the last REJECT the container is
    # briefly open, and there is no reason to open it. So the lock is only
    # installed here if it is not already there.
    ns "$IPT" -C OUTPUT -p tcp -j REJECT --reject-with tcp-reset 2>/dev/null || apply || exit 1
    while :; do
      sleep "$INTERVAL"
      [ -n "$(cpid)" ] || exit 0
      # A container that lost its rules (restart, someone's flush) is re-locked
      # rather than left open: the check is the presence of the final REJECT.
      if ! ns "$IPT" -C OUTPUT -p tcp -j REJECT --reject-with tcp-reset 2>/dev/null; then
        log "rules missing on $CONTAINER — reapplying"; apply || exit 1; continue
      fi
      for ip in $NETLOCK_CIDRS $(resolved); do allow_ip "$ip"; done
    done ;;
  *) echo "usage: netlock.sh apply|watch|show <container> [interval]" >&2; exit 2 ;;
esac
