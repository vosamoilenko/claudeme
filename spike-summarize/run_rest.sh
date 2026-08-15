#!/usr/bin/env bash
set -uo pipefail
cd /Users/vo1/Developer/claudeme/spike-summarize
declare -a jobs=(
  "gpt-5.4 t3"
  "gpt-5.4-mini t1"
  "gpt-5.4-mini t2"
  "gpt-5.4-mini t3"
  "gpt-5.6-luna t1"
  "gpt-5.6-luna t2"
  "gpt-5.6-luna t3"
)
for job in "${jobs[@]}"; do
  m=$(echo $job | cut -d' ' -f1)
  t=$(echo $job | cut -d' ' -f2)
  echo "=== $m $t start $(date +%T) ==="
  START=$(date +%s)
  ./run_codex.sh "$m" prompts/$t.md results/$m/$t.raw.json results/$m/$t.codex.log
  END=$(date +%s)
  echo "$m $t elapsed=$((END-START))s" >> run_rest.timing
  echo "=== $m $t end elapsed=$((END-START))s ==="
done
echo ALL_DONE >> run_rest.timing
