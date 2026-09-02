#!/usr/bin/env python3
"""Frontend/backend route parity audit: every /api/ path referenced from any
client (web, admin, Android, iOS, extension) must resolve to a registered
Go API route."""
import glob
import re
import sys

# Client source roots and the string-literal syntax each uses for paths.
SCAN_ROOTS = [
    ("web", "apps/web/src", (".ts", ".tsx")),
    ("admin", "apps/admin/src", (".ts", ".tsx")),
    ("android", "apps/android", (".kt",)),
    ("ios", "apps/ios", (".swift",)),
    ("extension", "apps/extension", (".js", ".html")),
]

def norm(p):
    p = p.replace("`", "").split("?")[0]
    # Swift string interpolation \(var)
    p = re.sub(r"\\\([A-Za-z_][A-Za-z0-9_]*\)", "{}", p)
    # Kotlin/JS template interpolation ${...} and bare $var
    p = re.sub(r"\$\{[^{}]*$", "{}", p)
    p = re.sub(r"\$\{[^}]*\}", "{}", p)
    p = re.sub(r"\$([A-Za-z_][A-Za-z0-9_]*)", "{}", p)
    return re.sub(r"\{[^}]*\}", "{}", p)

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
    total_files = 0
    per_client = []
    for name, root, exts in SCAN_ROOTS:
        files = [
            f for f in glob.glob(root + "/**/*", recursive=True)
            if f.endswith(exts)
        ]
        total_files += len(files)
        refs = 0
        for path in files:
            try:
                src = open(path, encoding="utf-8").read()
            except (OSError, UnicodeDecodeError):
                continue
            for m in re.finditer(r'[`"\'](/api/[^\s`\'"]+)', src):
                ref = norm(m.group(1))
                refs += 1
                if any(segmatch(route, ref) for route in routes):
                    continue
                trimmed = ref.rstrip("{}").rstrip("/")
                if trimmed not in routes:
                    missing.append((ref, path))
        per_client.append(f"{name}={len(files)} files/{refs} refs")
    if missing:
        for ref, path in sorted(set(missing)):
            print(f"MISS {ref} in {path}", file=sys.stderr)
        print(f"parity: {len(missing)} orphaned refs across {total_files} files")
        sys.exit(1)
    print(f"parity: OK ({total_files} files, {len(routes)} registered routes; "
          + ", ".join(per_client) + ")")

main()
