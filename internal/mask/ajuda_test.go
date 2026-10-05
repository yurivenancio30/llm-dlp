package mask

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Funções de apoio usadas por vários testes.

var chaveTeste = []byte("0123456789abcdef0123456789abcdef")

func novoTeste(t *testing.T) *Masker {
	t.Helper()
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"empresa-ficticia"}
	cfg.PadroesExtras = []config.PadraoExtra{{Rotulo: "matricula", Regex: `\bMAT\d{6}\b`}}
	cfg.Termos = []config.Termo{{Rotulo: "projeto", Valores: []string{"Projeto Fenix"}}}
	ps, _ := CarregarPessoas(t.TempDir() + "/p.json")
	p := NovoPseudo(chaveTeste)
	ps.Importar(p, "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	ps.Importar(p, "Ana Paula Souza", "", "U2001")
	vs, _ := CarregarVistos(t.TempDir() + "/v.json")
	m, err := NovoMasker(cfg, chaveTeste, ps, vs)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// gera documentos válidos a partir dos dígitos-base, calculando os verificadores
func gerarCPF(base string) string {
	for dv := 0; dv < 100; dv++ {
		c := fmt.Sprintf("%s%02d", base, dv)
		if CPFValido(c) {
			return c[:3] + "." + c[3:6] + "." + c[6:9] + "-" + c[9:]
		}
	}
	return ""
}

func gerarOnze(base string, ok func(string) bool) string {
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
func gerarValido(base string, n int, ok func(string) bool) string {
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

// comoPandas imprime uma tabela como o pandas faz em print(df): índice à esquerda, cada
// coluna com a largura do maior valor, tudo alinhado à direita, dois espaços entre colunas.
func comoPandas(cols []string, linhas [][]string) string {
	larg := make([]int, len(cols))
	for j, c := range cols {
		larg[j] = len([]rune(c))
		for _, l := range linhas {
			if n := len([]rune(l[j])); n > larg[j] {
				larg[j] = n
			}
		}
	}
	idx := len(fmt.Sprint(len(linhas) - 1))
	var b strings.Builder
	linha := func(primeiro string, cels []string) {
		b.WriteString(fmt.Sprintf("%-*s", idx, primeiro))
		for j, c := range cels {
			b.WriteString("  " + strings.Repeat(" ", larg[j]-len([]rune(c))) + c)
		}
		b.WriteString("\n")
	}
	linha("", cols)
	for i, l := range linhas {
		linha(fmt.Sprint(i), l)
	}
	return b.String()
}

// numerar põe o número da linha antes de cada linha, como a ferramenta de leitura do Claude Code.
func numerar(texto string) string {
	var b strings.Builder
	for i, l := range strings.Split(strings.TrimRight(texto, "\n"), "\n") {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, l)
	}
	return b.String()
}

// semObjetos: sem os leitores de estrutura (nomes de tabela, coluna, servidor...), para os
// testes que olham só os detectores de dado pessoal.
func semObjetos(t *testing.T) *Masker {
	m := novoTeste(t)
	m.cfg.Objetos.Ligado = false
	return m
}
