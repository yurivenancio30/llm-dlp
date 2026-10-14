package mask

import (
	_ "embed"
	"strings"
	"unicode"
)

// Referência pública derivada (ver docs/pt-BR/estruturas.md). Dois arquivos de dados
// GERADOS por medição no material público da máquina (TestGeneratePublicRef, em
// vocab_gerar_test.go), nunca escritos à mão. O cabeçalho de cada um diz de onde, quando
// e com que critério foi gerado.
//
//   - ref_publica.txt: palavras que aparecem em posição de nome de recurso (valor de chave de
//     tipo em YAML/JSON/TOML/INI, opção --tipo, nome depois de FROM/JOIN/INTO/SCHEMA no SQL) em
//     muitos projetos públicos distintos (default, public, dbo, api, app...). São genéricas
//     demais para a memória da conversa: mascaradas onde uma regra as decidiu, nunca
//     propagadas (Decision.Generic, isGeneric).
//   - imagens_oficiais.txt: as Imagens Oficiais do Docker (redis, postgres, nginx...), as
//     pastas do repositório público docker-library/docs (uma por imagem, com content.md e
//     metadata.json). Só uma imagem dessa lista, sem organização, prova que o serviço com o
//     mesmo nome é o software (textSoftware); imagem local ("image: api-x" com build:) não.
//   - software_publico.txt e fornecedores.txt: nome e dono dos repositórios públicos populares
//     do GitHub. Na regra de imagem, org/nome só fica em claro com as duas chaves: organização
//     fornecedora e nome de software público.
//   - tipos_linguagem.txt: os tipos de dado do vocabulário (vocab_tipos.go) que o código Go
//     público usa como tipo de campo ou de variável ("nome    tipo", a mesma forma de um
//     esquema). Num bloco "nome    tipo", só esses tipos não decidem que é esquema (o leitor de
//     esquema pede um tipo que só os dados usam: object, category, datetime64[ns], VARCHAR(n)).

//go:embed dados/ref_publica.txt
var publicRefTxt string

//go:embed dados/tipos_linguagem.txt
var languageTypesTxt string

//go:embed dados/imagens_oficiais.txt
var officialImagesTxt string

//go:embed dados/software_publico.txt
var publicSoftwareTxt string

//go:embed dados/fornecedores.txt
var vendorsTxt string

// publicSoftware e fornecedores: nome e dono dos repositórios populares do GitHub (medido;
// normSoftware). Só valem juntos (publicPath): sozinha, a lista de nomes inclui codinomes.
var publicSoftware, vendors map[string]bool

// officialImages: nomes das Imagens Oficiais do Docker (medido; minúsculas).
var officialImages map[string]bool

// languageTypes: tipos de dado que também são tipo de linguagem (medido; minúsculas).
var languageTypes map[string]bool

func init() {
	publicRef = readGeneratedList(publicRefTxt)
	languageTypes = readGeneratedList(languageTypesTxt)
	officialImages = readGeneratedList(officialImagesTxt)
	publicSoftware = readGeneratedList(publicSoftwareTxt)
	vendors = readGeneratedList(vendorsTxt)
}

// readGeneratedList: uma palavra por linha (minúsculas); "#" começa comentário (o cabeçalho).
func readGeneratedList(txt string) map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(txt, "\n") {
		if l = strings.TrimSpace(l); l != "" && l[0] != '#' {
			m[strings.ToLower(l)] = true
		}
	}
	return m
}

// normSoftware: o nome como as listas do GitHub guardam (minúsculas, sem - _ .): o Docker Hub
// escreve "prometheuscommunity/postgres-exporter" e o GitHub "prometheus-community/postgres_exporter".
func normSoftware(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.':
			return -1
		}
		return unicode.ToLower(r)
	}, v)
}

// publicOrg e publicName: as duas chaves da imagem pública (publicPath, textSoftware).
func publicOrg(org string) bool {
	return publicRef[strings.ToLower(org)] || vendors[normSoftware(org)]
}

// publicName: o nome inteiro é software público ou, pedaço a pedaço, cada pedaço é software,
// palavra da referência, papel, tipo ou versão, e pelo menos um é software (clickhouse-server,
// statsd-exporter, node-chrome). Um pedaço desconhecido bloqueia (payments-api, cp-kafka).
func publicName(name string) bool {
	l := strings.ToLower(name)
	if officialImages[l] || publicRef[l] || publicSoftware[normSoftware(name)] {
		return true
	}
	ps := valuePieces(name)
	if len(ps) < 2 || len(ps) > 6 {
		return false
	}
	sw := false
	for _, p := range ps {
		switch {
		case officialImages[p] || publicSoftware[p]:
			sw = true
		case isDigit(p[0]) || len(p) <= 2 && p[0] == 'v' || publicRef[p] || roleVocab[p] || typeVocab[p]:
		default:
			return false
		}
	}
	return sw
}
