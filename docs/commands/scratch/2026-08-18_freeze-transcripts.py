import gzip, os, shutil, subprocess, sys, tempfile
SH = os.path.expanduser('~/.config/claudeme/shared')
LIVE, ARCH = os.path.join(SH, 'projects'), os.path.join(SH, 'archive')
dry = '--apply' not in sys.argv

def walk(root, suffix):
    for dp, _, fs in os.walk(root):
        for f in fs:
            if f.endswith(suffix):
                yield os.path.join(dp, f)

missing, stale, same = [], [], 0
for src in walk(LIVE, '.jsonl'):
    rel = os.path.relpath(src, LIVE)
    dst = os.path.join(ARCH, rel + '.gz')
    if not os.path.exists(dst):
        missing.append((src, dst)); continue
    live_n = sum(1 for _ in open(src, 'rb'))
    with gzip.open(dst, 'rb') as fh:
        arch_n = sum(1 for _ in fh)
    if live_n > arch_n:
        stale.append((src, dst, arch_n, live_n))
    else:
        same += 1

print(f'{same} already frozen and current, {len(missing)} missing, {len(stale)} stale')
if dry:
    for s, d, a, l in stale[:10]:
        print(f'  stale {os.path.basename(s)}: archive {a} lines, live {l}')
    print('  dry run — nothing written (pass --apply)')
    raise SystemExit

n = 0
for item in [(s, d) for s, d in missing] + [(s, d) for s, d, _, _ in stale]:
    src, dst = item
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    tmp = dst + '.tmp'
    with open(src, 'rb') as fi, gzip.open(tmp, 'wb') as fo:
        shutil.copyfileobj(fi, fo)
    os.replace(tmp, dst)
    n += 1
print(f'{n} transcripts frozen into {ARCH}')
