#!/usr/bin/env bash
# Imprime a seção de uma versão do CHANGELOG.md (as notas da release). Uso: notas-da-versao.sh 0.2.0
set -euo pipefail
cd "$(dirname "$0")/.."
V=${1:?uso: notas-da-versao.sh X.Y.Z}
awk -v v="$V" '
  /^## \[/           { dentro = (index($0, "## [" v "]") == 1); next }
  !dentro            { next }
  /^[[:space:]]*$/   { if (comecou) vazias++; next }
                     { for (; vazias > 0; vazias--) print ""; print; comecou = 1 }
' CHANGELOG.md
