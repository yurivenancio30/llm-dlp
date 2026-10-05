package mask

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDetecta(t *testing.T) {
	m := novoTeste(t)
	cpf := gerarCPF("529982247")
	pis := gerarOnze("1201234567", PISValido)
	cnh := gerarOnze("123456789", CNHValida)
	casos := []struct {
		texto, tipo, real string
	}{
		{"owner: joao.silva@empresa-ficticia.com.br ok", "email", "joao.silva@empresa-ficticia.com.br"},
		{"conectando em 10.42.7.15:3306", "ip", "10.42.7.15"},
		{"host mysql-dh.empresa-ficticia.intra caiu", "host", "mysql-dh.empresa-ficticia.intra"},
		{"cliente " + cpf + " ativo", "cpf", cpf},
		{"cpf: " + strings.NewReplacer(".", "", "-", "").Replace(cpf), "cpf", strings.NewReplacer(".", "", "-", "").Replace(cpf)},
		{"empresa 11.222.333/0001-81", "cnpj", "11.222.333/0001-81"},
		{"ligue (11) 98765-4321 amanhã", "telefone", "(11) 98765-4321"},
		{"whatsapp: +55 11 98765-4321", "telefone", "+55 11 98765-4321"},
		{"CEP 01310-100", "cep", "01310-100"},
		{"cartão 4111 1111 1111 1111", "cartao", "4111 1111 1111 1111"},
		{"RG: 12.345.678-9", "rg", "12.345.678-9"},
		{"PIS " + pis, "pis", pis},
		{"CNH " + cnh, "cnh", cnh},
		{"agência 1234-5 conta 123456-7", "conta", "1234-5"},
		{"chave pix 123e4567-e89b-42d3-a456-426614174000", "pix", "123e4567-e89b-42d3-a456-426614174000"},
		{"mora na Rua das Flores, 123 - centro", "endereco", "Rua das Flores, 123"},
		{"data de nascimento: 12/03/1985", "nascimento", "12/03/1985"},
		{"o owner é JOAO CARLOS SILVA", "nome", "JOAO CARLOS SILVA"},
		{"falar com João Silva hoje", "nome", "João Silva"},
		{"steward Ana Paula Souza", "nome", "Ana Paula Souza"},
		{"usuario MAT123456 travou", "x:matricula", "MAT123456"},
		{"o Projeto Fenix atrasou", "t:projeto", "Projeto Fenix"},
		{"token=ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO", "segredo", "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"},
	}
	for _, c := range casos {
		achou := false
		for _, a := range m.Detectar(c.texto) {
			if a.Tipo == c.tipo && a.Real == c.real {
				achou = true
			}
		}
		if !achou {
			t.Errorf("não detectou %s %q em %q; achados: %+v", c.tipo, c.real, c.texto, m.Detectar(c.texto))
		}
	}
}

func TestNaoDetecta(t *testing.T) {
	m := novoTeste(t)
	limpos := []string{
		"versão 2.1.280, porta 8080 em 127.0.0.1, dns 8.8.8.8",
		"oid 1.3.6.1.4.1.311 e build 10.0.19045.1",
		"run_id 123e4567-e89b-42d3-a456-426614174000 terminou",
		"o cargo de gerente e o rg da tabela", // "rg" sem número
		"contato pelo fulano@example.com",
		"commit 9f8e7d6c5b4a39281706f5e4d3c2b1a0 em 2026-10-02",
		"a Rua do Comércio é longa",
		"timestamp 1727894400123 e pedido 98765432109",
		"p.abc12345@dxyz9.invalid já mascarado",
		"IP falso 240.1.2.3 já mascarado",
		"evento (1727894400 epoch) registrado",                  // timestamp entre parênteses não é telefone
		"exception no concept 20261003-153000-1abc do receptor", // "cep" dentro de outra palavra, e uma data
		"CEP de teste: 20261003",                                // data escrita junta
	}
	for _, s := range limpos {
		if as := m.Detectar(s); len(as) > 0 {
			t.Errorf("falso positivo em %q: %+v", s, as)
		}
	}
}

func TestIdempotenteEDeterministico(t *testing.T) {
	m := novoTeste(t)
	s := "owner joao.silva@empresa-ficticia.com.br (João Silva) em 10.42.7.15 e 10.42.7.80, cpf " + gerarCPF("529982247")
	a, _ := m.Mascarar(s)
	b, _ := m.Mascarar(s)
	if a != b {
		t.Fatal("não determinístico")
	}
	if c, ents := m.Mascarar(a); c != a || len(ents) > 0 {
		t.Fatalf("mascarar de novo mudou o texto (pseudônimo re-detectado): %q -> %q", a, c)
	}
	// nome e e-mail da mesma pessoa ganham o mesmo identificador
	if !strings.Contains(a, "Pessoa ") {
		t.Fatalf("nome não mascarado: %q", a)
	}
	pid := a[strings.Index(a, "p.")+2 : strings.Index(a, "p.")+10]
	if !strings.Contains(a, "Pessoa "+pid) {
		t.Errorf("nome e e-mail sem o mesmo id: %q", a)
	}
	// mesma sub-rede /24 continua na mesma sub-rede falsa, com o mesmo host
	if !strings.Contains(a, ".15") || !strings.Contains(a, ".80") {
		t.Errorf("octeto do host não preservado: %q", a)
	}
	// outra chave -> outros pseudônimos
	m2, _ := NovoMasker(m.cfg, []byte("ffffffffffffffffffffffffffffffff"), nil, nil)
	if c, _ := m2.Mascarar(s); c == a {
		t.Error("chave diferente gerou os mesmos pseudônimos")
	}
}

func TestSenhas(t *testing.T) {
	pega := map[string]string{
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
		// na dúvida, mascara: nomes e referências com cara de valor também saem
		"secretRef: db-credentials":                   "db-credentials",
		"existingPasswordSecret: mysql8-secrets":      "mysql8-secrets",
		"password_file: /run/secrets/db_pass1":        "/run/secrets/db_pass1",
		"PASSWORD: cofre-producao:DB_PASSWORD:Atual1": "cofre-producao:DB_PASSWORD:Atual1",
	}
	for texto, senha := range pega {
		if out, _ := novoTeste(t).Mascarar(texto); strings.Contains(out, senha) {
			t.Errorf("senha passou: %q -> %q", texto, out)
		}
	}
	limpos := []string{
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
	for _, texto := range limpos {
		if out, ents := novoTeste(t).Mascarar(texto); len(ents) > 0 {
			t.Errorf("falso positivo: %q -> %q", texto, out)
		}
	}
}

func TestDetectoresExtras(t *testing.T) {
	tit := gerarValido("1234567801", 12, TituloValido)
	cns := gerarValido("7000000000000", 15, CNSValido)
	ren := gerarValido("6392748451", 11, RenavamValido)
	pega := map[string]string{
		"Authorization: Basic am9hbzpGaWN0aWNpYUAyMDI0":                             "am9hbzpGaWN0aWNpYUAyMDI0",
		"> authorization: Bearer 8f3a2b1c9d4e5f60718293a4b5c6d7e8":                  "8f3a2b1c9d4e5f60718293a4b5c6d7e8",
		"Cookie: JSESSIONID=A1B2C3D4E5F60718293A4B5C6D7E8F90; theme=dark":           "A1B2C3D4E5F60718293A4B5C6D7E8F90",
		"< Set-Cookie: sessionid=k2j3h4g5f6d7s8a9q0w1e2r3; Path=/; HttpOnly":        "k2j3h4g5f6d7s8a9q0w1e2r3",
		"processo 0001234-56.2023.8.13.0024 em andamento":                           "0001234-56.2023.8.13.0024",
		"fornecedor 12.ABC.345/01DE-35 ativo":                                       "12.ABC.345/01DE-35",
		"transferir para GB82 WEST 1234 5698 7654 32 hoje":                          "GB82 WEST 1234 5698 7654 32",
		"conta BR1500000000000010932840814P2.":                                      "BR1500000000000010932840814P2",
		"título de eleitor " + tit:                                                  tit,
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
	for texto, real := range pega {
		if out, _ := novoTeste(t).Mascarar(texto); strings.Contains(out, real) {
			t.Errorf("passou %q em %q -> %q", real, texto, out)
		}
	}
	fica := []string{
		"Cookie: theme=dark; lang=pt-BR",
		"Set-Cookie: a=b; Path=/; Max-Age=3600; SameSite=Lax",
		"versão 2023.8.13 do pacote, build 0001234",
		"hash GB82WESTxyz e código BR15 em análise",
		"o título do relatório tem 123456789012 caracteres",
		"placa de vídeo RTX4090 instalada",
		"sexo;idade;uf\nF;34;MG\n", // quase identificadores: desligados por padrão
		"card_type: visa",
		"status_code: 200\ncontent-type: application/json",
		"curl -H \"Authorization: Bearer ${TOKEN}\" https://x.test",
		"Authorization: Bearer <seu-token-aqui>",
		"client_id: 3f2a1b4c-5d6e-7f80-9a1b-2c3d4e5f6071\ntenant_id: AZURE_TENANT_ID",
		"WHERE user_id = :user_id",
	}
	for _, s := range fica {
		if out, ents := novoTeste(t).Mascarar(s); len(ents) > 0 {
			t.Errorf("mascarou sem precisar: %q -> %q", s, out)
		}
	}
}

// Nome importado em qualquer grafia.
func TestNomeQualquerGrafia(t *testing.T) {
	m := novoTeste(t)
	for _, s := range []string{"falei com joão silva ontem", "o JOAO CARLOS SILVA saiu", "Joao Carlos Silva", "ana paula souza aprovou"} {
		if out, _ := m.Mascarar(s); !strings.Contains(out, "Pessoa ") {
			t.Errorf("nome não mascarado em %q: %q", s, out)
		}
	}
	// palavras comuns que não são nome importado continuam intactas
	if out, _ := m.Mascarar("a silva e souza são sobrenomes comuns"); strings.Contains(out, "Pessoa ") {
		t.Errorf("falso positivo: %q", out)
	}
}

// Segredo dentro de base64 (Secret do Kubernetes), sem palavra-chave por perto.
func TestSegredoEmBase64(t *testing.T) {
	m := novoTeste(t)
	enc := base64.StdEncoding.EncodeToString([]byte("ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"))
	for _, s := range []string{
		"apiVersion: v1\nkind: Secret\ndata:\n  conexao: " + enc + "\n  outro: abc\n",
		enc + "\nlinha seguinte", // na primeira linha (coluna do gitleaks tem outro deslocamento)
	} {
		out, _ := m.Mascarar(s)
		if strings.Contains(out, enc) || strings.Contains(out, enc[:20]) {
			t.Errorf("base64 com segredo não mascarado:\n%s", out)
		}
	}
}

// Texto grande vai ao gitleaks em janelas. Um segredo em qualquer posição, inclusive em cima
// do corte, tem que ser achado como no texto inteiro.
func TestSegredoNaFronteiraDaJanela(t *testing.T) {
	tok := "ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"
	linha := "linha de log comum sem nada sensivel, status=200\n"
	for _, pos := range []int{0, janelaLeaks - 20, janelaLeaks - 5, janelaLeaks, janelaLeaks + 3, 2*janelaLeaks - sobraLeaks - 10, 3 * janelaLeaks} {
		s := strings.Repeat(linha, pos/len(linha)+1)[:pos] + "\ntoken=" + tok + "\n" + strings.Repeat(linha, 2*janelaLeaks/len(linha))
		if out, _ := novoTeste(t).Mascarar(s); strings.Contains(out, tok) {
			t.Errorf("token na posição %d (janela de %d) passou", pos, janelaLeaks)
		}
	}
	// sem nenhuma quebra de linha (um bloco só)
	bloco := strings.Repeat("a ", janelaLeaks/2-10) + "token=" + tok + " " + strings.Repeat("b ", janelaLeaks)
	if out, _ := novoTeste(t).Mascarar(bloco); strings.Contains(out, tok) {
		t.Error("token em cima do corte, sem quebra de linha, passou")
	}
}

// Item 1 e 2 (revisão externa): CPF e CNPJ válidos, só com dígitos, sem a palavra "cpf" por
// perto e sem nome de coluna reconhecido. Reproduz uma consulta com UNION em que a coluna
// recebeu o alias "valor": os valores saíam em claro.
func TestDocumentoSoDigitosSemContexto(t *testing.T) {
	cpf1 := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("318452760"))
	cpf2 := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("529982247"))
	cnpj := strings.NewReplacer(".", "", "-", "", "/", "").Replace("11.222.333/0001-81")
	casos := map[string][]string{
		"valor\n" + cpf1 + "\n" + cpf2 + "\n":               {cpf1, cpf2},
		`{"id": 7, "userDocument": "` + cpf1 + `"}`:         {cpf1},
		"o resultado da consulta foi " + cpf2 + " e pronto": {cpf2},
		"fornecedor " + cnpj + " ativo":                     {cnpj},
	}
	for texto, reais := range casos {
		out, _ := novoTeste(t).Mascarar(texto)
		for _, r := range reais {
			if strings.Contains(out, r) {
				t.Errorf("passou %q em %q -> %q", r, texto, out)
			}
		}
	}
}

// As exceções do CPF/CNPJ sem contexto: pedaço de hash, parte decimal, número com versão e
// carimbo de data e hora (14 dígitos) não são documentos.
func TestDocumentoSoDigitosExcecoes(t *testing.T) {
	cpf := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("318452760"))
	fica := []string{
		"hash a" + cpf + "f e id_" + cpf,
		"pi vale 3." + cpf,
		"versão v1." + cpf + ".2",
		"carimbo 20261004104220 gravado",
	}
	for _, s := range fica {
		if out, ents := novoTeste(t).Mascarar(s); len(ents) > 0 {
			t.Errorf("mascarou sem precisar: %q -> %q", s, out)
		}
	}
	cfg := novoTeste(t).cfg
	cfg.DocumentosSemContexto = false
	m, _ := NovoMasker(cfg, chaveTeste, nil, nil)
	if out, _ := m.Mascarar("valor " + cpf); !strings.Contains(out, cpf) {
		t.Errorf("com documentos_sem_contexto desligado, deveria ficar como está: %q", out)
	}
}
