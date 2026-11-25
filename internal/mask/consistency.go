package mask

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Consistency of a request, for the log (counts only, never the value). The log only sees what the
// proxy detected; these two numbers are the part of its own mistakes it can see by itself:
//
//   - in the clear elsewhere: a value masked in this request that still goes out in the clear in
//     another new data text of the same request (the user's message, a command output, a file, the
//     system). A reader decided it in one place and another reader let it through in another: the
//     pseudonym and the real value side by side tell the model which is which. The assistant's text
//     does not count (who wrote it, docs/structures.md): the model writes a common word because it is
//     the word.
//   - common words masked: an object name that is a dictionary word ("ingestão" as an object). It may
//     be right (a `billing` table), but a rise in it is the sign of masking too much.
//
// Texts frozen from earlier requests are not looked at: what already went out is counted once, in the
// request that sent it.
type Consistency struct {
	InClear, Commons map[string]int // type (without "obj.") -> distinct values
}

// Check computes the consistency of the request from its entries (those of the frozen texts
// included: a value masked in an old text and in the clear in a new one also counts).
func (l *Batch) Check(es []Entry) Consistency {
	c := Consistency{InClear: map[string]int{}, Commons: map[string]int{}}
	var words map[string]bool // whole words of the new data texts, built once
	var texts []string
	for pos := range l.data {
		texts = append(texts, l.outputs[pos].text)
	}
	seenSet := map[string]bool{}
	for _, e := range es {
		if len(e.Real) < 4 || seenSet[e.Real] {
			continue
		}
		seenSet[e.Real] = true
		t := strings.TrimPrefix(e.Type, objTypePrefix)
		if isObject(e.Type) && commonWord(e.Real) {
			c.Commons[t]++
		}
		if len(texts) == 0 {
			continue
		}
		if oneWord(e.Real) {
			if words == nil {
				words = map[string]bool{}
				for _, s := range texts {
					for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !wordRune(r) }) {
						words[w] = true
					}
				}
			}
			if words[e.Real] {
				c.InClear[t]++
			}
			continue
		}
		for _, s := range texts {
			if hasWhole(s, e.Real) {
				c.InClear[t]++
				break
			}
		}
	}
	return c
}

func wordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func oneWord(v string) bool {
	for _, r := range v {
		if !wordRune(r) {
			return false
		}
	}
	return true
}

// hasWhole: v appears in s with no word character glued before or after it.
func hasWhole(s, v string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], v)
		if j < 0 {
			return false
		}
		a, b := i+j, i+j+len(v)
		before, _ := utf8.DecodeLastRuneInString(s[:a])
		after, _ := utf8.DecodeRuneInString(s[b:])
		if (a == 0 || !wordRune(before)) && (b == len(s) || !wordRune(after)) {
			return true
		}
		i = a + 1
	}
}
