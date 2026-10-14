package mask

import (
	"strings"
	"unicode"
)

// Detecção pelo nome do campo, parte 1: o vocabulário (que nomes de campo guardam que dado).

// Detecção pelo RÓTULO do campo. Muito dado pessoal não tem formato próprio (um nome, um
// código de usuário, um CPF sem pontuação numa coluna, a religião de alguém), mas vem com
// o nome do campo ao lado: o cabeçalho de um CSV ou do resultado de uma consulta, a chave de
// um JSON, um "campo: valor". Aqui o nome do campo decide o que fazer com o valor.
//
// As categorias seguem a política de classificação do cliente (PII: CPF, CNPJ, nome, e-mail,
// telefone, endereço...) e os dados pessoais sensíveis da LGPD (art. 5º, II).

// vocabulario: as palavras que o detector conhece nos nomes de campo. O padrão junta o que
// as ferramentas de classificação de dados usam (piicatcher, OpenMetadata, datahub-classify,
// em inglês) com os termos e prefixos usados no Brasil (NM_, NO_, NU_, CO_, CD_...). Como
// cada empresa abrevia do seu jeito, dá para acrescentar palavras no config.json
// (extra_fields) e ver o que foi reconhecido com "llm-dlp columns arquivo".
type vocabulary struct {
	class   map[string]string // palavra -> tipo de dado do campo
	person  map[string]bool   // de quem é o campo: "cliente", "titular", "owner"...
	abrev   map[string]bool   // pessoa abreviada ("cli", "func"): só vale junto de nome/código
	neutral map[string]bool   // palavras que não mudam o que o campo guarda
	almost  bool              // classes de "quase identificadores" ligadas (opcional)
}

func setOf(ps ...string) map[string]bool {
	m := make(map[string]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

func defaultVocabulary() *vocabulary {
	v := &vocabulary{class: map[string]string{}}
	by := func(typ string, ps ...string) {
		for _, p := range ps {
			v.class[p] = typ
		}
	}
	// e-mail é mascarado pelo formato, em qualquer campo; a classe existe só para o comando
	// "colunas" dizer isso em vez de "não reconhecido"
	by("email", "email", "mail")
	by("cpf", "cpf")
	by("cnpj", "cnpj")
	by("rg", "rg", "identidade")
	by("cnh", "cnh", "habilitacao")
	by("pis", "pis", "pasep", "nis", "nit")
	by("doc", "document", "documento", "documentos", "taxpayer", "taxid", "tin", "cpfcnpj", "passaporte", "passport", "renavam", "ctps", "chassi", "vin", "placa", "cns", "ssn", "nif", "dni", "curp", "rfc", "cuit", "rut",
		"taxid", "imei", "certidao", "rne", "crm", "oab", "crea")
	by("nascimento", "nascimento", "nasc", "birth", "birthdate", "birthday", "dob", "dateofbirth", "obito", "falecimento")
	by("telefone", "telefone", "fone", "celular", "phone", "telephone", "mobile", "whatsapp", "tel", "fax", "cell", "cellphone", "ramal")
	by("cep", "cep", "zipcode", "zip", "postal", "postalcode", "postcode")
	by("endereco", "endereco", "logradouro", "street", "rua")
	by("usuario", "matricula", "login", "username", "userid", "logon", "chapa", "cracha", "badge", "employeeid", "empid", "customerid")
	by("conta", "iban", "bban")
	by("sensivel", "religiao", "religion", "crenca", "raca", "etnia", "ethnicity", "sindicato", "sindical", "doenca", "deficiencia", "sanguineo",
		"biometria", "biometrico", "diagnosis", "disability", "prontuario", "race", "racial", "genetico", "genetica",
		"genetic", "hiv", "sorologia", "biometric", "biometrics", "comorbidade", "comorbidades")
	by("nome", "sobrenome", "surname", "fullname", "firstname", "lastname", "fname", "lname", "maidenname", "nickname", "apelido", "filiacao")
	by("quase", "sexo", "genero", "gender", "idade", "age", "nacionalidade", "naturalidade", "nationality", "profissao", "ocupacao", "occupation",
		"salario", "renda", "salary", "income", "latitude", "longitude", "lat", "lng", "lon", "escolaridade")
	v.person = setOf("cliente", "customer", "pessoa", "person", "titular", "owner", "steward", "custodian", "custodiante", "funcionario",
		"colaborador", "employee", "usuario", "usu", "user", "responsavel", "gestor", "manager", "gerente", "contato", "contact", "socio",
		"beneficiario", "dependente", "representante", "procurador", "favorecido", "pagador", "sacado", "cedente", "avalista", "correntista",
		"portador", "solicitante", "aprovador", "analista", "autor", "author", "criador", "creator", "requisitante", "destinatario", "remetente",
		"paciente", "patient", "aluno", "student", "candidato", "mae", "pai", "mother", "father", "conjuge", "spouse", "completo", "full", "first",
		"last", "segurado", "contribuinte", "devedor", "credor", "emitente", "fiador", "tomador", "proponente", "cotitular", "vendedor",
		"comprador", "operador", "atendente", "medico", "professor", "motorista", "condutor", "passageiro", "hospede", "morador", "inquilino",
		"proprietario", "assinante", "membro", "member", "participante", "contratante", "contratado", "empregado", "estagiario", "terceiro",
		"consultor", "supervisor", "coordenador", "diretor", "lider", "subscriber", "holder", "buyer", "seller", "guest")
	v.abrev = setOf("cli", "func", "colab", "resp", "usr", "emp", "cust", "ger", "tit", "benef", "dep")
	v.neutral = setOf("num", "nr", "nu", "numero", "number", "no", "cod", "codigo", "code", "cd", "co", "id", "dt", "data", "date", "ds", "tx", "txt",
		"de", "do", "da", "dos", "das", "e", "the", "of", "principal", "primario", "secundario", "novo", "antigo", "atual", "res", "com",
		"cel", "residencial", "comercial", "pessoal", "1", "2", "3", "sg", "st", "tp", "vl", "qt", "in", "fl", "ed", "pk", "fk", "sk", "nk",
		"src", "tgt", "raw", "old", "new", "hash", "masked", "valor", "value", "str", "desc", "info", "dados")
	return v
}

var numericDoc = setOf("cpf", "cnpj", "rg", "cnh", "pis", "doc")

// labelClass devolve o tipo de dado que um campo com esse nome guarda ("" = nada sensível).
//
// É de propósito rígido: TODAS as palavras do rótulo têm que ser conhecidas (a palavra da
// classe, a pessoa, ou uma palavra neutra). "NUM_CPF_CLIENTE" vale; "login_timeout",
// "usuario_erro_pct" e uma frase com "nome" no meio não valem. As ferramentas de catálogo
// usam regex larga (".*name.*") porque só marcam a coluna para revisão; aqui o valor é
// trocado, e uma regra larga mascararia "NOM_SISTEMA" e "file_name".
func (v *vocabulary) labelClass(label string) string {
	ts := labelTokens(label)
	if len(ts) == 0 || len(ts) > 5 {
		return ""
	}
	if compoundSensitive(ts) {
		return "sensivel"
	}
	class, name, person, abbrevOnly, user, codeID := "", false, false, true, false, false
	for i, t := range ts {
		switch {
		case v.class[t] != "":
			c := v.class[t]
			if c == "quase" && !v.almost {
				return ""
			}
			if class != "" && class != c {
				if !numericDoc[class] || !numericDoc[c] {
					return "" // duas classes no mesmo rótulo: não é um campo simples
				}
				continue // "CPF/CNPJ", "RG_CPF": fica a primeira
			}
			class = c
		case t == "nome" || t == "nom" || t == "name" || t == "nm" || (t == "no" && i == 0 && len(ts) > 1):
			name = true // "NO_" no começo é o prefixo de nome de alguns padrões (DATASUS)
		case v.person[t]:
			person, abbrevOnly = true, false
			user = user || t == "usuario" || t == "usu" || t == "user"
		case v.abrev[t]:
			person = true
			user = user || t == "usr"
		case v.neutral[t]:
			codeID = codeID || t == "cod" || t == "codigo" || t == "code" || t == "id" || t == "cd" || t == "co"
		case t == "eleitor" || t == "titulo" || t == "sus" || t == "cartao" || t == "card" || t == "credito" || t == "credit" || t == "debito" ||
			t == "sexual" || t == "orientacao" || t == "politico" || t == "partido" || t == "cor" || t == "tipo" || t == "type" ||
			t == "conta" || t == "account" || t == "bank" || t == "bancaria" || t == "corrente" || t == "agencia" || t == "poupanca" ||
			t == "estado" || t == "civil" || t == "marital" || t == "status" || t == "tax" || t == "social" || t == "security" ||
			t == "license" || t == "licence" || t == "driver" || t == "drivers":
			// só valem nas combinações abaixo
		default:
			return ""
		}
	}
	has := func(ps ...string) bool {
		for _, t := range ts {
			for _, p := range ps {
				if t == p {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has("titulo") && has("eleitor"), has("cartao") && has("sus"), has("tax") && codeID, has("social") && has("security"),
		has("driver", "drivers") && has("license", "licence"):
		return "doc"
	case has("cartao", "card") && (has("credito", "credit", "debito") || has("num", "numero", "number", "nr", "nu")) && !has("sus"):
		return "cartao"
	case has("conta", "account") && (has("corrente", "bancaria", "bank", "poupanca") || has("num", "numero", "number", "nr", "nu")),
		has("agencia") && (has("conta") || has("num", "numero", "nr", "nu", "cod", "cd")):
		return "conta"
	case has("orientacao") && has("sexual"), has("partido") && has("politico"):
		return "sensivel"
	case has("estado", "marital") && has("civil", "status"):
		if v.almost {
			return "quase"
		}
		return ""
	case has("titulo", "eleitor", "sus", "cartao", "card", "credito", "credit", "debito", "sexual", "orientacao", "politico", "partido",
		"conta", "account", "bank", "bancaria", "corrente", "agencia", "poupanca", "estado", "civil", "marital", "status", "tax", "social",
		"security", "license", "licence", "driver", "drivers"):
		return ""
	case has("tipo", "type") && class != "sensivel", has("cor") && class != "sensivel":
		return "" // "tipo_usuario", "cor" sozinho; valem "tipo_sanguineo", "raca_cor"
	case class != "":
		return class
	case name && user && len(ts) <= 3:
		return "usuario" // "user_name", "nome_usuario": é o login
	case name && person:
		return "nome"
	case codeID && person:
		return "usuario" // "cod_usu_owner", "id_cliente", "user_id", "cd_func"
	case len(ts) == 1 && (ts[0] == "usuario" || ts[0] == "usu"):
		return "usuario"
	case person && !abbrevOnly && !codeID && !name && !user && !has("completo", "full", "first", "last") &&
		!has("pai"): // "pai" sozinho costuma ser "processo pai", "nó pai"
		// "Cliente", "Paciente", "Owner", "Data Steward", "Responsável": costuma ser o nome da
		// pessoa, mas também pode ser um papel ou sistema ("SYSADMIN", "TI"); só vale para
		// valor com nome e sobrenome
		return "nomecompleto"
	case name && len(ts) == 1:
		// "nome" ou "name" sozinho: só vale numa tabela que tenha outra coluna de pessoa
		return "nomesolto"
	}
	return ""
}

// compoundSensitive: rótulos de dado sensível (LGPD, art. 5º, II) feitos de palavras que sozinhas
// são de outra classe ou de nenhuma: "identidade de gênero" (identidade é RG), "filiação
// partidária" e "filiação sindical" (filiação é o nome dos pais), "opinião política",
// "convicção religiosa", "vida sexual", "impressão digital", "cor/raça".
func compoundSensitive(ts []string) bool {
	has := map[string]bool{}
	for _, t := range ts {
		has[t] = true
	}
	switch {
	case has["identidade"] && has["genero"], has["filiacao"] && (has["partidaria"] || has["sindical"] || has["partido"]),
		has["opiniao"] && (has["politica"] || has["politico"]), has["conviccao"] && (has["religiosa"] || has["politica"] || has["filosofica"]),
		has["vida"] && has["sexual"], has["impressao"] && (has["digital"] || has["digitais"]), has["raca"] && has["cor"],
		has["partido"] && has["politico"], has["orientacao"] && has["sexual"]:
		return true
	}
	return false
}

// labelClass com o vocabulário padrão (usado nos testes).
func labelClass(label string) string { return defaultVocab.labelClass(label) }

var defaultVocab = defaultVocabulary()

// labelTokens: "NOM_DATA_OWNER", "nomeCliente", "Name da Mãe" -> [nom data owner], [nome cliente], [nome da mae]
func labelTokens(r string) []string {
	var ts []string
	var b strings.Builder
	closes := func() {
		if b.Len() > 0 {
			ts = append(ts, b.String())
			b.Reset()
		}
	}
	prev := rune(0)
	for _, c := range NormName(strings.TrimSpace(noGluedCapitals(r))) { // NormName tira acentos e põe em maiúsculas
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(unicode.ToLower(c))
		} else {
			closes()
		}
		prev = c
	}
	_ = prev
	closes()
	return ts
}

// noGluedCapitals separa camelCase: "nomeCliente" -> "nome Cliente"
func noGluedCapitals(r string) string {
	var b strings.Builder
	prev := rune(0)
	for _, c := range r {
		if unicode.IsUpper(c) && unicode.IsLower(prev) {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
		prev = c
	}
	return b.String()
}

// classe é labelClass com memória (os mesmos rótulos se repetem em toda linha).
func (m *Masker) class(label string) string {
	if c, ok := m.classes.Load(label); ok {
		return c.(string)
	}
	c := m.vocab.labelClass(label)
	if m.nClasses.Add(1) > 20000 { // teto simples
		m.classes.Clear()
		m.nClasses.Store(0)
	}
	m.classes.Store(label, c)
	return c
}
