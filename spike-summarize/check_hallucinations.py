#!/usr/bin/env python3
"""
Mechanically check a model's raw.json summary against the source digest for
hallucinated absolute paths, shell commands, and URLs.

Usage: check_hallucinations.py <raw.json> <digest.txt>
Prints offenders and a count.
"""
import json, re, sys

def load_json_relaxed(path):
    with open(path) as f:
        return json.load(f)

def extract_paths(text):
    # genuine absolute filesystem paths only (start with /Users/) to avoid
    # false positives from prose that uses "/" as a shorthand separator
    # (e.g. "entity/DTO/service", "form/model/mapper/component").
    pat = re.compile(r'/Users/[A-Za-z0-9_.\-]+(?:/[A-Za-z0-9_.\-]+){1,}')
    found = set(pat.findall(text))
    cleaned = set()
    for p in found:
        p = p.rstrip('.,)\'"`:;')
        if len(p) > 3:
            cleaned.add(p)
    return cleaned

def extract_urls(text):
    pat = re.compile(r'https?://[^\s"\'`)]+')
    return set(m.rstrip('.,)\'"`:;') for m in pat.findall(text))

def main():
    raw_path, digest_path = sys.argv[1], sys.argv[2]
    digest = open(digest_path).read()

    try:
        data = load_json_relaxed(raw_path)
        raw_text = json.dumps(data)
    except Exception as e:
        print(f"SCHEMA_PARSE_FAIL: {e}")
        sys.exit(1)

    # commands_of_note: verbatim check
    cmd_offenders = []
    for cmd in data.get("commands_of_note", []) or []:
        if cmd not in digest:
            cmd_offenders.append(cmd)

    # paths anywhere in the output JSON text
    paths = extract_paths(raw_text)
    path_offenders = sorted(p for p in paths if p not in digest)

    # urls
    urls = extract_urls(raw_text)
    url_offenders = sorted(u for u in urls if u not in digest)

    total = len(cmd_offenders) + len(path_offenders) + len(url_offenders)
    print(f"paths_checked={len(paths)} url_checked={len(urls)} commands_checked={len(data.get('commands_of_note', []) or [])}")
    print(f"HALLUCINATION_COUNT={total}")
    if cmd_offenders:
        print("-- command offenders --")
        for o in cmd_offenders:
            print(" ", o)
    if path_offenders:
        print("-- path offenders --")
        for o in path_offenders:
            print(" ", o)
    if url_offenders:
        print("-- url offenders --")
        for o in url_offenders:
            print(" ", o)

if __name__ == "__main__":
    main()
