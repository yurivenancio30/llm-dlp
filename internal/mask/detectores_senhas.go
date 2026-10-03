package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detector de senhas comuns (as que o gitleaks não pega).

// Senhas "de gente": as regras do gitleaks pegam tokens com formato próprio e valores de
// alta entropia, mas deixam passar a senha comum de banco ("Ficticia@2024"), a palavra
// "senha" em português, a senha dentro de URL e a do "mysql -pXXX". Estes padrões cobrem
// isso. O último grupo de cada um é a senha.
//
// Critério: na dúvida, mascara. Um nome de segredo do Kubernetes ("secretRef: db-credentials")
// ou um caminho também saem mascarados; isso não quebra nada (a volta desfaz) e é melhor do
// que deixar uma senha passar. Só fica de fora o que claramente é código ou marcador.
var (
	// chave = valor   (senha: X, DB_PASSWORD=X, "password": "X", pwd => X)
	reSenhaAtrib = regexp.MustCompile("(?i)(?:senha|passw(?:or)?d|pwd|secret|segredo)([\\w.\\-]*)[\"']?\\s*(?:=>|:=|=|:)\\s*[\"'`]?([^\\s\"'`,;]{4,})")
	// frase   (a senha do banco é X, the password is X)
	reSenhaFrase = regexp.MustCompile("(?i)\\b(?:senha|password)\\b(?:[ \\t]+[\\p{L}_\\-]+){0,4}?[ \\t]+(?:é|eh|is|era|será)[ \\t]+[\"'`]?([^\\s\"'`,;]{6,})")
	// usuário:senha@ numa URL
	reSenhaURL = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.\\-]*://[^\\s:/@]+:([^\\s@/]{3,})@")
	// linha de comando
	reSenhaCLI = regexp.MustCompile("(?:\\b(?:mysql|mysqldump|mysqladmin|mariadb)\\b[^\\n|;&]*?\\s-p|\\bsshpass\\s+-p\\s*|--password[= ]+|\\bcurl\\b[^\\n|;&]*?\\s(?:-u|--user)\\s+[^\\s:]+:)[\"']?([^\\s\"'`]{4,})")
)

var reSufixoArquivo = regexp.MustCompile(`\.\w{1,5}$`)

var naoESenha = map[string]bool{"null": true, "none": true, "nil": true, "true": true, "false": true, "undefined": true,
	"string": true, "str": true, "varchar": true, "text": true, "required": true, "optional": true}

// pareceSenha separa um valor literal de um pedaço de código (variável, chamada, tipo).
// fraca=true aceita qualquer literal (posições em que só cabe uma senha, como numa URL).

func pareceSenha(v string, fraca bool) bool {
	if naoESenha[strings.ToLower(v)] || strings.ContainsAny(v[:1], "$<{%*|=") || strings.ContainsAny(v, "()[]") {
		return false
	}
	if strings.HasPrefix(v, "SEGREDO-") || strings.Trim(v, "*x.#-") == "" {
		return false // já mascarado, ou "*****"
	}
	if fraca {
		return true
	}
	if d := soDigitos(v); len(d) < 6 && len(strings.Trim(v, "0123456789-")) == 0 {
		return false // número curto: linha de arquivo, porta, código
	}
	// pelo menos um dígito ou símbolo: "self.password", "args.senha" e "obrigatória" não contam
	for _, r := range v {
		if unicode.IsDigit(r) || (r < 128 && !unicode.IsLetter(r) && r != '_' && r != '.') {
			return true
		}
	}
	return false
}

func (m *Masker) acharSenhas(s, baixo string, add func(ini, fim int, tipo string)) {
	olhar := func(re *regexp.Regexp, fraca bool) {
		for _, ix := range re.FindAllStringSubmatchIndex(s, -1) {
			ini, fim := ix[len(ix)-2], ix[len(ix)-1]
			if re == reSenhaAtrib && reSufixoArquivo.MatchString(s[ix[2]:ix[3]]) {
				continue // nome de arquivo: "PasswordEncoder.java:123", "secrets.yaml: ..."
			}
			for fim > ini && strings.ContainsRune(".,;:!?)]}", rune(s[fim-1])) {
				fim-- // pontuação da frase, não da senha
			}
			if fim-ini >= 3 && pareceSenha(s[ini:fim], fraca) {
				add(ini, fim, "segredo")
			}
		}
	}
	if strings.Contains(baixo, "senha") || strings.Contains(baixo, "passw") || strings.Contains(baixo, "pwd") ||
		strings.Contains(baixo, "secret") || strings.Contains(baixo, "segredo") {
		olhar(reSenhaAtrib, false)
		olhar(reSenhaFrase, false)
		olhar(reSenhaCLI, false)
	} else if strings.Contains(s, " -p") || strings.Contains(s, " -u ") || strings.Contains(s, "--user ") {
		olhar(reSenhaCLI, false)
	}
	if strings.Contains(s, "://") {
		olhar(reSenhaURL, true)
	}
}
