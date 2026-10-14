package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detector of common passwords (the ones gitleaks does not catch).

// "Human" passwords: gitleaks rules catch tokens with their own format and high-entropy values, but
// let through the common database password ("Ficticia@2024"), the word "senha" in Portuguese, the
// password inside a URL and that of "mysql -pXXX". These patterns cover that. The last group of
// each one is the password.
//
// Criterion: when in doubt, mask. A Kubernetes secret name ("secretRef: db-credentials") or a path
// also go out masked; that breaks nothing (the return path undoes it) and is better than letting a
// password through. Only what is clearly code or a marker stays out.
var (
	// chave = valor   (senha: X, DB_PASSWORD=X, "password": "X", pwd => X)
	rePasswordAttr = regexp.MustCompile("(?i)(?:senha|passw(?:or)?d|pwd|secret|segredo)([\\w.\\-]*)[\"']?\\s*(?:=>|:=|=|:)\\s*[\"'`]?([^\\s\"'`,;]{4,})")
	// sentence (a senha do sistema é X, the password is X)
	rePasswordSentence = regexp.MustCompile("(?i)\\b(?:senha|password)\\b(?:[ \\t]+[\\p{L}_\\-]+){0,4}?[ \\t]+(?:é|eh|is|era|será)[ \\t]+[\"'`]?([^\\s\"'`,;]{6,})")
	// word and value between backticks or quotes, without ":" or "=" (senha `X`, password "X")
	reQuotedPassword = regexp.MustCompile("(?i)\\b(?:senha|password|passwd|pwd)\\b(?:[ \\t]+[\\p{L}_\\-]+){0,3}?[ \\t]+[`\"']([^\\s`\"']{4,})[`\"']")
	// user:password@ in a URL
	rePasswordURL = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.\\-]*://[^\\s:/@]+:([^\\s@/]{3,})@")
	// command line
	rePasswordCLI = regexp.MustCompile("(?:\\b(?:mysql|mysqldump|mysqladmin|mariadb)\\b[^\\n|;&]*?\\s-p|\\bsshpass\\s+-p\\s*|--password[= ]+|\\bcurl\\b[^\\n|;&]*?\\s(?:-u|--user)\\s+[^\\s:]+:)[\"']?([^\\s\"'`]{4,})")
)

var reFileSuffix = regexp.MustCompile(`\.\w{1,5}$`)

var notPassword = map[string]bool{"null": true, "none": true, "nil": true, "true": true, "false": true, "undefined": true,
	"string": true, "str": true, "varchar": true, "text": true, "required": true, "optional": true}

// looksLikePassword tells a literal value from a piece of code (variable, call, type). weak=true
// accepts any literal (positions where only a password fits, as in a URL).

func looksLikePassword(v string, weak bool) bool {
	if notPassword[strings.ToLower(v)] || strings.ContainsAny(v[:1], "$<{%*|=") || strings.ContainsAny(v, "()[]") {
		return false
	}
	if strings.HasPrefix(v, "SECRET-") || strings.HasPrefix(v, "SEGREDO-") || strings.Trim(v, "*x.#-") == "" {
		return false // already masked, or "*****"
	}
	if weak {
		return true
	}
	if d := digitsOnly(v); len(d) < 6 && len(strings.Trim(v, "0123456789-")) == 0 {
		return false // short number: file line, port, code
	}
	// at least one digit or symbol: "self.password", "args.senha" and "obrigatória" do not count
	for _, r := range v {
		if unicode.IsDigit(r) || (r < 128 && !unicode.IsLetter(r) && r != '_' && r != '.') {
			return true
		}
	}
	return false
}

func (m *Masker) findPasswords(s, lower string, add func(ini, end int, typ string)) {
	look := func(re *regexp.Regexp, weak bool) {
		for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
			ini, end := ix[len(ix)-2], ix[len(ix)-1]
			if re == rePasswordAttr && reFileSuffix.MatchString(s[ix[2]:ix[3]]) {
				continue // file name: "PasswordEncoder.java:123", "secrets.yaml: ..."
			}
			// sentence punctuation, not the password's. In a URL, no: the password goes up to the
			// "@" (Senha!@host)
			for re != rePasswordURL && end > ini && strings.ContainsRune(".,;:!?)]}", rune(s[end-1])) {
				end--
			}
			if end-ini >= 3 && looksLikePassword(s[ini:end], weak) {
				add(ini, end, "secret")
			}
		}
	}
	if strings.Contains(lower, "senha") || strings.Contains(lower, "passw") || strings.Contains(lower, "pwd") ||
		strings.Contains(lower, "secret") || strings.Contains(lower, "segredo") {
		look(rePasswordAttr, false)
		look(rePasswordSentence, false)
		look(reQuotedPassword, false)
		look(rePasswordCLI, false)
	} else if strings.Contains(s, " -p") || strings.Contains(s, " -u ") || strings.Contains(s, "--user ") {
		look(rePasswordCLI, false)
	}
	if strings.Contains(s, "://") {
		look(rePasswordURL, true)
	}
}
