package mask

import (
	_ "embed"
	"strings"
	"unicode"
)

// Referência pública derivada (ver docs/estruturas.md). Dois arquivos de dados
// GERADOS por medição no material público da máquina (TestGerarRefPublica, em
// vocab_gerar_test.go), nunca escritos à mão. O cabeçalho de cada um diz de onde, quando
// e com que critério foi gerado.
//
//   - ref_publica.txt: palavras que aparecem em posição de nome de recurso (valor de chave de
//     tipo em YAML/JSON/TOML/INI, opção --tipo, nome depois de FROM/JOIN/INTO/SCHEMA no SQL) em
//     muitos projetos públicos distintos (default, public, dbo, api, app...). São genéricas
//     demais para a memória da conversa: mascaradas onde uma regra as decidiu, nunca
//     propagadas (Decisao.Generica, ehGenerica).
//   - imagens_oficiais.txt: as Imagens Oficiais do Docker (redis, postgres, nginx...), as
//     pastas do repositório público docker-library/docs (uma por imagem, com content.md e
//     metadata.json). Só uma imagem dessa lista, sem organização, prova que o serviço com o
//     mesmo nome é o software (softwareDoTexto); imagem local ("image: api-x" com build:) não.
//   - software_publico.txt e fornecedores.txt: nome e dono dos repositórios públicos populares
//     do GitHub. Na regra de imagem, org/nome só fica em claro com as duas chaves: organização
//     fornecedora e nome de software público.
//   - tipos_linguagem.txt: os tipos de dado do vocabulário (vocab_tipos.go) que o código Go
//     público usa como tipo de campo ou de variável ("nome    tipo", a mesma forma de um
//     esquema). Num bloco "nome    tipo", só esses tipos não decidem que é esquema (o leitor de
//     esquema pede um tipo que só os dados usam: object, category, datetime64[ns], VARCHAR(n)).

//go:embed dados/ref_publica.txt
var refPublicaTxt string

//go:embed dados/tipos_linguagem.txt
var tiposLinguagemTxt string

//go:embed dados/imagens_oficiais.txt
var imagensOficiaisTxt string

//go:embed dados/software_publico.txt
var softwarePublicoTxt string

//go:embed dados/fornecedores.txt
var fornecedoresTxt string

// softwarePublico e fornecedores: nome e dono dos repositórios populares do GitHub (medido;
// normSoftware). Só valem juntos (caminhoPublico): sozinha, a lista de nomes inclui codinomes.
var softwarePublico, fornecedores map[string]bool

// imagensOficiais: nomes das Imagens Oficiais do Docker (medido; minúsculas).
var imagensOficiais map[string]bool

// tiposLinguagem: tipos de dado que também são tipo de linguagem (medido; minúsculas).
var tiposLinguagem map[string]bool

func init() {
	refPublica = lerListaGerada(refPublicaTxt)
	tiposLinguagem = lerListaGerada(tiposLinguagemTxt)
	imagensOficiais = lerListaGerada(imagensOficiaisTxt)
	softwarePublico = lerListaGerada(softwarePublicoTxt)
	fornecedores = lerListaGerada(fornecedoresTxt)
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

// orgPublica e nomePublico: as duas chaves da imagem pública (caminhoPublico, softwareDoTexto).
func orgPublica(org string) bool {
	return refPublica[strings.ToLower(org)] || fornecedores[normSoftware(org)]
}

// nomePublico: o nome inteiro é software público ou, pedaço a pedaço, cada pedaço é software,
// palavra da referência, papel, tipo ou versão, e pelo menos um é software (clickhouse-server,
// statsd-exporter, node-chrome). Um pedaço desconhecido bloqueia (payments-api, cp-kafka).
func nomePublico(nome string) bool {
	l := strings.ToLower(nome)
	if imagensOficiais[l] || refPublica[l] || softwarePublico[normSoftware(nome)] {
		return true
	}
	ps := pedacosValor(nome)
	if len(ps) < 2 || len(ps) > 6 {
		return false
	}
	sw := false
	for _, p := range ps {
		switch {
		case imagensOficiais[p] || softwarePublico[p]:
			sw = true
		case ehDigito(p[0]) || len(p) <= 2 && p[0] == 'v' || refPublica[p] || vocabPapel[p] || vocabTipo[p]:
		default:
			return false
		}
	}
	return sw
}
