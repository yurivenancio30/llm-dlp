package mask

import (
	"crypto/sha256"
	_ "embed"
	"regexp"
	"strings"
)

// Authentication data. Resolução CD/ANPD nº 15/2024, art. 5º, IV, lists "dados de autenticação em
// sistemas" among the data that make a security incident a relevant risk. Most of them are short
// values that only the label next to them tells apart from any other number:
//   - card security code (CVV/CVC) and PIN: the "sensitive authentication data" of PCI DSS v4.0
//     (PCI Security Standards Council, glossary);
//   - one-time codes (HOTP and TOTP, RFC 4226 and RFC 6238: 6 to 8 digits) and verification codes;
//   - a password made of digits only (the password detector leaves short numbers alone);
//   - recovery codes and the answer to a security question;
//   - a wallet recovery phrase (BIP-39), also recognized by the data itself: 12 to 24 words of the
//     official list with a valid checksum.
// All of them are masked as "secret", like passwords and tokens.

var (
	// label, up to 4 words, an optional separator and the digits:
	// "PIN: 4821", "CVV 123", "o código de verificação é 482913", "senha do cartão 4821"
	reAuthCode = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_.\-])(` +
		`pin|cvv2?|cvc2?|csc|cav2|c[oó]digo\s+de\s+seguran[cç]a|security\s+code|card\s+verification(?:\s+(?:code|value))?|` +
		`otp|totp|hotp|2fa|mfa|c[oó]digo\s+(?:de\s+)?(?:verifica[cç][aã]o|autentica[cç][aã]o|acesso|confirma[cç][aã]o)|` +
		`verification\s+code|auth(?:entication)?\s+code|access\s+code|one[\s-]time\s+(?:pass)?code|one[\s-]time\s+password|` +
		`token\s+(?:sms|otp|2fa|mfa|de\s+acesso|de\s+verifica[cç][aã]o)|senha|password|passcode` +
		`)((?:[ \t]+[\pL\-]+){0,4}?)[ \t]*(=>|:|=|é|eh|is|was|era)?[ \t]*["'\x60]?(\d{3,12})(?:[^\pL\pN\-]|$)`)
	// "token: 482913": a bare token label counts only with a separator and the size of a one-time code
	reAuthToken = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_.\-])token[ \t]*[:=][ \t]*["']?(\d{6,8})(?:[^\pL\pN\-]|$)`)
	// recovery or backup codes: each code after the label
	reRecoveryCodes = regexp.MustCompile(`(?i)\b(?:c[oó]digos?\s+de\s+(?:recupera[cç][aã]o|backup|reserva|emerg[eê]ncia)|recovery\s+codes?|backup\s+codes?)\b[^\n:=]{0,20}[:=][ \t]*([^\n;|]+)`)
	reRecoveryCode  = regexp.MustCompile(`[\pL\pN][\pL\pN\-]{3,}`)
	// answer to a security question, and a recovery phrase given after its label (up to the end of
	// the line, a ";" or a "|"; the end of the sentence is cut in findAuth)
	reSecretAnswer = regexp.MustCompile(`(?i)\b(?:resposta\s+(?:secreta|de\s+seguran[cç]a|[aà]\s+pergunta\s+(?:secreta|de\s+seguran[cç]a))|` +
		`security\s+answer|secret\s+answer|answer\s+to\s+(?:the\s+)?security\s+question|` +
		`frase\s+(?:de\s+recupera[cç][aã]o|semente|secreta|mnem[oô]nica)|palavras\s+(?:de\s+recupera[cç][aã]o|secretas)|` +
		`(?:secret\s+)?recovery\s+phrase|seed\s+phrase|seed\s+words|mnemonic(?:\s+phrase)?)\b[ \t]*[:=][ \t]*["'\x60]?([^\n;|]+)`)
)

// authDigits: how many digits the value may have, by label. A card security code (CVV, CVC, CSC,
// CAV2) has 3 or 4; "security code" and "card verification" also name codes sent by SMS, so 3 or
// more; a PIN, a numeric password and a one-time code, 4 or more (RFC 4226 asks for at least 6;
// some services send 4 or 5).
func authDigits(label string) (min, max int) {
	l := strings.ToLower(label)
	switch {
	case strings.HasPrefix(l, "cvv"), strings.HasPrefix(l, "cvc"), l == "csc", l == "cav2":
		return 3, 4
	case strings.Contains(l, "seguran"), strings.Contains(l, "security"), strings.Contains(l, "card"):
		return 3, 12
	}
	return 4, 12
}

func (m *Masker) findAuth(s, lower string, add func(ini, end int, typ string)) {
	if strings.ContainsAny(lower, "0123456789") && containsAny(lower, "pin", "cvv", "cvc", "csc", "cav2", "código", "codigo", "code",
		"otp", "2fa", "mfa", "token", "senha", "password", "passcode") {
		for _, ix := range reAuthCode.FindAllStringSubmatchIndex(s, -1) {
			label, sep, v := s[ix[2]:ix[3]], ix[6] >= 0, s[ix[8]:ix[9]]
			if lo, hi := authDigits(label); len(v) < lo || len(v) > hi {
				continue
			}
			// without ":"/"=" or "é"/"is", a year after a preposition is a date ("a senha de 2024")
			if !sep && len(v) == 4 && (strings.HasPrefix(v, "19") || strings.HasPrefix(v, "20")) {
				continue
			}
			add(ix[8], ix[9], "secret")
		}
		if strings.Contains(lower, "token") {
			for _, ix := range reAuthToken.FindAllStringSubmatchIndex(s, -1) {
				add(ix[2], ix[3], "secret")
			}
		}
	}
	if containsAny(lower, "recupera", "recovery", "backup", "reserva", "emerg") {
		for _, ix := range reRecoveryCodes.FindAllStringSubmatchIndex(s, -1) {
			base := ix[2]
			for _, c := range reRecoveryCode.FindAllStringIndex(s[ix[2]:ix[3]], -1) {
				if strings.ContainsAny(s[base+c[0]:base+c[1]], "0123456789") {
					add(base+c[0], base+c[1], "secret")
				}
			}
		}
	}
	if containsAny(lower, "resposta", "answer", "frase", "palavras", "phrase", "seed", "mnem") {
		for _, ix := range reSecretAnswer.FindAllStringSubmatchIndex(s, -1) {
			ini, end := ix[2], ix[3]
			if i := strings.Index(s[ini:end], ". "); i >= 0 {
				end = ini + i
			}
			for end > ini && strings.ContainsRune(" \t\r.,;!?\"'`", rune(s[end-1])) {
				end--
			}
			if end-ini >= 2 {
				add(ini, end, "secret")
			}
		}
	}
	m.findRecoveryPhrase(s, add)
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// BIP-39 wordlists (MIT License; origin in each file's header). The position of a word is its
// 11-bit index.
//
//go:embed data/bip39_english.txt
var bip39EnglishTxt string

//go:embed data/bip39_portuguese.txt
var bip39PortugueseTxt string

var bip39Lists = []map[string]int{readBIP39(bip39EnglishTxt), readBIP39(bip39PortugueseTxt)}

func readBIP39(txt string) map[string]int {
	m := make(map[string]int, 2048)
	for _, l := range strings.Split(txt, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			m[l] = len(m)
		}
	}
	return m
}

// findRecoveryPhrase: 12, 15, 18, 21 or 24 words in a row from the same BIP-39 list, whose checksum
// matches (BIP-39: the first bits of the SHA-256 of the entropy). Between two words, only spaces,
// line breaks, commas or a numbering ("1. abandon 2. ability"). Without the checksum (a mistyped
// phrase), only the label catches it (reSecretAnswer).
func (m *Masker) findRecoveryPhrase(s string, add func(ini, end int, typ string)) {
	if strings.Count(s, " ")+strings.Count(s, "\n") < 11 {
		return
	}
	type word struct{ ini, end, idx int }
	for _, list := range bip39Lists {
		var run []word
		check := func() {
			for n := 24; n >= 12; n -= 3 {
				for i := 0; i+n <= len(run); i++ {
					idx := make([]int, n)
					for k := range idx {
						idx[k] = run[i+k].idx
					}
					if bip39Checksum(idx) {
						add(run[i].ini, run[i+n-1].end, "secret")
						i += n - 1
					}
				}
			}
		}
		prevEnd := -1
		for i := 0; i < len(s); {
			if !isASCIILetter(s[i]) {
				i++
				continue
			}
			j := i
			for j < len(s) && isASCIILetter(s[j]) {
				j++
			}
			// a word glued to other characters ("abc_def", "x@y") is not a phrase word
			glued := i > 0 && (s[i-1] == '_' || s[i-1] == '@' || s[i-1] == '/' || s[i-1] >= 0x80) ||
				j < len(s) && (s[j] == '_' || s[j] == '@' || s[j] == '/' || s[j] >= 0x80)
			idx, ok := 0, false
			if j-i >= 3 && j-i <= 8 { // every BIP-39 word has 3 to 8 letters
				idx, ok = list[strings.ToLower(s[i:j])]
			}
			if !ok || glued || prevEnd >= 0 && !phraseGap(s[prevEnd:i]) {
				if len(run) >= 12 {
					check()
				}
				run = run[:0]
			}
			if ok && !glued {
				run = append(run, word{i, j, idx})
				prevEnd = j
			} else {
				prevEnd = -1
			}
			i = j
		}
		if len(run) >= 12 {
			check()
		}
	}
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// phraseGap: what may separate two words of a recovery phrase.
func phraseGap(g string) bool {
	if len(g) == 0 || len(g) > 8 {
		return false
	}
	for i := 0; i < len(g); i++ {
		if !strings.ContainsRune(" \t\r\n,;.)-:0123456789", rune(g[i])) {
			return false
		}
	}
	return true
}

// bip39Checksum: the words' indices (11 bits each) are the entropy followed by a checksum of
// len/3 bits, which must be the first bits of the SHA-256 of the entropy.
func bip39Checksum(idx []int) bool {
	bits := make([]byte, 0, len(idx)*11)
	for _, v := range idx {
		for b := 10; b >= 0; b-- {
			bits = append(bits, byte(v>>b&1))
		}
	}
	cs := len(bits) / 33
	ent := bits[:len(bits)-cs]
	entropy := make([]byte, len(ent)/8)
	for i, b := range ent {
		entropy[i/8] |= b << (7 - i%8)
	}
	h := sha256.Sum256(entropy)
	for i := 0; i < cs; i++ {
		if bits[len(ent)+i] != h[i/8]>>(7-i%8)&1 {
			return false
		}
	}
	return true
}
