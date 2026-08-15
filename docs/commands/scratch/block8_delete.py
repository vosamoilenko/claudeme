"""Block 8: delete archived transcripts that have a verified digest.

A session is deletable only when its digest carries a non-empty summary AND
outcome. Its nested subagents/ directory goes with it: distill.py folds those
into the parent's digest, so they are deleted with the parent or not at all.

Run with --apply to actually remove. Without it, prints what it would do.
"""
import json, glob, os, shutil, sys

ARCHIVE = os.path.expanduser("~/.config/claudeme/shared/archive")
HISTORY = os.path.expanduser("~/.config/claudeme/shared/history")
apply = "--apply" in sys.argv

good = set()
for p in glob.glob(os.path.join(HISTORY, "*", "*.json")):
    for sid, d in json.load(open(p))["sessions"].items():
        s = d.get("summary") or {}
        if s.get("summary") and s.get("outcome"):
            good.add(sid)

def tree_bytes(path):
    if os.path.isfile(path):
        return os.path.getsize(path)
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            total += os.path.getsize(os.path.join(root, f))
    return total

targets, kept, freed = [], [], 0
for gz in sorted(glob.glob(os.path.join(ARCHIVE, "*", "*.jsonl.gz"))):
    sid = os.path.basename(gz).replace(".jsonl.gz", "")
    nested = gz[: -len(".jsonl.gz")]
    if sid in good:
        targets.append(gz)
        freed += tree_bytes(gz)
        if os.path.isdir(nested):
            targets.append(nested)
            freed += tree_bytes(nested)
    else:
        kept.append(gz)

# A session directory with no top-level transcript beside it belongs to nobody
# we can verify: leave it alone and report it.
orphan_dirs = [
    d for d in glob.glob(os.path.join(ARCHIVE, "*", "*"))
    if os.path.isdir(d) and not os.path.exists(d + ".jsonl.gz")
]

print(f"{'DELETING' if apply else 'would delete'}: {len(targets)} paths, {freed/1024/1024:.1f}M")
print(f"keeping: {len(kept)} undigested transcripts, {len(orphan_dirs)} orphan session dirs")
for k in kept:
    print("  keep:", os.path.basename(k))
for o in orphan_dirs:
    print("  orphan dir:", os.path.basename(os.path.dirname(o)) + "/" + os.path.basename(o))

if not apply:
    print("\ndry run — nothing removed. Re-run with --apply")
    raise SystemExit(0)

removed = 0
for t in targets:
    try:
        shutil.rmtree(t) if os.path.isdir(t) else os.remove(t)
        removed += 1
    except OSError as e:
        print("  FAILED:", t, e)
print(f"removed {removed} of {len(targets)} paths")
