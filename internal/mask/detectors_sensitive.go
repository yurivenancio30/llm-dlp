package mask

import (
	"encoding/base64"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// LGPD sensitive personal data (art. 5, II) written in the middle of the text: color or race,
// religion, sexual orientation and gender identity, political party, trade union and biometrics.
// With the field name ("religiao: X") the detection by field takes care of it (fields.go); here it
// is the sentence ("o paciente é evangélico", "filiado ao PSOL", "mulher parda"). It masks only the
// value, and only after an expression that ties the value to someone: the word alone ("tela preta",
// "igreja católica do bairro") stays. The values come from the official lists cited in each one.

// unaccentedRE: the value's text as a regex that accepts it with and without accents.
func unaccentedRE(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		switch r {
		case 'a', 'á', 'à', 'â', 'ã':
			b.WriteString("[aáàâã]")
		case 'e', 'é', 'ê':
			b.WriteString("[eéê]")
		case 'i', 'í':
			b.WriteString("[ií]")
		case 'o', 'ó', 'ô', 'õ':
			b.WriteString("[oóôõ]")
		case 'u', 'ú', 'ü':
			b.WriteString("[uúü]")
		case 'c', 'ç':
			b.WriteString("[cç]")
		case ' ':
			b.WriteString(`[\s-]+`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}

// alternatives: the forms as a regex, the longest first (in an alternation the first one wins).
func alternatives(vs []string, fn func(string) string) string {
	vs = append([]string(nil), vs...)
	sort.SliceStable(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	ps := make([]string, len(vs))
	for i, v := range vs {
		ps[i] = fn(v)
	}
	return strings.Join(ps, "|")
}

// inflections: "católic" -> católico, católica, católicos, católicas.
func inflections(radicais ...string) []string {
	var out []string
	for _, r := range radicais {
		out = append(out, r+"o", r+"a", r+"os", r+"as")
	}
	return out
}

// color or race: the five IBGE categories (branca, preta, parda, amarela, indígena) and "negra",
// which IBGE uses as the sum of pretos and pardos.
var raceColorValues = append(inflections("branc", "pret", "pard", "amarel", "negr"), "indígena", "indígenas")

// religion: the groups of the IBGE Census religion classification (2010, annex 7), as an adjective
// of the follower and as a noun; and the churches the classification names.
var religionValues = append(append(inflections("católic", "evangélic", "luteran", "presbiterian", "anglican",
	"agnóstic", "muçulman", "islâmic", "judaic"),
	"protestante", "protestantes", "pentecostal", "pentecostais", "neopentecostal", "neopentecostais",
	"metodista", "metodistas", "batista", "batistas", "adventista", "adventistas", "menonita", "menonitas",
	"congregacional", "espírita", "espíritas", "kardecista", "kardecistas", "espiritualista", "espiritualistas",
	"umbandista", "umbandistas", "candomblecista", "candomblecistas", "budista", "budistas", "hinduísta",
	"hinduístas", "xintoísta", "taoísta", "judeu", "judeus", "judia", "judias", "ateu", "ateus", "ateia", "ateias",
	"mórmon", "mórmons", "testemunha de jeová", "testemunhas de jeová", "sem religião",
	"umbanda", "candomblé", "judaísmo", "islamismo", "budismo", "hinduísmo", "espiritismo", "catolicismo",
	"católica apostólica romana", "católica apostólica brasileira", "católica ortodoxa"),
	churchesIBGE...)

var churchesIBGE = []string{"assembleia de deus", "congregação cristã do brasil", "universal do reino de deus",
	"evangelho quadrangular", "deus é amor", "o brasil para cristo", "casa da bênção", "casa de oração",
	"maranata", "comunidade cristã", "nova vida", "igreja mundial do poder de deus", "renascer em cristo",
	"internacional da graça de deus", "igreja do nazareno", "exército da salvação", "legião da boa vontade",
	"santos dos últimos dias", "igreja messiânica mundial", "seicho-no-ie", "hare krishna", "soka gakkai",
	"santo daime", "união do vegetal"}

// sexual orientation: the options of the National Health Survey (IBGE, 2019: heterossexual,
// homossexual, bissexual) and of the e-SUS APS registration form (Ministry of Health: gay, lésbica,
// pansexual).
var orientationValues = []string{"heterossexual", "heterossexuais", "homossexual", "homossexuais", "bissexual",
	"bissexuais", "pansexual", "pansexuais", "gay", "gays", "lésbica", "lésbicas"}

// gender identity: the e-SUS APS options (cisgender and transgender man/woman, travesti,
// non-binary) and the short forms "homem trans", "mulher trans".
var genderValues = []string{"homem cisgênero", "mulher cisgênero", "homem transgênero", "mulher transgênero",
	"homem trans", "mulher trans", "cisgênero", "cisgêneros", "transgênero", "transgêneros", "transexual",
	"transexuais", "travesti", "travestis", "não-binário", "não-binária", "não-binários", "não-binárias",
	"não binário", "não binária"}

// parties: acronyms and names of the party bodies in TSE open data (including extinct ones, for old
// memberships). Acronym only in uppercase (PT, PL, PP are also other things); a name that is also a
// common word ("New", "Rede", "Avante") only with an initial capital or all uppercase.
var partyAcronyms = []string{"AGIR", "AVANTE", "CIDADANIA", "DC", "DEM", "DEMOCRATA", "MDB", "MISSÃO", "MISSAO",
	"MOBILIZA", "NOVO", "PATRIOTA", "PCB", "PCdoB", "PCDOB", "PC do B", "PCO", "PDT", "PHS", "PL", "PODE", "PODEMOS",
	"PP", "PPL", "PRD", "PROS", "PRP", "PRTB", "PSB", "PSC", "PSDB", "PSD", "PSL", "PSOL", "PSTU", "PTB", "PT", "PV",
	"REDE", "REPUBLICANOS", "SOLIDARIEDADE", "UNIÃO", "UNIAO", "UNIÃO BRASIL", "UP", "PROGRESSISTAS",
	"Agir", "Avante", "Cidadania", "Democrata", "Democratas", "Missão", "Mobiliza", "Novo", "Patriota", "Podemos",
	"Progressistas", "Rede", "Republicanos", "Solidariedade", "União Brasil"}

var partyNames = []string{"democracia cristã", "movimento democrático brasileiro", "partido missão",
	"mobilização nacional", "partido novo", "partido comunista brasileiro", "partido comunista do brasil",
	"partido da causa operária", "partido democrático trabalhista", "partido humanista da solidariedade",
	"partido liberal", "partido pátria livre", "partido renovação democrática", "partido republicano da ordem social",
	"partido republicano progressista", "partido renovador trabalhista brasileiro", "partido socialista brasileiro",
	"partido social cristão", "partido da social democracia brasileira", "partido social democrático",
	"partido social liberal", "partido socialismo e liberdade", "partido socialista dos trabalhadores unificado",
	"partido trabalhista brasileiro", "partido dos trabalhadores", "partido verde", "rede sustentabilidade",
	"unidade popular"}

// whoDeclares: the expression of someone declaring about themselves ("se declara",
// "autodeclarado"): the only one that counts for color or race, because "é preta" is also the color
// of a screen.
const whoDeclares = `(?:se\s+(?:auto)?(?:declara|declarou|declaram|considera|considerou|identifica|identificou)(?:\s+como)?|(?:auto)?declarad[oa]s?(?:\s+como)?)`

// whoIs: the expression that ties the value to a person ("é", "se declara", "tornou-se"), for the
// values that have no other meaning ("católico", "bissexual", "travesti").
const whoIs = `(?:é|sou|somos|são|era|eram|foi|fui|seja|sejam|se\s+(?:auto)?(?:declara|declarou|declaram|considera|considerou|diz|disse|identifica|identificou|assume|assumiu|tornou|converteu)(?:\s+(?:como|ser|ao|à|a|para\s+[oa]))?|tornou-se|converteu-se(?:\s+(?:ao|à|a|para\s+[oa]))?|convertid[oa]s?\s+(?:ao|à|a)|(?:auto)?declarad[oa]s?(?:\s+como)?|assumidamente|assumid[oa]s?)`

// person: nouns that describe someone ("mulher parda", "paciente evangélico").
const personRE = `(?:homem|homens|mulher|mulheres|pessoa|pessoas|paciente|pacientes|cidad[ãa]o|cidad[ãa]os|cidad[ãa]s?|jovem|jovens|crian[cç]as?|idos[oa]s?|menor|menores|indiv[ií]duos?|rapaz|rapazes|mo[cç]as?|senhora?s?|v[ií]timas?|suspeit[oa]s?|cliente|clientes|funcion[aá]ri[oa]s?|aluno|aluna|alunos|alunas|casal|casais|filh[oa]s?|m[ãa]e|pai|esposa|marido|candidat[oa]s?|eleitor[a]?|eleitores|colaborador[a]?|empregad[oa]s?|segurad[oa]s?|benefici[aá]ri[oa]s?)`

type sensitiveRule struct {
	re *regexp.Regexp
	// clues: pieces (lowercase, unaccented) of which at least one has to be in the text
	clues []string
}

var sensitiveRules = func() []sensitiveRule {
	fa := func(vs []string) string { return alternatives(vs, unaccentedRE) }
	exactly := func(vs []string) string { return alternatives(vs, regexp.QuoteMeta) }
	// the value is group 1; before it, the expression; after it, the value cannot continue (the
	// edge is checked in findSensitive, because Go's \b does not know accented letters)
	r := func(before, value string, clues ...string) sensitiveRule {
		return sensitiveRule{regexp.MustCompile(`(?:^|[^\p{L}\p{N}_])(?:` + before + `)[\s:=]+(` + value + `)`), clues}
	}
	colorLabel := `(?i:ra[cç]a\s*(?:/|ou|e)\s*cor|cor\s*(?:/|ou|e)\s*ra[cç]a|ra[cç]a|etnia|cor\s+da\s+pele)\s*[:=]?\s*(?:é|e)?`
	relLabel := `(?i:religi[ãa]o|de\s+religi[ãa]o|cren[cç]a\s+religiosa|credo|praticantes?\s+d[aoe]|seguidor(?:a|es|as)?\s+d[aoe]|adept[oa]s?\s+d[aoe]|fi[eé]is?\s+d[aoe]|membros?\s+d[aoe](?:\s+igreja)?|frequenta(?:dor[a]?)?\s+(?:a|o|d[ao])(?:\s+igreja)?|congrega\s+n[ao](?:\s+igreja)?)`
	origLabel := `(?i:orienta[cç][ãa]o\s+sexual\s*[:=]?\s*(?:é|e)?|casal|casais)`
	genLabel := `(?i:identidade\s+de\s+g[eê]nero\s*[:=]?\s*(?:é|e)?)`
	partyVerb := `(?i:filiad[oa]s?\s+(?:ao|à|a|no|na)(?:\s+partido)?|filia[cç][ãa]o\s+(?:partid[aá]ria\s*)?(?:[:=]|ao|à|a|no|na)?|partid[aá]ria\s*[:=]|milit(?:a|ante|antes|ou|ava)\s+(?:no|na|do|da|pelo|pela)|candidat[oa]s?\s+(?:pelo|pela|do|da)|eleit(?:or|ora|ores|oras)\s+(?:do|da)|vot(?:a|ou|o|ei|amos|aram|ava|ará)\s+(?:no|na|em)|apoiador(?:a|es|as)?\s+(?:do|da)|simpatizantes?\s+(?:do|da)|membros?\s+(?:do|da)|partido(?:\s+pol[ií]tico)?\s*[:=])`
	return []sensitiveRule{
		{regexp.MustCompile(`(?:^|[^\p{L}\p{N}_])(?i:` + whoDeclares + `|` + personRE + `)\s+(` + `(?i:` + fa(raceColorValues) + `))`),
			[]string{"branc", "pret", "pard", "amarel", "negr", "indigena", "indígena"}},
		r(colorLabel, `(?i:`+fa(raceColorValues)+`)`, "branc", "pret", "pard", "amarel", "negr", "indigena", "indígena"),
		r(`(?i:`+whoIs+`)`, `(?i:`+fa(religionValues)+`)`, "catol", "católi", "evang", "protest", "pentecost", "luteran",
			"presbit", "anglican", "metodist", "batist", "adventist", "menonit", "congreg", "espirit", "espírit", "kardec",
			"umband", "candombl", "budis", "hindu", "xinto", "taois", "taoís", "jude", "judi", "judai", "ateu", "ateia",
			"agnost", "agnóst", "muçulm", "muculm", "islam", "islâm", "mórmon", "mormon", "jeov", "sem relig"),
		r(relLabel, `(?i:`+fa(religionValues)+`)`, "relig", "credo", "crença", "crenca", "praticante", "seguidor", "adept",
			"fiel", "fiéis", "membro", "frequenta", "congrega", "igreja"),
		r(`(?i:`+whoIs+`)|`+origLabel, `(?i:`+fa(orientationValues)+`)`, "sexua", "gay", "lésbic", "lesbic"),
		r(`(?i:`+whoIs+`|`+personRE+`)|`+genLabel, `(?i:`+fa(genderValues)+`)`, "gênero", "genero", "trans", "travesti", "binári", "binari"),
		r(partyVerb, exactly(partyAcronyms)+`|(?i:`+fa(partyNames)+`)`, "filia", "milit", "candidat", "eleitor", "vot",
			"apoiador", "simpatizante", "membro", "partid"),
	}
}()

// trade union: the name of the union after someone who is a member of it ("sindicalizado no
// Sindicato dos Metalúrgicos do ABC"). Union membership is sensitive data; the name alone is not.
var reUnion = regexp.MustCompile(`(?i:sindicalizad[oa]s?|filiad[oa]s?|associad[oa]s?|membros?|delegad[oa]s?\s+sindica(?:l|is)|dirigentes?|diretor[a]?|presidente)\s+(?:a|ao|à|no|na|do|da|de)\s+(Sindicato|SINDICATO)\s`)

var nameConnectors = setOf("de", "da", "do", "das", "dos", "e", "em", "no", "na", "nos", "nas", "a", "o")

// unionNameEnd: where "Sindicato dos Metalúrgicos do ABC" ends: capitalized words, acronyms and the
// connectors between them.
func unionNameEnd(s string, p int) int {
	end := p
	for q := p; q < len(s); {
		for q < len(s) && s[q] == ' ' {
			q++
		}
		r := q
		for r < len(s) && !strings.ContainsRune(" \t\n\r,;.:()[]{}\"'!?/", rune(s[r])) {
			r++
		}
		if r == q {
			break
		}
		w := s[q:r]
		c, _ := utf8.DecodeRuneInString(w)
		switch {
		case nameConnectors[strings.ToLower(w)]:
		case unicode.IsUpper(c) || unicode.IsDigit(c):
			end = r
		default:
			return end
		}
		q = r
	}
	return end
}

// biometrics: the fingerprint template in the minutiae interchange format (ISO/IEC 19794-2 and
// ANSI/INCITS 378: the record starts with "FMR\0"), which usually goes in base64 in a JSON or a
// column. In base64, "FMR\0" starts with "Rk1SA".
func findBiometricTemplate(s string, add func(ini, end int, typ string)) {
	b64 := func(c byte) bool { return isAlnum(c) || c == '+' || c == '/' || c == '=' }
	for i := 0; ; {
		j := strings.Index(s[i:], "Rk1SA")
		if j < 0 {
			return
		}
		ini := i + j
		end := ini
		for end < len(s) && b64(s[end]) {
			end++
		}
		i = end
		if ini > 0 && b64(s[ini-1]) || end-ini < 32 {
			continue
		}
		// the first 8 characters are the first 6 bytes of the record
		if d, err := base64.StdEncoding.DecodeString(s[ini : ini+8]); err == nil && strings.HasPrefix(string(d), "FMR\x00") {
			add(ini, end, "sensitive")
		}
	}
}

// findSensitive runs the rules of this file. lower: s in lowercase.
func findSensitive(s, lower string, add func(ini, end int, typ string)) {
	letter := func(i int) bool { // s[i] starts a letter or digit (also accented)
		return i < len(s) && (isAlnum(s[i]) || s[i] >= 0xC3 || s[i] == '_')
	}
	for _, rg := range sensitiveRules {
		has := false
		for _, p := range rg.clues {
			if strings.Contains(lower, p) {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		for _, ix := range rg.re.FindAllStringSubmatchIndex(s, -1) {
			if ix[2] >= 0 && !letter(ix[3]) {
				add(ix[2], ix[3], "sensitive")
			}
		}
	}
	if strings.Contains(lower, "sindicato") {
		for _, ix := range reUnion.FindAllStringSubmatchIndex(s, -1) {
			if end := unionNameEnd(s, ix[3]); end > ix[3] {
				add(ix[2], end, "sensitive")
			}
		}
	}
	if strings.Contains(s, "Rk1SA") {
		findBiometricTemplate(s, add)
	}
}
