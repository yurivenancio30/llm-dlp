package mask

import (
	"encoding/base64"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Dados pessoais sensíveis da LGPD (art. 5º, II) escritos no meio do texto: cor ou raça,
// religião, orientação sexual e identidade de gênero, partido político, sindicato e biometria.
// Com o nome do campo ("religiao: X") quem cuida é a detecção pelo campo (campos.go); aqui é a
// frase ("o paciente é evangélico", "filiado ao PSOL", "mulher parda"). Mascara só o valor, e só
// depois de uma expressão que liga o valor a alguém: a palavra sozinha ("tela preta", "igreja
// católica do bairro") fica. Os valores vêm das listas oficiais citadas em cada uma.

// unaccentedRE: o texto do valor como regex que aceita com e sem acento.
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

// alternativas: as formas como regex, as mais longas primeiro (na alternância vale a primeira).
func alternatives(vs []string, fn func(string) string) string {
	vs = append([]string(nil), vs...)
	sort.SliceStable(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	ps := make([]string, len(vs))
	for i, v := range vs {
		ps[i] = fn(v)
	}
	return strings.Join(ps, "|")
}

// flexoes: "católic" -> católico, católica, católicos, católicas.
func inflections(radicais ...string) []string {
	var out []string
	for _, r := range radicais {
		out = append(out, r+"o", r+"a", r+"os", r+"as")
	}
	return out
}

// cor ou raça: as cinco categorias do IBGE (branca, preta, parda, amarela, indígena) e "negra",
// que o IBGE usa como a soma de pretos e pardos.
var raceColorValues = append(inflections("branc", "pret", "pard", "amarel", "negr"), "indígena", "indígenas")

// religião: os grupos da classificação de religiões do Censo do IBGE (2010, anexo 7), como
// adjetivo de quem segue e como nome; e as igrejas que a classificação nomeia.
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

// orientação sexual: as opções da Pesquisa Nacional de Saúde (IBGE, 2019: heterossexual,
// homossexual, bissexual) e da ficha de cadastro do e-SUS APS (Ministério da Saúde: gay, lésbica,
// pansexual).
var orientationValues = []string{"heterossexual", "heterossexuais", "homossexual", "homossexuais", "bissexual",
	"bissexuais", "pansexual", "pansexuais", "gay", "gays", "lésbica", "lésbicas"}

// identidade de gênero: as opções do e-SUS APS (homem/mulher cisgênero e transgênero, travesti,
// não-binário) e as formas curtas "homem trans", "mulher trans".
var genderValues = []string{"homem cisgênero", "mulher cisgênero", "homem transgênero", "mulher transgênero",
	"homem trans", "mulher trans", "cisgênero", "cisgêneros", "transgênero", "transgêneros", "transexual",
	"transexuais", "travesti", "travestis", "não-binário", "não-binária", "não-binários", "não-binárias",
	"não binário", "não binária"}

// partidos: siglas e nomes dos órgãos partidários nos dados abertos do TSE (inclusive os extintos,
// para filiação antiga). Sigla só em maiúsculas (PT, PL, PP também são outras coisas); nome que
// também é palavra comum ("New", "Rede", "Avante") só com a primeira maiúscula ou tudo em maiúsculas.
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

// whoDeclares: a expressão de quem declara de si ("se declara", "autodeclarado"): a única que
// vale para cor ou raça, porque "é preta" também é a cor de uma tela.
const whoDeclares = `(?:se\s+(?:auto)?(?:declara|declarou|declaram|considera|considerou|identifica|identificou)(?:\s+como)?|(?:auto)?declarad[oa]s?(?:\s+como)?)`

// whoIs: a expressão que liga o valor a uma pessoa ("é", "se declara", "tornou-se"), para os
// valores que não têm outro sentido ("católico", "bissexual", "travesti").
const whoIs = `(?:é|sou|somos|são|era|eram|foi|fui|seja|sejam|se\s+(?:auto)?(?:declara|declarou|declaram|considera|considerou|diz|disse|identifica|identificou|assume|assumiu|tornou|converteu)(?:\s+(?:como|ser|ao|à|a|para\s+[oa]))?|tornou-se|converteu-se(?:\s+(?:ao|à|a|para\s+[oa]))?|convertid[oa]s?\s+(?:ao|à|a)|(?:auto)?declarad[oa]s?(?:\s+como)?|assumidamente|assumid[oa]s?)`

// pessoa: substantivos que descrevem alguém ("mulher parda", "paciente evangélico").
const personRE = `(?:homem|homens|mulher|mulheres|pessoa|pessoas|paciente|pacientes|cidad[ãa]o|cidad[ãa]os|cidad[ãa]s?|jovem|jovens|crian[cç]as?|idos[oa]s?|menor|menores|indiv[ií]duos?|rapaz|rapazes|mo[cç]as?|senhora?s?|v[ií]timas?|suspeit[oa]s?|cliente|clientes|funcion[aá]ri[oa]s?|aluno|aluna|alunos|alunas|casal|casais|filh[oa]s?|m[ãa]e|pai|esposa|marido|candidat[oa]s?|eleitor[a]?|eleitores|colaborador[a]?|empregad[oa]s?|segurad[oa]s?|benefici[aá]ri[oa]s?)`

type sensitiveRule struct {
	re *regexp.Regexp
	// pistas: pedaços (em minúsculas, sem acento) dos quais pelo menos um tem de estar no texto
	clues []string
}

var sensitiveRules = func() []sensitiveRule {
	fa := func(vs []string) string { return alternatives(vs, unaccentedRE) }
	exactly := func(vs []string) string { return alternatives(vs, regexp.QuoteMeta) }
	// o valor é o grupo 1; antes, a expressão; depois, o valor não pode continuar (a borda é
	// conferida em findSensitive, porque \b do Go não conhece letra acentuada)
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

// sindicato: o nome do sindicato depois de quem é filiado a ele ("sindicalizado no Sindicato dos
// Metalúrgicos do ABC"). Filiação a sindicato é dado sensível; o nome sozinho, não.
var reUnion = regexp.MustCompile(`(?i:sindicalizad[oa]s?|filiad[oa]s?|associad[oa]s?|membros?|delegad[oa]s?\s+sindica(?:l|is)|dirigentes?|diretor[a]?|presidente)\s+(?:a|ao|à|no|na|do|da|de)\s+(Sindicato|SINDICATO)\s`)

var nameConnectors = setOf("de", "da", "do", "das", "dos", "e", "em", "no", "na", "nos", "nas", "a", "o")

// unionNameEnd: onde acaba "Sindicato dos Metalúrgicos do ABC": palavras com maiúscula,
// siglas e os conectores entre elas.
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

// biometria: o template de impressão digital no formato de intercâmbio de minúcias (ISO/IEC
// 19794-2 e ANSI/INCITS 378: o registro começa por "FMR\0"), que costuma ir em base64 num JSON
// ou numa coluna. Em base64, "FMR\0" começa por "Rk1SA".
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
		// os 8 primeiros caracteres são os 6 primeiros bytes do registro
		if d, err := base64.StdEncoding.DecodeString(s[ini : ini+8]); err == nil && strings.HasPrefix(string(d), "FMR\x00") {
			add(ini, end, "sensivel")
		}
	}
}

// findSensitive roda as regras deste arquivo. baixo: s em minúsculas.
func findSensitive(s, lower string, add func(ini, end int, typ string)) {
	letter := func(i int) bool { // s[i] começa uma letra ou dígito (também acentuada)
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
				add(ix[2], ix[3], "sensivel")
			}
		}
	}
	if strings.Contains(lower, "sindicato") {
		for _, ix := range reUnion.FindAllStringSubmatchIndex(s, -1) {
			if end := unionNameEnd(s, ix[3]); end > ix[3] {
				add(ix[2], end, "sensivel")
			}
		}
	}
	if strings.Contains(s, "Rk1SA") {
		findBiometricTemplate(s, add)
	}
}
