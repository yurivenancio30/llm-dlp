package mask

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Ida e volta: o valor é mascarado com contexto, o modelo o repete num texto neutro, o
// proxy desmascara para o usuário e, na mensagem seguinte, o histórico volta com o valor
// real SEM o contexto. Ele tem que ser mascarado de novo (e com o mesmo pseudônimo).
func TestIdaEVoltaSemContexto(t *testing.T) {
	cpf := strings.NewReplacer(".", "", "-", "").Replace(gerarCPF("529982247"))
	casos := []struct{ nome, original, real string }{
		{"rg", "RG: 12.345.678-9", "12.345.678-9"},
		{"cnh", "CNH " + gerarOnze("123456789", CNHValida), gerarOnze("123456789", CNHValida)},
		{"pis", "PIS " + gerarOnze("1201234567", PISValido), gerarOnze("1201234567", PISValido)},
		{"cep", "CEP 01310-100", "01310-100"},
		{"conta", "conta corrente 123456-7", "123456-7"},
		{"agencia", "agência 4321-0", "4321-0"},
		{"pix", "chave pix 123e4567-e89b-42d3-a456-426614174000", "123e4567-e89b-42d3-a456-426614174000"},
		{"nascimento", "data de nascimento: 12/03/1985", "12/03/1985"},
		{"telefone", "celular 11 98765-4321", "11 98765-4321"},
		{"cartao", "cartão 4111111111111111", "4111111111111111"},
		{"cpf", "cpf " + cpf, cpf},
		{"senha", "password: Xk9mP2qL7vT4zR8w", "Xk9mP2qL7vT4zR8w"},
	}
	for _, c := range casos {
		m := novoTeste(t)
		mas, ents := m.Mascarar(c.original)
		if strings.Contains(mas, c.real) || len(ents) == 0 {
			t.Errorf("%s: nem a primeira passada mascarou: %q", c.nome, mas)
			continue
		}
		resposta := "Anotado: o valor " + ents[0].Pseudo + " foi registrado."
		historico := NovaTabela(ents).Desmascarar(resposta, false) // é o que o Claude Code guarda
		if !strings.Contains(historico, c.real) {
			t.Fatalf("%s: desmascarar falhou: %q", c.nome, historico)
		}
		de_novo, _ := m.Mascarar(historico)
		if strings.Contains(de_novo, c.real) {
			t.Errorf("%s: VAZA na volta: %q", c.nome, de_novo)
		} else if de_novo != resposta {
			t.Errorf("%s: pseudônimo mudou na volta (quebra o cache): %q != %q", c.nome, de_novo, resposta)
		}
	}
}

// Depois de um reinício do proxy a memória some; só os hashes em disco restam. O valor
// repetido sem contexto ainda tem que ser reconhecido.
func TestIdaEVoltaAposReinicio(t *testing.T) {
	dir := t.TempDir()
	novo := func() *Masker {
		vs, _ := CarregarVistos(dir + "/vistos.json")
		m, err := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m1 := novo()
	for _, s := range []string{"RG: 12.345.678-9", "password: Xk9mP2qL7vT4zR8w", "CEP 01310-100", "celular 11 98765-4321"} {
		m1.Mascarar(s)
	}
	if err := m1.vistos.SalvarSeSujo(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dir + "/vistos.json"); strings.Contains(string(b), "12.345") || strings.Contains(string(b), "Xk9mP2") {
		t.Fatalf("valor legível gravado em disco: %s", b)
	}
	m2 := novo() // "reiniciou": memória vazia
	for _, real := range []string{"12.345.678-9", "Xk9mP2qL7vT4zR8w", "01310-100", "11 98765-4321"} {
		if out, _ := m2.Mascarar("o valor " + real + " apareceu de novo"); strings.Contains(out, real) {
			t.Errorf("após reinício, vazou: %q", out)
		}
	}
}

// O valor apareceu solto ANTES de ser aprendido: o resultado memorizado daquele texto
// tem que ser refeito quando o valor vira conhecido.
func TestMemoRefeitoQuandoAprende(t *testing.T) {
	m := novoTeste(t)
	solto := "pedido referente a 12.345.678-9 em análise"
	if out, _ := m.Mascarar(solto); out != solto {
		t.Fatalf("sem contexto não deveria mascarar ainda: %q", out)
	}
	m.Mascarar("RG: 12.345.678-9")
	if out, _ := m.Mascarar(solto); strings.Contains(out, "12.345.678-9") {
		t.Errorf("memo antigo reaproveitado depois de aprender o valor: %q", out)
	}
}

// Aprender um número curto não pode fazer números parecidos virarem dado sensível, e a
// mesma numeração com outra pontuação é reconhecida e volta sem conflito.
func TestConhecidosSemFalsoPositivo(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("agência 4321-0")
	limpo := "a porta 43210 respondeu em 43210 ms; linha 4321 ok"
	if out, _ := m.Mascarar(limpo); out != limpo {
		t.Errorf("número curto aprendido gerou falso positivo: %q", out)
	}
	m.Mascarar("RG: 12.345.678-9")
	mas, ents := m.Mascarar("cadastro 123456789 e também 12.345.678-9")
	if strings.Contains(mas, "123456789") || strings.Contains(mas, "12.345.678-9") {
		t.Fatalf("outra pontuação do mesmo RG não foi mascarada: %q", mas)
	}
	tab := NovaTabela(ents)
	if tab.Conflitos != 0 {
		t.Errorf("mesma numeração com pontuações diferentes foi tratada como colisão")
	}
}

// Depois de um reinício só o hash existe. Quando o valor é reconhecido pelo hash, ele volta
// para a memória; daí em diante é pego também grudado em outra coisa.
func TestReconhecidoPeloHashVoltaParaAMemoria(t *testing.T) {
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	m.Mascarar("DB_PASSWORD=Ficticia@2024")
	vs.SalvarSeSujo()

	vs2, _ := CarregarVistos(dir + "/v.json")
	m2, _ := NovoMasker(m.cfg, chaveTeste, nil, vs2) // "reiniciou": memória vazia
	// primeira aparição depois do reinício já grudada, sem palavra-chave: pega pelos pedaços
	for _, grudado := range []string{"GET /x?a=1&k=Ficticia@2024&b=2", "mysql://app:Ficticia@2024@db1:3306/x", "v=Ficticia@2024"} {
		vs3, _ := CarregarVistos(dir + "/v.json")
		m3, _ := NovoMasker(m.cfg, chaveTeste, nil, vs3)
		if out, _ := m3.Mascarar(grudado); strings.Contains(out, "Ficticia@2024") {
			t.Fatalf("grudado, logo depois do reinício, passou: %s", out)
		}
	}
	if out, _ := m2.Mascarar("erro ao logar com Ficticia@2024 no banco"); strings.Contains(out, "Ficticia@2024") {
		t.Fatalf("solto, depois do reinício, deveria ser pego pelo hash: %s", out)
	}
	// reconhecido pelo hash, volta para a memória: daí em diante é pego em qualquer posição
	if out, _ := m2.Mascarar("xFicticia@2024;y e (Ficticia@2024)"); strings.Count(out, "Ficticia@2024") != 1 {
		t.Fatalf("esperava só a ocorrência colada em letra sem máscara: %s", out)
	}
}

// A busca pelo índice tem que achar exatamente o que a busca ingênua (valor por valor) acha.
func TestVarrerIgualABuscaIngenua(t *testing.T) {
	c := novosConhecidos()
	vals := []string{"Xk9$mQ2vLp7w", "12.345.678-9", "1234-5", "abcd", "abcdef", "Xk9$", "ção-ñ9"}
	for _, v := range vals {
		c.aprender("segredo", v)
	}
	texto := "a Xk9$mQ2vLp7w b 12.345.678-9, (1234-5) xabcd abcdef abcd. 912.345.678-9 Xk9$ ção-ñ9 abc 1234-56"
	ingenua := map[string]bool{}
	for _, v := range vals {
		for i := 0; ; {
			j := strings.Index(texto[i:], v)
			if j < 0 {
				break
			}
			ini, fim := i+j, i+j+len(v)
			i = ini + 1
			if ehAlnum(v[0]) && ini > 0 && ehAlnum(texto[ini-1]) {
				continue
			}
			if ehAlnum(v[len(v)-1]) && fim < len(texto) && ehAlnum(texto[fim]) {
				continue
			}
			ingenua[fmt.Sprint(ini, "-", fim)] = true
		}
	}
	indice := map[string]bool{}
	c.varrer(texto, 0, func(ini, fim int, _ string) bool { indice[fmt.Sprint(ini, "-", fim)] = true; return true })
	if len(ingenua) < 7 || fmt.Sprint(ingenua) != fmt.Sprint(indice) {
		t.Fatalf("ingênua %v\níndice  %v", ingenua, indice)
	}
}

// A memória em RAM tem teto; o que sai dela continua reconhecido pelo hash em disco.
func TestConhecidosTemTeto(t *testing.T) {
	vs, _ := CarregarVistos(t.TempDir() + "/v.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	primeiro := "senha: Zq8wPrimeiroValor01"
	m.Mascarar(primeiro)
	for i := 0; i < maxConhecidos+10; i++ {
		m.conh.aprender("segredo", fmt.Sprintf("Lixo%08dabc", i))
	}
	if n := len(m.conh.reais); n > maxConhecidos {
		t.Fatalf("RAM sem teto: %d valores", n)
	}
	if len(m.conh.tipo) != len(m.conh.reais) || len(m.conh.seq) != len(m.conh.reais) {
		t.Fatalf("mapas não acompanharam a lista: %d %d %d", len(m.conh.tipo), len(m.conh.seq), len(m.conh.reais))
	}
	if _, ok := m.conh.tipo["Zq8wPrimeiroValor01"]; ok {
		t.Fatal("o valor mais antigo deveria ter saído da RAM")
	}
	if out, _ := m.Mascarar("erro com Zq8wPrimeiroValor01 no log"); strings.Contains(out, "Zq8wPrimeiroValor01") {
		t.Fatalf("valor que saiu da RAM vazou: %s", out)
	}
	// texto memorizado antes da saída: não dá mais para conferir, então é refeito
	if !m.conh.contemDesde("qualquer", 0) {
		t.Fatal("geração anterior ao teto deveria forçar refazer")
	}
}

func TestVistosTemTeto(t *testing.T) {
	v, _ := CarregarVistos(t.TempDir() + "/v.json")
	for i := 0; i < maxVistos+1000; i++ {
		v.Marcar(fmt.Sprint("id", i), "rg")
	}
	if len(v.ids) > maxVistos || v.n["rg"] != len(v.ids) {
		t.Fatalf("hashes sem teto: %d (contador %d)", len(v.ids), v.n["rg"])
	}
}

// Item 4 (revisão externa): um valor que passou em claro num texto e só depois foi aprendido
// (em outra grafia) não pode continuar em claro quando o histórico é reenviado. O memo do
// texto antigo tem que ser refeito.
func TestMemoRefeitoQuandoAprendeOutraGrafia(t *testing.T) {
	m := novoTeste(t)
	antigo := "linha 7: 123456789;ATIVO"
	if out, _ := m.Mascarar(antigo); !strings.Contains(out, "123456789") {
		t.Skip("premissa mudou: o número já é mascarado sozinho")
	}
	m.Mascarar("RG: 12.345.678-9`") // aprende o mesmo número, com pontuação
	if out, _ := m.Mascarar(antigo); strings.Contains(out, "123456789") {
		t.Fatalf("o texto antigo continuou em claro depois de o valor ser aprendido: %q", out)
	}
}

// Uma palavra comum mascarada como senha (exemplo numa URL de documentação) não pode ser
// lembrada: senão toda ocorrência dela vira segredo e o histórico inteiro é reescrito.
func TestPalavraComumNaoViraSegredoLembrado(t *testing.T) {
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	m.Mascarar("export HTTPS_PROXY=http://username:pinguim@proxy.example.com:8080")
	if out, ents := m.Mascarar("o pinguim mora no polo sul"); len(ents) > 0 {
		t.Fatalf("palavra comum foi lembrada como segredo: %q", out)
	}
	// e uma senha de verdade continua sendo lembrada
	m.Mascarar("DB_PASSWORD=Pinguim#2026")
	if out, _ := m.Mascarar("tentei Pinguim#2026 e falhou"); strings.Contains(out, "Pinguim#2026") {
		t.Fatalf("senha real deixou de ser lembrada: %q", out)
	}
	// palavra aprendida por versão antiga (hash em disco) é ignorada na consulta
	vs.Marcar(m.p.ID("visto", "s:pinguim"), "segredo")
	vs.SalvarSeSujo()
	vs2, _ := CarregarVistos(dir + "/v.json")
	m2, _ := NovoMasker(m.cfg, chaveTeste, nil, vs2)
	if out, ents := m2.Mascarar("o pinguim mora no polo sul"); len(ents) > 0 {
		t.Fatalf("palavra guardada por versão antiga continuou sendo mascarada: %q", out)
	}
}
