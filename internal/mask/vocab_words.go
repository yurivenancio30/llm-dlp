package mask

import (
	_ "embed"
	"strings"
	"unicode"
)

// Common words: a common Portuguese or English word, alone ("tabela", "conta", "linhagem",
// "status"), decided as a name in data (a CONTA table exists) stays masked where a reader decides
// it (in the data, in the command, in a DDL or kubectl -n pasted in the message), but contagion
// does not carry it into prose (the text of the message, from the user or the response): there it
// is the word, with its meaning ("só acesso a conta trial", "em qual tabela você rodaria?"). A
// customer name that is a common word alone and that the user cites in prose goes as a word (the
// user's choice; what matters to protect goes in "terms"). Applies only to object names: secret,
// document, e-mail and the format detectors do not go through here. A registered term (config
// "terms") always wins: a common word that contains a term stays masked.
//
// Data: common_words.txt, the 20000 most frequent words of each language in FrequencyWords
// (OpenSubtitles 2018), CC BY-SA 4.0; origin and criterion in the file header.

//go:embed data/common_words.txt
var commonWordsTxt string

var commonWords = readGeneratedList(commonWordsTxt)

// dictionary_words.txt: the 50000 most frequent of each language, minus the common ones (generated
// by TestGenerateDictionary). Only the software proof uses it (memory_software.go).
//
//go:embed data/dictionary_words.txt
var dictionaryWordsTxt string

var dictionaryWords = readGeneratedList(dictionaryWordsTxt)

// commonWord: v (without quotes and brackets) is a single word, of letters, from the list.
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

// hasTerm: v contains a registered term (config "terms").
func (m *Masker) hasTerm(v string) bool { return m.terms != nil && m.terms.MatchString(v) }

// freeCommon: an object name that stays in plain text because it is a common word.
func (m *Masker) freeCommon(v string) bool { return commonWord(v) && !m.hasTerm(v) }
