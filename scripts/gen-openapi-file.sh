#!/bin/sh
# 从一个 OpenAPI JSON 文件生成 web/src/openapi.gen.js（Docs 页展示用）。
# 用法：scripts/gen-openapi-file.sh <openapi.json 路径>
set -eu
SRC="${1:-build/openapi.json}"
OUT="web/src/openapi.gen.js"
[ -f "$SRC" ] || { echo "gen-openapi-file: 找不到 $SRC" >&2; exit 1; }

python3 - "$OUT" "$SRC" <<'PY'
import json, sys
out, src = sys.argv[1], sys.argv[2]
with open(src, encoding='utf-8') as f:
    doc = json.load(f)
pretty = json.dumps(doc, ensure_ascii=False, indent=2)
esc = pretty.replace('\\', '\\\\').replace('`', '\\`').replace('${', '\\${')
with open(out, 'w', encoding='utf-8') as f:
    f.write('// 自动生成：勿手改。来源 scripts/gen-openapi-file.sh，内容为 /openapi.json 原文。\n')
    f.write('export const OPENAPI = `%s`\n' % esc)
print('gen-openapi-file: wrote', out)
PY
