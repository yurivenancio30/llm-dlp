// Package versao diz qual código está rodando: a versão (escrita aqui) e o commit (gravado na
// compilação pelo Makefile). "llm-dlp versao" e /__llm-dlp/saude mostram os dois.
package versao

// Versao: a versão do llm-dlp, no formato MAIOR.MENOR.CORREÇÃO (ver docs/versoes.md). É a única
// fonte: a tag do git (v0.2.0) e a primeira seção do CHANGELOG.md têm de dizer o mesmo, e
// TestVersaoBateComOChangelog confere.
const Versao = "0.2.0"

// Commit: preenchido por -ldflags "-X .../internal/versao.Commit=abc1234" (o Makefile faz).
// "-mod" no fim: compilado com mudanças não commitadas.
var Commit = "desconhecido"

// Completa: "0.2.0 (abc1234)".
func Completa() string { return Versao + " (" + Commit + ")" }
