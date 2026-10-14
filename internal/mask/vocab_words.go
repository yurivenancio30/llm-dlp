package mask

import (
	_ "embed"
	"strings"
	"unicode"
)

// Words comuns: uma palavra comum do português ou do inglês, sozinha ("tabela", "conta",
// "linhagem", "status"), decidida como nome num dado (uma tabela CONTA existe) continua
// mascarada onde um leitor a decide (no dado, no comando, num DDL ou kubectl -n colado na
// mensagem), mas o contágio não a leva para a prosa (o texto da mensagem, do usuário ou da
// resposta): lá ela é a palavra, com o sentido dela ("só acesso a conta trial", "em qual tabela
// você rodaria?"). O nome do cliente que é uma palavra comum sozinha e que o usuário cita em
// prosa vai como palavra (escolha do usuário; o que importa proteger vai em "termos"). Vale só para nome de objeto: segredo, documento, e-mail e os detectores de formato
// não passam por aqui. Term cadastrado (config "termos") sempre vence: palavra comum que
// contém um termo continua mascarada.
//
// Dados: common_words.txt, as 20000 palavras mais frequentes de cada idioma no FrequencyWords
// (OpenSubtitles 2018), CC BY-SA 4.0; origem e critério no cabeçalho do arquivo.

//go:embed data/common_words.txt
var commonWordsTxt string

var commonWords = readGeneratedList(commonWordsTxt)

// dictionary_words.txt: as 50000 mais frequentes de cada idioma, menos as comuns (gerado por
// TestGenerateDictionary). Só a prova de software o usa (memory_software.go).
//
//go:embed data/dictionary_words.txt
var dictionaryWordsTxt string

var dictionaryWords = readGeneratedList(dictionaryWordsTxt)

// commonWord: v (sem aspas e colchetes) é uma palavra só, de letras, da lista.
func commonWord(v string) bool {
	v = strings.Trim(unquoted(v), "[]")
	if len(v) < 2 {
		return false
	}
	for _, r := range v {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return commonWords[strings.ToLower(v)]
}

// hasTerm: v contém um termo cadastrado (config "termos").
func (m *Masker) hasTerm(v string) bool { return m.terms != nil && m.terms.MatchString(v) }

// freeCommon: nome de objeto que fica em claro por ser palavra comum.
func (m *Masker) freeCommon(v string) bool { return commonWord(v) && !m.hasTerm(v) }
