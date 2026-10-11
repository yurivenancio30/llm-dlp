#!/usr/bin/env bash
# Imprime a seção de uma versão do CHANGELOG.md como as notas da release no GitHub.
# Uso: notas-da-versao.sh 0.2.0
#
# A página de release mostra cada quebra de linha do texto como quebra de linha, e não acha
# link relativo. Por isso cada parágrafo e cada item de lista sai numa linha só (o CHANGELOG é
# quebrado em 95 colunas), e os links para arquivos do repositório viram links para a tag.
set -euo pipefail
cd "$(dirname "$0")/.."
V=${1:?uso: notas-da-versao.sh X.Y.Z}
REPO=https://github.com/yurivenancio30/llm-dlp
awk -v v="$V" '
  function vazios()  { for (; vazias > 0; vazias--) print "" }
  function soltar()  { if (buf != "") { print buf; buf = "" } }
  /^## \[/           { soltar(); dentro = (index($0, "## [" v "]") == 1); next }
  !dentro            { next }
  /^```/             { soltar(); vazios(); print; cerca = !cerca; comecou = 1; next }
  cerca              { print; next }
  /^[[:space:]]*$/   { soltar(); if (comecou) vazias++; next }
  /^(#|\|)/          { soltar(); vazios(); print; comecou = 1; next }   # título e linha de tabela
  /^[[:space:]]*([-*+]|[0-9]+\.)[[:space:]]/ { soltar(); vazios(); buf = $0; comecou = 1; next }
  buf != ""          { sub(/^[[:space:]]+/, ""); buf = buf " " $0; next }   # continuação
                     { vazios(); buf = $0; comecou = 1 }
  END                { soltar() }
' CHANGELOG.md |
  sed -E "s#\]\((docs/|\.github/|README\.md|CHANGELOG\.md|install\.sh|LICENSE)#](${REPO}/blob/v${V}/\1#g"
cat <<FIM

---

**Install or update:** \`curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh\` (then close Claude Code and run \`llm-dlp parar\`). The archives below are checked against \`checksums.txt\`.

[Changelog em português](${REPO}/blob/v${V}/docs/pt-BR/CHANGELOG.md) · [All versions](${REPO}/blob/main/CHANGELOG.md)
FIM
