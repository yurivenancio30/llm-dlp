// Package versao diz qual código está rodando. O commit é gravado na compilação (ver o
// Makefile): "llm-dlp versao" e /__llm-dlp/saude mostram os dois.
package versao

const Versao = "0.1.0"

// Commit: preenchido por -ldflags "-X .../internal/versao.Commit=abc1234" (o Makefile faz).
// "-mod" no fim: compilado com mudanças não commitadas.
var Commit = "desconhecido"

// Completa: "0.1.0 (abc1234)".
func Completa() string { return Versao + " (" + Commit + ")" }
