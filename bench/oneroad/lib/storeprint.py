"""storeprint: one line per file under a folder — size, mtime and path, sorted —
for cell.sh's silence detector. It is the portable spelling of
`find -type f ! -name presence.json -printf '%s %T@ %p\n' | sort`, which BSD
find cannot run: on macOS that pipeline printed nothing, every reading matched
the last one, and a cell settled as idle after SILENCE_SECONDS however busy it
was. presence.json is left out for the reason cell.sh gives: it is a heartbeat.
"""
import os
import sys

lines = []
for folder, _, files in os.walk(sys.argv[1]):
    for name in files:
        if name == "presence.json":
            continue
        path = os.path.join(folder, name)
        try:
            st = os.stat(path)
        except OSError:
            continue
        lines.append(f"{st.st_size} {st.st_mtime} {path}")
print("\n".join(sorted(lines)))
