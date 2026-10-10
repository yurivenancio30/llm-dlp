package versao

import (
	"os"
	"regexp"
	"testing"
)

// A versão é escrita em dois lugares que uma pessoa lê (versao.go e CHANGELOG.md): têm de dizer
// o mesmo, e a versão tem de ter uma seção no CHANGELOG antes de ser lançada.
func TestVersaoBateComOChangelog(t *testing.T) {
	if !regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`).MatchString(Versao) {
		t.Fatalf("Versao = %q: o formato é MAIOR.MENOR.CORREÇÃO", Versao)
	}
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("CHANGELOG.md sem nenhuma seção de versão (## [X.Y.Z] - AAAA-MM-DD)")
	}
	if string(m[1]) != Versao {
		t.Fatalf("versao.go diz %s e a primeira versão do CHANGELOG.md é %s", Versao, m[1])
	}
}
