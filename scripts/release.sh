#!/usr/bin/env bash
# Lança a versão que está em internal/versao/versao.go: confere, testa e cria a tag vX.Y.Z.
# Não envia nada: no fim, mostra o comando de envio. Ver docs/versoes.md.
set -euo pipefail
cd "$(dirname "$0")/.."

erro() { echo "release: $*" >&2; exit 1; }

V=$(sed -n 's/^const Versao = "\(.*\)"$/\1/p' internal/versao/versao.go)
[[ $V =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || erro "versão inválida em internal/versao/versao.go: '$V'"
TAG="v$V"

[[ $(git branch --show-current) == main ]] || erro "a versão sai da branch main (você está em $(git branch --show-current))"
[[ -z $(git status --porcelain) ]] || erro "há mudança não commitada"
git rev-parse -q --verify "refs/tags/$TAG" >/dev/null && erro "a tag $TAG já existe: aumente a versão em versao.go"
grep -q "^## \[$V\] - [0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}$" CHANGELOG.md || erro "CHANGELOG.md sem a seção '## [$V] - AAAA-MM-DD'"
[[ -n $(./scripts/notas-da-versao.sh "$V") ]] || erro "a seção $V do CHANGELOG.md está vazia"

echo "== testes"
make test
echo "== binários"
make dist

git tag -s "$TAG" -m "llm-dlp $V"
echo
echo "Tag $TAG criada. Para publicar (o GitHub monta a release com os binários):"
echo "  git push origin main $TAG"
