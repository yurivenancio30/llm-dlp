package mask

import (
	_ "embed"
	"strings"
	"unicode"
)

// Palavras comuns: uma palavra comum do português ou do inglês, sozinha ("tabela", "conta",
// "linhagem", "status"), decidida como nome num dado (uma tabela CONTA existe) continua
// mascarada onde é nome (no dado, no comando), mas o contágio não a leva para a prosa do modelo
// (o bloco de texto da resposta): lá ela é a palavra, com o sentido dela ("só acesso a conta
// trial"). Vale só para nome de objeto: segredo, documento, e-mail e os detectores de formato
// não passam por aqui. Termo cadastrado (config "termos") sempre vence: palavra comum que
// contém um termo continua mascarada.
//
// Dados: palavras_comuns.txt, as 20000 palavras mais frequentes de cada idioma no FrequencyWords
// (OpenSubtitles 2018), CC BY-SA 4.0; origem e critério no cabeçalho do arquivo.

//go:embed palavras_comuns.txt
var palavrasComunsTxt string

var palavrasComuns = lerListaGerada(palavrasComunsTxt)

// palavraComum: v (sem aspas e colchetes) é uma palavra só, de letras, da lista.
func palavraComum(v string) bool {
	v = strings.Trim(semCitacao(v), "[]")
	if len(v) < 2 {
		return false
	}
	for _, r := range v {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return palavrasComuns[strings.ToLower(v)]
}

// temTermo: v contém um termo cadastrado (config "termos").
func (m *Masker) temTermo(v string) bool { return m.termos != nil && m.termos.MatchString(v) }

// comumLivre: nome de objeto que fica em claro por ser palavra comum.
func (m *Masker) comumLivre(v string) bool { return palavraComum(v) && !m.temTermo(v) }
