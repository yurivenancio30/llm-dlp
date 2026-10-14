package mask

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDetects(t *testing.T) {
	m := newTest(t)
	cpf := genCPF("529982247")
	pis := genEleven("1201234567", ValidPIS)
	cnh := genEleven("123456789", ValidCNH)
	cases := []struct {
		text, typ, real string
	}{
		{"owner: joao.silva@empresa-ficticia.com.br ok", "email", "joao.silva@empresa-ficticia.com.br"},
		{"conectando em 10.42.7.15:3306", "ip", "10.42.7.15"},
		{"host mysql-dh.empresa-ficticia.intra caiu", "host", "mysql-dh.empresa-ficticia.intra"},
		{"cliente " + cpf + " ativo", "cpf", cpf},
		{"cpf: " + strings.NewReplacer(".", "", "-", "").Replace(cpf), "cpf", strings.NewReplacer(".", "", "-", "").Replace(cpf)},
		{"empresa 11.222.333/0001-81", "cnpj", "11.222.333/0001-81"},
		{"ligue (11) 98765-4321 amanhã", "phone", "(11) 98765-4321"},
		{"whatsapp: +55 11 98765-4321", "phone", "+55 11 98765-4321"},
		{"CEP 01310-100", "cep", "01310-100"},
		{"cartão 4111 1111 1111 1111", "card", "4111 1111 1111 1111"},
		{"RG: 12.345.678-9", "rg", "12.345.678-9"},
		{"PIS " + pis, "pis", pis},
		{"CNH " + cnh, "cnh", cnh},
		{"agência 1234-5 conta 123456-7", "account", "1234-5"},
		{"chave pix 123e4567-e89b-42d3-a456-426614174000", "pix", "123e4567-e89b-42d3-a456-426614174000"},
		{"mora na Rua das Flores, 123 - centro", "address", "Rua das Flores, 123"},
		{"data de nascimento: 12/03/1985", "birthdate", "12/03/1985"},
		{"o owner é JOAO CARLOS SILVA", "name", "JOAO CARLOS SILVA"},
		{"falar com João Silva hoje", "name", "João Silva"},
		{"steward Ana Paula Souza", "name", "Ana Paula Souza"},
		{"usuario MAT123456 travou", "x:matricula", "MAT123456"},
		{"o Projeto Fenix atrasou", "t:projeto", "Projeto Fenix"},
		{"token=ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO", "secret", "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"},
	}
	for _, c := range cases {
		foundIt := false
		for _, a := range m.Detect(c.text) {
			if a.Type == c.typ && a.Real == c.real {
				foundIt = true
			}
		}
		if !foundIt {
			t.Errorf("did not detect %s %q in %q; findings: %+v", c.typ, c.real, c.text, m.Detect(c.text))
		}
	}
}

func TestDoesNotDetect(t *testing.T) {
	m := newTest(t)
	cleanedOnes := []string{
		"versão 2.1.280, porta 8080 em 127.0.0.1, dns 8.8.8.8",
		"oid 1.3.6.1.4.1.311 e build 10.0.19045.1",
		"run_id 123e4567-e89b-42d3-a456-426614174000 terminou",
		"o cargo de gerente e o rg da tabela", // "rg" without a number
		"contato pelo fulano@example.com",
		"commit 9f8e7d6c5b4a39281706f5e4d3c2b1a0 em 2026-10-02",
		"a Rua do Comércio é longa",
		"timestamp 1727894400123 e pedido 98765432109",
		"p.abc12345@dxyz9.invalid já mascarado",
		"IP falso 240.1.2.3 já mascarado",
		"evento (1727894400 epoch) registrado",                  // a timestamp in parentheses is not a telephone
		"exception no concept 20261003-153000-1abc do receptor", // "cep" inside another word, and a date
		"CEP de teste: 20261003",                                // data escrita junta
	}
	for _, s := range cleanedOnes {
		if as := m.Detect(s); len(as) > 0 {
			t.Errorf("false positive in %q: %+v", s, as)
		}
	}
}

func TestIdempotentAndDeterministic(t *testing.T) {
	m := newTest(t)
	s := "owner joao.silva@empresa-ficticia.com.br (João Silva) em 10.42.7.15 e 10.42.7.80, cpf " + genCPF("529982247")
	a, _ := m.Mask(s)
	b, _ := m.Mask(s)
	if a != b {
		t.Fatal("not deterministic")
	}
	if c, ents := m.Mask(a); c != a || len(ents) > 0 {
		t.Fatalf("masking again changed the text (pseudonym detected again): %q -> %q", a, c)
	}
	// name and e-mail of the same person get the same identifier
	if !strings.Contains(a, "Person ") {
		t.Fatalf("name not masked: %q", a)
	}
	pid := a[strings.Index(a, "p.")+2 : strings.Index(a, "p.")+10]
	if !strings.Contains(a, "Person "+pid) {
		t.Errorf("name and e-mail without the same id: %q", a)
	}
	// the same /24 subnet stays in the same fake subnet, with the same host
	if !strings.Contains(a, ".15") || !strings.Contains(a, ".80") {
		t.Errorf("host octet not kept: %q", a)
	}
	// another key -> other pseudonyms
	m2, _ := NewMasker(m.cfg, []byte("ffffffffffffffffffffffffffffffff"), nil, nil)
	if c, _ := m2.Mask(s); c == a {
		t.Error("a different key generated the same pseudonyms")
	}
}

func TestPasswords(t *testing.T) {
	takes := map[string]string{
		"senha: Zq8wPrimeiroValor01":                  "Zq8wPrimeiroValor01",
		"senha=Ficticia@2024":                         "Ficticia@2024",
		"a senha é Ficticia@2024.":                    "Ficticia@2024",
		"a senha do banco de dev é Ficticia@2024":     "Ficticia@2024",
		"SENHA_BANCO=Ficticia@2024":                   "Ficticia@2024",
		"DB_PASSWORD=Ficticia@2024":                   "Ficticia@2024",
		`"password": "admin123"`:                      "admin123",
		"pwd: Ficticia@2024":                          "Ficticia@2024",
		"mysql -u root -pFicticia@2024 -h db1":        "Ficticia@2024",
		"sshpass -p 'Ficticia@2024' ssh x":            "Ficticia@2024",
		"mysqldump --password=Ficticia@2024 base":     "Ficticia@2024",
		"curl -u app:Ficticia2024x https://x.test/a":  "Ficticia2024x",
		"mysql://app:Ficticia2024x@db1:3306/base":     "Ficticia2024x",
		"postgresql://app:somenteletras@db1/base":     "somenteletras",
		"the password is Tr0ub4dor&3":                 "Tr0ub4dor&3",
		"Usuário `app`, senha `Dh!Prod2026#Mysql`.":   "Dh!Prod2026#Mysql",
		"a senha do banco \"Xy7#kLm2pQ\" expira hoje": "Xy7#kLm2pQ",
		"clientSecret: aB3~xY9.kL2-mN8qR5tU1wZ4":      "aB3~xY9.kL2-mN8qR5tU1wZ4",
		"SECRET: -FraseLongaSemDigito":                "-FraseLongaSemDigito",
		"segredo: Ficticia@2024":                      "Ficticia@2024",
		// when in doubt, mask: names and references that look like values also go out
		"secretRef: db-credentials":                   "db-credentials",
		"existingPasswordSecret: mysql8-secrets":      "mysql8-secrets",
		"password_file: /run/secrets/db_pass1":        "/run/secrets/db_pass1",
		"PASSWORD: cofre-producao:DB_PASSWORD:Atual1": "cofre-producao:DB_PASSWORD:Atual1",
	}
	for text, password := range takes {
		if out, _ := newTest(t).Mask(text); strings.Contains(out, password) {
			t.Errorf("password went through: %q -> %q", text, out)
		}
	}
	cleanedOnes := []string{
		"a senha é obrigatória para entrar",
		"password = os.getenv(\"DB_PASSWORD\")",
		"password: str = None",
		"conn = connect(user=user, password=password)",
		"senha = self.senha",
		"password: ${DB_PASSWORD}",
		"password: <sua senha aqui>",
		"password: ********",
		"password=args.password",
		"campo senha: varchar",
		"if password is None:",
		"git push -u origin main && ls -p",
		"https://example.com:8080/caminho",
		"ssh -p 2222 servidor",
		"password=os.environ[\"X1\"]",
		"at PasswordConfigEnhancer.java:123",
		"grep -E \"clientSecret:|password:|TOKEN_A\" x",
		"edite o secrets.yaml: ele fica na raiz",
		"# password: 123-",
	}
	for _, text := range cleanedOnes {
		if out, ents := newTest(t).Mask(text); len(ents) > 0 {
			t.Errorf("false positive: %q -> %q", text, out)
		}
	}
}

func TestExtraDetectors(t *testing.T) {
	ttl := genValid("1234567801", 12, ValidVoterID)
	cns := genValid("7000000000000", 15, ValidCNS)
	ren := genValid("6392748451", 11, ValidRenavam)
	takes := map[string]string{
		"Authorization: Basic am9hbzpGaWN0aWNpYUAyMDI0":                             "am9hbzpGaWN0aWNpYUAyMDI0",
		"> authorization: Bearer 8f3a2b1c9d4e5f60718293a4b5c6d7e8":                  "8f3a2b1c9d4e5f60718293a4b5c6d7e8",
		"Cookie: JSESSIONID=A1B2C3D4E5F60718293A4B5C6D7E8F90; theme=dark":           "A1B2C3D4E5F60718293A4B5C6D7E8F90",
		"< Set-Cookie: sessionid=k2j3h4g5f6d7s8a9q0w1e2r3; Path=/; HttpOnly":        "k2j3h4g5f6d7s8a9q0w1e2r3",
		"processo 0001234-56.2023.8.13.0024 em andamento":                           "0001234-56.2023.8.13.0024",
		"fornecedor 12.ABC.345/01DE-35 ativo":                                       "12.ABC.345/01DE-35",
		"transferir para GB82 WEST 1234 5698 7654 32 hoje":                          "GB82 WEST 1234 5698 7654 32",
		"conta BR1500000000000010932840814P2.":                                      "BR1500000000000010932840814P2",
		"título de eleitor " + ttl:                                                  ttl,
		"cartão do SUS: " + cns:                                                     cns,
		"RENAVAM " + ren:                                                            ren,
		"passaporte FZ123456 vencido":                                               "FZ123456",
		"veículo placa BRA2E19 e chassi 9BWZZZ377VT004251":                          "BRA2E19",
		"chassi: 9BWZZZ377VT004251":                                                 "9BWZZZ377VT004251",
		"IMEI 490154203237518":                                                      "490154203237518",
		"first_name,last_name,ssn\nRenata,Vasconcelos,078-05-1120\n":                "078-05-1120",
		"NO_PACIENTE;NU_CNS;CO_MUNICIPIO\nMarcos Vinicius Paz;" + cns + ";310620\n": "Marcos Vinicius Paz",
		"CD_FUNC;NM_FUNC;DS_CARGO\nF0012345;Sandra Maia Lins;analista\n":            "F0012345",
		"account_number,bank_code\n00123456-7,341\n":                                "00123456-7",
		"credit_card_number: 4111111111111111":                                      "4111111111111111",
		"employeeId=E0098765 dept=TI":                                               "E0098765",
	}
	for text, real := range takes {
		if out, _ := newTest(t).Mask(text); strings.Contains(out, real) {
			t.Errorf("%q went through in %q -> %q", real, text, out)
		}
	}
	stays := []string{
		"Cookie: theme=dark; lang=pt-BR",
		"Set-Cookie: a=b; Path=/; Max-Age=3600; SameSite=Lax",
		"versão 2023.8.13 do pacote, build 0001234",
		"hash GB82WESTxyz e código BR15 em análise",
		"o título do relatório tem 123456789012 caracteres",
		"placa de vídeo RTX4090 instalada",
		"sexo;idade;uf\nF;34;MG\n", // quasi-identifiers: off by default
		"card_type: visa",
		"status_code: 200\ncontent-type: application/json",
		"curl -H \"Authorization: Bearer ${TOKEN}\" https://x.test",
		"Authorization: Bearer <seu-token-aqui>",
		"client_id: 3f2a1b4c-5d6e-7f80-9a1b-2c3d4e5f6071\ntenant_id: AZURE_TENANT_ID",
		"WHERE user_id = :user_id",
	}
	for _, s := range stays {
		if out, ents := newTest(t).Mask(s); len(ents) > 0 {
			t.Errorf("masked needlessly: %q -> %q", s, out)
		}
	}
}

// Imported name in any spelling.
func TestNameAnySpelling(t *testing.T) {
	m := newTest(t)
	for _, s := range []string{"falei com joão silva ontem", "o JOAO CARLOS SILVA saiu", "Joao Carlos Silva", "ana paula souza aprovou"} {
		// the pseudonym follows the case of the name: Person, PERSON or person
		if out, _ := m.Mask(s); !strings.Contains(strings.ToLower(out), "person ") {
			t.Errorf("name not masked in %q: %q", s, out)
		}
	}
	// common words that are not an imported name stay intact
	if out, _ := m.Mask("a silva e souza são sobrenomes comuns"); strings.Contains(strings.ToLower(out), "person ") {
		t.Errorf("false positive: %q", out)
	}
}

// A secret inside base64 (Kubernetes Secret), without a keyword nearby.
func TestSecretInBase64(t *testing.T) {
	m := newTest(t)
	enc := base64.StdEncoding.EncodeToString([]byte("ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"))
	for _, s := range []string{
		"apiVersion: v1\nkind: Secret\ndata:\n  conexao: " + enc + "\n  outro: abc\n",
		enc + "\nlinha seguinte", // on the first line (the gitleaks column has another offset)
	} {
		out, _ := m.Mask(s)
		if strings.Contains(out, enc) || strings.Contains(out, enc[:20]) {
			t.Errorf("base64 with a secret not masked:\n%s", out)
		}
	}
}

// Large text goes to gitleaks in windows. A secret in any position, including on top of the cut,
// has to be found as in the whole text.
func TestSecretAtWindowBoundary(t *testing.T) {
	tok := "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"
	line := "linha de log comum sem nada sensivel, status=200\n"
	for _, pos := range []int{0, leaksWindow - 20, leaksWindow - 5, leaksWindow, leaksWindow + 3, 2*leaksWindow - leaksLeftover - 10, 3 * leaksWindow} {
		s := strings.Repeat(line, pos/len(line)+1)[:pos] + "\ntoken=" + tok + "\n" + strings.Repeat(line, 2*leaksWindow/len(line))
		if out, _ := newTest(t).Mask(s); strings.Contains(out, tok) {
			t.Errorf("token at position %d (window of %d) went through", pos, leaksWindow)
		}
	}
	// without any line break (a single block)
	block := strings.Repeat("a ", leaksWindow/2-10) + "token=" + tok + " " + strings.Repeat("b ", leaksWindow)
	if out, _ := newTest(t).Mask(block); strings.Contains(out, tok) {
		t.Error("token on top of the cut, without a line break, went through")
	}
}

// Items 1 and 2 (external review): valid CPF and CNPJ, digits only, without the word "cpf" nearby
// and without a recognized column name. Reproduces a query with UNION in which the column got the
// alias "valor": the values went out in the clear.
func TestDigitsOnlyDocumentWithoutContext(t *testing.T) {
	cpf1 := strings.NewReplacer(".", "", "-", "").Replace(genCPF("318452760"))
	cpf2 := strings.NewReplacer(".", "", "-", "").Replace(genCPF("529982247"))
	cnpj := strings.NewReplacer(".", "", "-", "", "/", "").Replace("11.222.333/0001-81")
	cases := map[string][]string{
		"valor\n" + cpf1 + "\n" + cpf2 + "\n":               {cpf1, cpf2},
		`{"id": 7, "userDocument": "` + cpf1 + `"}`:         {cpf1},
		"o resultado da consulta foi " + cpf2 + " e pronto": {cpf2},
		"fornecedor " + cnpj + " ativo":                     {cnpj},
	}
	for text, reals := range cases {
		out, _ := newTest(t).Mask(text)
		for _, r := range reals {
			if strings.Contains(out, r) {
				t.Errorf("%q went through in %q -> %q", r, text, out)
			}
		}
	}
}

// The exceptions of CPF/CNPJ without context: a piece of a hash, a decimal part, a number with a
// version and a date-time stamp (14 digits) are not documents.
func TestDigitsOnlyDocumentExceptions(t *testing.T) {
	cpf := strings.NewReplacer(".", "", "-", "").Replace(genCPF("318452760"))
	stays := []string{
		"hash a" + cpf + "f e id_" + cpf,
		"pi vale 3." + cpf,
		"versão v1." + cpf + ".2",
		"carimbo 20261004104220 gravado",
	}
	for _, s := range stays {
		if out, ents := newTest(t).Mask(s); len(ents) > 0 {
			t.Errorf("masked needlessly: %q -> %q", s, out)
		}
	}
	cfg := newTest(t).cfg
	cfg.DocumentsWithoutContext = false
	m, _ := NewMasker(cfg, testKey, nil, nil)
	if out, _ := m.Mask("valor " + cpf); !strings.Contains(out, cpf) {
		t.Errorf("with documents_without_context off, it should stay as it is: %q", out)
	}
}
