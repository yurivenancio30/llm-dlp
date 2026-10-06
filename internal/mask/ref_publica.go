package mask

import (
	_ "embed"
	"strings"
)

// Referência pública derivada (P1, item D3; ver docs/estruturas.md). Dois arquivos de dados
// GERADOS por medição no material público da máquina (TestGerarRefPublica, em
// ref_publica_gerar_test.go), nunca escritos à mão. O cabeçalho de cada um diz de onde, quando
// e com que critério foi gerado.
//
//   - ref_publica.txt: palavras que aparecem em posição de nome de recurso (valor de chave de
//     tipo em YAML/JSON/TOML/INI, opção --tipo, nome depois de FROM/JOIN/INTO/SCHEMA no SQL) em
//     muitos projetos públicos distintos (default, public, dbo, api, app...). São genéricas
//     demais para a memória da conversa: mascaradas onde uma regra as decidiu, nunca
//     propagadas (Decisao.Generica, ehGenerica).
//   - tipos_linguagem.txt: os tipos de dado do vocabulário (vocab_tipos.go) que o código Go
//     público usa como tipo de campo ou de variável ("nome    tipo", a mesma forma de um
//     esquema). Num bloco "nome    tipo", só esses tipos não decidem que é esquema (o leitor de
//     esquema pede um tipo que só os dados usam: object, category, datetime64[ns], VARCHAR(n)).

//go:embed ref_publica.txt
var refPublicaTxt string

//go:embed tipos_linguagem.txt
var tiposLinguagemTxt string

// tiposLinguagem: tipos de dado que também são tipo de linguagem (medido; minúsculas).
var tiposLinguagem map[string]bool

func init() {
	refPublica = lerListaGerada(refPublicaTxt)
	tiposLinguagem = lerListaGerada(tiposLinguagemTxt)
}

// lerListaGerada: uma palavra por linha (minúsculas); "#" começa comentário (o cabeçalho).
func lerListaGerada(txt string) map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(txt, "\n") {
		if l = strings.TrimSpace(l); l != "" && l[0] != '#' {
			m[strings.ToLower(l)] = true
		}
	}
	return m
}
