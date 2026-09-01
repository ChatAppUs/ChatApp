#!/usr/bin/env python3
"""Frontend/backend route parity audit: every /api/ path referenced from the
web and admin front-ends must resolve to a registered Go API route."""
import glob
import re
import sys

def norm(p):
    p = p.replace("`", "").split("?")[0]
    # Collapse template-embedded ternaries like `positions${statusFilter ? ...}`
    p = re.sub(r"\$\{[^{}]*$", "{}", p)
    return re.sub(r"\{[^}]*\}", "{}", p.replace("$", ""))

def segmatch(route, ref):
    rs, fs = route.split("/"), ref.split("/")
    if len(rs) != len(fs):
        return False
    return all(a == b or a == "{}" or b == "{}" for a, b in zip(rs, fs))

def main():
    go_main = open("services/api/main.go").read()
    routes = {}
    for m in re.finditer(r'mux\.HandleFunc\(\s*"([^"]+)"', go_main):
        route = m.group(1).split()[-1]
        routes.setdefault(norm(route), route)

    missing = []
    files = []
    for root in ["apps/web/src", "apps/admin/src"]:
        files.extend(
            f for f in glob.glob(root + "/**/*.ts*", recursive=True)
            if f.endswith((".ts", ".tsx"))
        )
    for path in files:
        src = open(path, encoding="utf-8").read()
        for m in re.finditer(r'[`"\'](/api/[^\s`\'"]+)', src):
            ref = norm(m.group(1))
            if not any(segmatch(route, ref) for route in routes):
                trimmed = ref.rstrip("{}").rstrip("/")
                if not trimmed in routes:
                    missing.append((ref, path))
    if missing:
        for ref, path in sorted(set(missing)):
            print(f"MISS {ref} in {path}", file=sys.stderr)
        print(f"parity: {len(missing)} orphaned refs across {len(files)} files")
        sys.exit(1)
    print(f"parity: OK ({len(files)} files, {len(routes)} registered routes)")

main()
