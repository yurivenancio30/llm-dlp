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
// (campos_extras) e ver o que foi reconhecido com "llm-dlp colunas arquivo".
type vocabulario struct {
	classe map[string]string // palavra -> tipo de dado do campo
	pessoa map[string]bool   // de quem é o campo: "cliente", "titular", "owner"...
	abrev  map[string]bool   // pessoa abreviada ("cli", "func"): só vale junto de nome/código
	neutra map[string]bool   // palavras que não mudam o que o campo guarda
	quase  bool              // classes de "quase identificadores" ligadas (opcional)
}

func conj(ps ...string) map[string]bool {
	m := make(map[string]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

func vocabularioPadrao() *vocabulario {
	v := &vocabulario{classe: map[string]string{}}
	por := func(tipo string, ps ...string) {
		for _, p := range ps {
			v.classe[p] = tipo
		}
	}
	// e-mail é mascarado pelo formato, em qualquer campo; a classe existe só para o comando
	// "colunas" dizer isso em vez de "não reconhecido"
	por("email", "email", "mail")
	por("cpf", "cpf")
	por("cnpj", "cnpj")
	por("rg", "rg", "identidade")
	por("cnh", "cnh", "habilitacao")
	por("pis", "pis", "pasep", "nis", "nit")
	por("doc", "passaporte", "passport", "renavam", "ctps", "chassi", "vin", "placa", "cns", "ssn", "nif", "dni", "curp", "rfc", "cuit", "rut",
		"taxid", "imei", "certidao", "rne", "crm", "oab", "crea")
	por("nascimento", "nascimento", "nasc", "birth", "birthdate", "birthday", "dob", "dateofbirth", "obito", "falecimento")
	por("telefone", "telefone", "fone", "celular", "phone", "telephone", "mobile", "whatsapp", "tel", "fax", "cell", "cellphone", "ramal")
	por("cep", "cep", "zipcode", "zip", "postal", "postalcode", "postcode")
	por("endereco", "endereco", "logradouro", "street", "rua")
	por("usuario", "matricula", "login", "username", "userid", "logon", "chapa", "cracha", "badge", "employeeid", "empid", "customerid")
	por("conta", "iban", "bban")
	por("sensivel", "religiao", "religion", "crenca", "raca", "etnia", "ethnicity", "sindicato", "sindical", "doenca", "deficiencia", "sanguineo",
		"biometria", "biometrico", "diagnosis", "disability", "prontuario")
	por("nome", "sobrenome", "surname", "fullname", "firstname", "lastname", "fname", "lname", "maidenname", "nickname", "apelido", "filiacao")
	por("quase", "sexo", "genero", "gender", "idade", "age", "nacionalidade", "naturalidade", "nationality", "profissao", "ocupacao", "occupation",
		"salario", "renda", "salary", "income", "latitude", "longitude", "lat", "lng", "lon", "escolaridade")
	v.pessoa = conj("cliente", "customer", "pessoa", "person", "titular", "owner", "steward", "custodian", "custodiante", "funcionario",
		"colaborador", "employee", "usuario", "usu", "user", "responsavel", "gestor", "manager", "gerente", "contato", "contact", "socio",
		"beneficiario", "dependente", "representante", "procurador", "favorecido", "pagador", "sacado", "cedente", "avalista", "correntista",
		"portador", "solicitante", "aprovador", "analista", "autor", "author", "criador", "creator", "requisitante", "destinatario", "remetente",
		"paciente", "patient", "aluno", "student", "candidato", "mae", "pai", "mother", "father", "conjuge", "spouse", "completo", "full", "first",
		"last", "segurado", "contribuinte", "devedor", "credor", "emitente", "fiador", "tomador", "proponente", "cotitular", "vendedor",
		"comprador", "operador", "atendente", "medico", "professor", "motorista", "condutor", "passageiro", "hospede", "morador", "inquilino",
		"proprietario", "assinante", "membro", "member", "participante", "contratante", "contratado", "empregado", "estagiario", "terceiro",
		"consultor", "supervisor", "coordenador", "diretor", "lider", "subscriber", "holder", "buyer", "seller", "guest")
	v.abrev = conj("cli", "func", "colab", "resp", "usr", "emp", "cust", "ger", "tit", "benef", "dep")
	v.neutra = conj("num", "nr", "nu", "numero", "number", "no", "cod", "codigo", "code", "cd", "co", "id", "dt", "data", "date", "ds", "tx", "txt",
		"de", "do", "da", "dos", "das", "e", "the", "of", "principal", "primario", "secundario", "novo", "antigo", "atual", "res", "com",
		"cel", "residencial", "comercial", "pessoal", "1", "2", "3", "sg", "st", "tp", "vl", "qt", "in", "fl", "ed", "pk", "fk", "sk", "nk",
		"src", "tgt", "raw", "old", "new", "hash", "masked", "valor", "value", "str", "desc", "info", "dados")
	return v
}

var docNumerico = conj("cpf", "cnpj", "rg", "cnh", "pis", "doc")

// classeRotulo devolve o tipo de dado que um campo com esse nome guarda ("" = nada sensível).
//
// É de propósito rígido: TODAS as palavras do rótulo têm que ser conhecidas (a palavra da
// classe, a pessoa, ou uma palavra neutra). "NUM_CPF_CLIENTE" vale; "login_timeout",
// "usuario_erro_pct" e uma frase com "nome" no meio não valem. As ferramentas de catálogo
// usam regex larga (".*name.*") porque só marcam a coluna para revisão; aqui o valor é
// trocado, e uma regra larga mascararia "NOM_SISTEMA" e "file_name".
func (v *vocabulario) classeRotulo(rotulo string) string {
	ts := tokensRotulo(rotulo)
	if len(ts) == 0 || len(ts) > 5 {
		return ""
	}
	classe, nome, pessoa, soAbrev, usuario, idCod := "", false, false, true, false, false
	for i, t := range ts {
		switch {
		case v.classe[t] != "":
			c := v.classe[t]
			if c == "quase" && !v.quase {
				return ""
			}
			if classe != "" && classe != c {
				if !docNumerico[classe] || !docNumerico[c] {
					return "" // duas classes no mesmo rótulo: não é um campo simples
				}
				continue // "CPF/CNPJ", "RG_CPF": fica a primeira
			}
			classe = c
		case t == "nome" || t == "nom" || t == "name" || t == "nm" || (t == "no" && i == 0 && len(ts) > 1):
			nome = true // "NO_" no começo é o prefixo de nome de alguns padrões (DATASUS)
		case v.pessoa[t]:
			pessoa, soAbrev = true, false
			usuario = usuario || t == "usuario" || t == "usu" || t == "user"
		case v.abrev[t]:
			pessoa = true
			usuario = usuario || t == "usr"
		case v.neutra[t]:
			idCod = idCod || t == "cod" || t == "codigo" || t == "code" || t == "id" || t == "cd" || t == "co"
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
	tem := func(ps ...string) bool {
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
	case tem("titulo") && tem("eleitor"), tem("cartao") && tem("sus"), tem("tax") && idCod, tem("social") && tem("security"),
		tem("driver", "drivers") && tem("license", "licence"):
		return "doc"
	case tem("cartao", "card") && (tem("credito", "credit", "debito") || tem("num", "numero", "number", "nr", "nu")) && !tem("sus"):
		return "cartao"
	case tem("conta", "account") && (tem("corrente", "bancaria", "bank", "poupanca") || tem("num", "numero", "number", "nr", "nu")),
		tem("agencia") && (tem("conta") || tem("num", "numero", "nr", "nu", "cod", "cd")):
		return "conta"
	case tem("orientacao") && tem("sexual"), tem("partido") && tem("politico"):
		return "sensivel"
	case tem("estado", "marital") && tem("civil", "status"):
		if v.quase {
			return "quase"
		}
		return ""
	case tem("titulo", "eleitor", "sus", "cartao", "card", "credito", "credit", "debito", "sexual", "orientacao", "politico", "partido",
		"conta", "account", "bank", "bancaria", "corrente", "agencia", "poupanca", "estado", "civil", "marital", "status", "tax", "social",
		"security", "license", "licence", "driver", "drivers"):
		return ""
	case tem("tipo", "type") && classe != "sensivel", tem("cor") && classe != "sensivel":
		return "" // "tipo_usuario", "cor" sozinho; valem "tipo_sanguineo", "raca_cor"
	case classe != "":
		return classe
	case nome && usuario && len(ts) <= 3:
		return "usuario" // "user_name", "nome_usuario": é o login
	case nome && pessoa:
		return "nome"
	case idCod && pessoa:
		return "usuario" // "cod_usu_owner", "id_cliente", "user_id", "cd_func"
	case len(ts) == 1 && (ts[0] == "usuario" || ts[0] == "usu"):
		return "usuario"
	case pessoa && !soAbrev && !idCod && !nome && !usuario && !tem("completo", "full", "first", "last") &&
		!tem("pai"): // "pai" sozinho costuma ser "processo pai", "nó pai"
		// "Cliente", "Paciente", "Owner", "Data Steward", "Responsável": costuma ser o nome da
		// pessoa, mas também pode ser um papel ou sistema ("SYSADMIN", "TI"); só vale para
		// valor com nome e sobrenome
		return "nomecompleto"
	case nome && len(ts) == 1:
		// "nome" ou "name" sozinho: só vale numa tabela que tenha outra coluna de pessoa
		return "nomesolto"
	}
	return ""
}

// classeRotulo com o vocabulário padrão (usado nos testes).
func classeRotulo(rotulo string) string { return vocabPadrao.classeRotulo(rotulo) }

var vocabPadrao = vocabularioPadrao()

// tokensRotulo: "NOM_DATA_OWNER", "nomeCliente", "Nome da Mãe" -> [nom data owner], [nome cliente], [nome da mae]
func tokensRotulo(r string) []string {
	var ts []string
	var b strings.Builder
	fecha := func() {
		if b.Len() > 0 {
			ts = append(ts, b.String())
			b.Reset()
		}
	}
	ant := rune(0)
	for _, c := range NormNome(strings.TrimSpace(semMaiusculasGrudadas(r))) { // NormNome tira acentos e põe em maiúsculas
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(unicode.ToLower(c))
		} else {
			fecha()
		}
		ant = c
	}
	_ = ant
	fecha()
	return ts
}

// semMaiusculasGrudadas separa camelCase: "nomeCliente" -> "nome Cliente"
func semMaiusculasGrudadas(r string) string {
	var b strings.Builder
	ant := rune(0)
	for _, c := range r {
		if unicode.IsUpper(c) && unicode.IsLower(ant) {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
		ant = c
	}
	return b.String()
}

// classe é classeRotulo com memória (os mesmos rótulos se repetem em toda linha).
func (m *Masker) classe(rotulo string) string {
	if c, ok := m.classes.Load(rotulo); ok {
		return c.(string)
	}
	c := m.vocab.classeRotulo(rotulo)
	if m.nClasses.Add(1) > 20000 { // teto simples
		m.classes.Clear()
		m.nClasses.Store(0)
	}
	m.classes.Store(rotulo, c)
	return c
}
