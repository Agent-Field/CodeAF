"""Extract wire shapes (types, enums, methods, examples) from the workstreams wire contract.

usage: python3 -I extract.py <contract markdown> <out-dir>
Writes contract.json (index, enums, errors, types) and one <method>.json per method/event
with its examples. No prose is copied.
"""
import json, os, re, sys

src, out = sys.argv[1], sys.argv[2]
lines = open(src, encoding="utf-8").read().split("\n")
end = next(i for i, l in enumerate(lines) if l.startswith("## Page document format"))
lines = lines[:end]

TICK = re.compile(r"`([^`]*)`")

def table_at(i):
    """rows of the markdown table starting at or after line i (skip blank lines)."""
    while i < len(lines) and not lines[i].startswith("|"):
        i += 1
    rows = []
    while i < len(lines) and lines[i].startswith("|"):
        cells = [c.strip() for c in lines[i].strip().strip("|").split("|")]
        rows.append(cells)
        i += 1
    return rows[2:]  # drop header + separator

def fields(rows):
    out = {}
    for cells in rows:
        if cells[0] in ("—", "-"):
            continue
        name = TICK.fullmatch(cells[0]).group(1)
        typ = TICK.fullmatch(cells[1]).group(1)
        req = cells[2]
        assert req in ("yes", "no"), cells
        out[name] = {"type": typ, "required": req == "yes"}
    return out

types, enums, errors, methods, events = {}, {}, [], [], []
method_info = {}
examples = {}
section = None  # H2
h3 = None
i = 0
while i < len(lines):
    l = lines[i]
    if l.startswith("## "):
        section, h3 = l[3:].strip(), None
    elif l.startswith("### "):
        h3 = TICK.search(l).group(1)
        if section == "Shared shapes":
            types[h3] = fields(table_at(i + 1))
        elif section == "Methods":
            methods.append(h3)
            method_info[h3] = {"method": h3, "errors": []}
    elif section == "Shared shapes" and l.startswith("| type | words |"):
        for cells in table_at(i):
            enums[TICK.fullmatch(cells[0]).group(1)] = TICK.findall(cells[1])
    elif section == "Method index" and l.startswith("| event | data |"):
        for cells in table_at(i):
            events.append({"event": TICK.fullmatch(cells[0]).group(1), "data_type": TICK.fullmatch(cells[1]).group(1)})
    elif section == "Method index" and l.startswith("| method | params | result |"):
        index = [TICK.fullmatch(c[0]).group(1) for c in table_at(i)]
    elif section == "Error codes" and l.startswith("| code |"):
        for cells in table_at(i):
            m = re.match(r"`(\w+)`( \(desk-local\))?", cells[0])
            det = re.match(r"`(Workstream\w+)`", cells[2])
            errors.append({"code": m.group(1), "desk_local": bool(m.group(2)), "detail_type": det.group(1) if det else None})
    m = re.match(r"^(Params|Result) \(`(\w+)`\):", l)
    if m and section == "Methods":
        types[m.group(2)] = fields(table_at(i + 1))
        method_info[h3][m.group(1).lower() + "_type"] = m.group(2)
    if l.startswith("Errors:") and section == "Methods":
        method_info[h3]["errors"] = TICK.findall(l)
    m = re.match(r"^```json (\S+) (params|result|error|data)$", l)
    if m:
        j = i + 1
        while lines[j] != "```":
            j += 1
        body = json.loads("\n".join(lines[i + 1:j]))
        examples.setdefault(m.group(1), {}).setdefault(m.group(2), []).append(body)
        i = j
    i += 1

assert index == methods, (index, methods)
os.makedirs(out, exist_ok=True)
contract = {"methods": methods, "events": events, "errors": errors, "enums": enums, "types": types}
def dump(path, v):
    with open(os.path.join(out, path), "w", encoding="utf-8") as f:
        json.dump(v, f, indent=2, ensure_ascii=False)
        f.write("\n")
dump("contract.json", contract)
for name in methods:
    info = dict(method_info[name])
    info["examples"] = examples.pop(name)
    dump(name + ".json", info)
for ev in events:
    dump(ev["event"] + ".json", {"event": ev["event"], "data_type": ev["data_type"], "examples": examples.pop(ev["event"])})
# collection examples the doc carries for the factory_job kind
for name, ex in sorted(examples.items()):
    dump(name + ".json", {"method": name, "examples": ex})
    print("extra:", name, sorted(ex))
print(len(methods), "methods;", len(types), "types;", len(enums), "enums;", len(errors), "error codes")
