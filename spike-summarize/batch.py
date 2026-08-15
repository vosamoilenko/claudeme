import glob, json, sys, time, traceback
from distill import distill
files = sorted(glob.glob("/Users/vo1/.config/claudeme/shared/projects/*/*.jsonl"))[:200]
ok, fail, rows = 0, [], []
t0 = time.time()
for f in files:
    try:
        d = distill(f)
        rows.append({k: d[k] for k in ("session_id","title","cwd","started","ended","metrics")})
        ok += 1
    except Exception as e:
        fail.append((f, repr(e)))
json.dump(rows, open("metrics.json","w"))
el = time.time()-t0
print(f"ok={ok} fail={len(fail)} in {el:.1f}s  ({el/max(ok,1)*1000:.0f} ms/session)")
for f, e in fail[:5]: print("FAIL", f.split('/')[-1], e)
m = [r["metrics"] for r in rows]
agg = lambda k: sum(x.get(k,0) for x in m)
print(f"\nacross {ok} sessions:")
print(f"  human prompts      {agg('human_prompts'):>12,}")
print(f"  assistant msgs     {agg('assistant_msgs'):>12,}")
print(f"  tool calls         {agg('tool_calls'):>12,}   errors {agg('tool_errors'):,}")
print(f"  interruptions      {agg('interruptions'):>12,}")
print(f"  subagents          {agg('subagents'):>12,}")
print(f"  output tokens      {agg('output_tokens') or sum(x['tokens'].get('output',0) for x in m):>12,}")
print(f"  cache read tokens  {sum(x['tokens'].get('cache_read',0) for x in m):>12,}")
print(f"  wall time          {agg('wall_ms')/3.6e6:>12.1f} h")
print(f"  sessions w/ 0 human prompts: {sum(1 for x in m if x['human_prompts']==0)}")
