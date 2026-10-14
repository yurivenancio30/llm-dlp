package mask

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Funções de apoio usadas por vários testes.

var testKey = []byte("0123456789abcdef0123456789abcdef")

func newTest(t testing.TB) *Masker {
	t.Helper()
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	cfg.ExtraPatterns = []config.ExtraPattern{{Label: "matricula", Regex: `\bMAT\d{6}\b`}}
	cfg.Terms = []config.Term{{Label: "projeto", Values: []string{"Projeto Fenix"}}}
	ps, _ := LoadPeople(t.TempDir() + "/p.json")
	p := NewPseudo(testKey)
	ps.Import(p, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	ps.Import(p, "Ana Paula Souza", "", "U2001")
	vs, _ := LoadSeen(t.TempDir() + "/v.json")
	m, err := NewMasker(cfg, testKey, ps, vs)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// gera documentos válidos a partir dos dígitos-base, calculando os verificadores
func genCPF(base string) string {
	for dv := 0; dv < 100; dv++ {
		c := fmt.Sprintf("%s%02d", base, dv)
		if ValidCPF(c) {
			return c[:3] + "." + c[3:6] + "." + c[6:9] + "-" + c[9:]
		}
	}
	return ""
}

func genEleven(base string, ok func(string) bool) string {
	for dv := 0; dv < 100; dv++ {
		if c := fmt.Sprintf("%s%02d", base, dv); ok(c) {
			return c
		}
	}
	for dv := 0; dv < 10; dv++ {
		if c := fmt.Sprintf("%s%d", base, dv); ok(c) {
			return c
		}
	}
	return ""
}

// gera um número válido completando os dígitos verificadores por tentativa
func genValid(base string, n int, ok func(string) bool) string {
	for dv := 0; dv < 100; dv++ {
		for _, c := range []string{fmt.Sprintf("%s%02d", base, dv), fmt.Sprintf("%s%d", base, dv%10)} {
			if len(c) == n && ok(c) {
				return c
			}
		}
	}
	return ""
}

func sha(s string) [32]byte { return sha256.Sum256([]byte(s)) }

// asPandas imprime uma tabela como o pandas faz em print(df): índice à esquerda, cada
// coluna com a largura do maior valor, tudo alinhado à direita, dois espaços entre colunas.
func asPandas(cols []string, lines [][]string) string {
	wid := make([]int, len(cols))
	for j, c := range cols {
		wid[j] = len([]rune(c))
		for _, l := range lines {
			if n := len([]rune(l[j])); n > wid[j] {
				wid[j] = n
			}
		}
	}
	idx := len(fmt.Sprint(len(lines) - 1))
	var b strings.Builder
	line := func(first string, cells []string) {
		b.WriteString(fmt.Sprintf("%-*s", idx, first))
		for j, c := range cells {
			b.WriteString("  " + strings.Repeat(" ", wid[j]-len([]rune(c))) + c)
		}
		b.WriteString("\n")
	}
	line("", cols)
	for i, l := range lines {
		line(fmt.Sprint(i), l)
	}
	return b.String()
}

// numerar põe o número da linha antes de cada linha, como a ferramenta de leitura do Claude Code.
func numberLines(text string) string {
	var b strings.Builder
	for i, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, l)
	}
	return b.String()
}
