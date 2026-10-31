#!/bin/sh
# 从运行中的 hookdock 抓 /openapi.json，生成 web/src/openapi.gen.js（Docs 页展示用）。
# 用法：scripts/gen-openapi.sh [base_url]   默认 http://127.0.0.1:8080
set -eu
BASE="${1:-http://127.0.0.1:8080}"
OUT="web/src/openapi.gen.js"

JSON=$(curl -fsS --http1.1 "$BASE/openapi.json")

python3 - "$OUT" "$JSON" <<'PY' 2>/dev/null || {
  # python3 不可用时退化为 sed 转义内嵌
  ESC=$(printf '%s' "$JSON" | sed 's/\\/\\\\/g; s/`/\\`/g; s/\$/\\$/g')
  printf '// 自动生成：勿手改。来源 scripts/gen-openapi.sh，内容为 /openapi.json 原文。\nexport const OPENAPI = `%s`\n' "$ESC" > "$OUT"
  echo "gen-openapi: wrote $OUT (sed fallback)"
  exit 0
}
import json, sys
out, raw = sys.argv[1], sys.argv[2]
doc = json.loads(raw)
pretty = json.dumps(doc, ensure_ascii=False, indent=2)
esc = pretty.replace('\\', '\\\\').replace('`', '\\`').replace('${', '\\${')
with open(out, 'w') as f:
    f.write('// 自动生成：勿手改。来源 scripts/gen-openapi.sh，内容为 /openapi.json 原文。\n')
    f.write('export const OPENAPI = `%s`\n' % esc)
print('gen-openapi: wrote', out)
PY
