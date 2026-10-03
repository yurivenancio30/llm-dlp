package mask

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

func TestIdaEVolta(t *testing.T) {
	m := novoTeste(t)
	s := "Owner: joao.silva@empresa-ficticia.com.br\nHost: mysql-dh.empresa-ficticia.intra (10.42.7.15)\n" +
		"JOAO CARLOS SILVA, cpf " + gerarCPF("529982247") + ", tel (11) 98765-4321, token=ghp_k3Jd9sLq2mXv8RtY7wPz4nBc6HfG1aE5uQiO"
	mas, ents := m.Mascarar(s)
	for _, real := range []string{"joao.silva", "mysql-dh", "10.42.7.15", "JOAO CARLOS", "98765", "ghp_"} {
		if strings.Contains(mas, real) {
			t.Errorf("vazou %q: %s", real, mas)
		}
	}
	if volta := NovaTabela(ents).Desmascarar(mas, false); volta != s {
		t.Errorf("ida e volta não bateu:\n%s\n---\n%s", s, volta)
	}
}

// Streaming: o texto mascarado é cortado em TODAS as posições (2 pedaços) e em
// pedaços aleatórios; o que sai tem que ser exatamente o texto real. Repete com várias
// chaves, porque cada chave gera pseudônimos diferentes (e coincidências diferentes).
func TestFluxoCortado(t *testing.T) {
	real := "a pessoa joao.silva@empresa-ficticia.com.br (JOAO CARLOS SILVA) usa 10.42.7.15 e 10.42.7.200/24; ok 2"
	for c := 0; c < 50; c++ {
		chave := []byte(fmt.Sprintf("%032d", c))
		ps, _ := CarregarPessoas(t.TempDir() + "/p.json")
		ps.Importar(NovoPseudo(chave), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "")
		mc, _ := NovoMasker(config.Padrao(), chave, ps, nil)
		mas, ents := mc.Mascarar(real)
		tab := NovaTabela(ents)
		for corte := 0; corte <= len(mas); corte++ {
			f := tab.NovoFluxo(false)
			out := f.Empurrar(mas[:corte]) + f.Empurrar(mas[corte:]) + f.Fechar()
			if out != real {
				t.Fatalf("chave %d, corte em %d: %q", c, corte, out)
			}
		}
	}
	m := novoTeste(t)
	mas, ents := m.Mascarar(real)
	tab := NovaTabela(ents)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		f := tab.NovoFluxo(false)
		var b strings.Builder
		for j := 0; j < len(mas); {
			n := 1 + r.Intn(7)
			if j+n > len(mas) {
				n = len(mas) - j
			}
			b.WriteString(f.Empurrar(mas[j : j+n]))
			j += n
		}
		b.WriteString(f.Fechar())
		if b.String() != real {
			t.Fatalf("pedaços aleatórios: %q", b.String())
		}
	}
}

func TestFluxoJSON(t *testing.T) {
	m := novoTeste(t)
	mas, ents := m.Mascarar(`grep "joao.silva@empresa-ficticia.com.br" a.txt`)
	tab := NovaTabela(ents)
	f := tab.NovoFluxo(true)
	out := f.Empurrar(`{"command":"`+mas[:10]) + f.Empurrar(mas[10:]+`"}`) + f.Fechar()
	if !strings.Contains(out, "joao.silva@empresa-ficticia.com.br") {
		t.Fatalf("json: %s", out)
	}
}

// Colisão: dois reais com o mesmo pseudônimo nunca são desmascarados (seria escolher um).
func TestColisaoNaoDesmascara(t *testing.T) {
	tab := NovaTabela([]Entrada{
		{"pessoa.aaaa@dom-x.invalid", "a@empresa.com", "email"},
		{"pessoa.aaaa@dom-x.invalid", "b@empresa.com", "email"},
		{"250.1.2.15", "10.0.0.15", "ip"},
		{"250.1.2.16", "10.9.9.16", "ip"}, // mesma sub-rede falsa, sub-redes reais diferentes
		{"pessoa.bbbb@dom-x.invalid", "c@empresa.com", "email"},
	})
	got := tab.Desmascarar("x pessoa.aaaa@dom-x.invalid y 250.1.2.15 z 250.1.2.99 w pessoa.bbbb@dom-x.invalid", false)
	want := "x pessoa.aaaa@dom-x.invalid y 250.1.2.15 z 250.1.2.99 w c@empresa.com"
	if got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
	if tab.Conflitos != 2 {
		t.Errorf("conflitos = %d, esperado 2", tab.Conflitos)
	}
}

func TestConfigSemDesmascarar(t *testing.T) {
	c := config.Padrao()
	if !c.SemDesmascarar("WebFetch") || !c.SemDesmascarar("WebSearch") || c.SemDesmascarar("Bash") {
		t.Error("padrão de ferramentas sem desmascarar incorreto")
	}
}

// Desmascarar com muitos pseudônimos distintos (um CSV de e-mails na conversa) tem que
// continuar rápido e correto.
func TestDesmascararComMuitosPseudonimos(t *testing.T) {
	m := novoTeste(t)
	var real strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&real, "linha %d: fulano%d@empresa-ficticia.com.br em 10.%d.%d.%d\n", i, i, i%5, i%8, i%251) // 40 sub-redes
	}
	mas, ents := m.Mascarar(real.String())
	if strings.Contains(mas, "fulano") {
		t.Fatal("e-mail passou")
	}
	t0 := time.Now()
	tab := NovaTabela(ents)
	volta := tab.Desmascarar(mas, false)
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("montar a tabela e desmascarar %d KB com %d pseudônimos levou %s", len(mas)>>10, len(tab.m), d)
	}
	if volta != real.String() {
		t.Error("ida e volta não bateu")
	}
	f := tab.NovoFluxo(false)
	var b strings.Builder
	for i := 0; i < len(mas); i += 11 {
		b.WriteString(f.Empurrar(mas[i:min(i+11, len(mas))]))
	}
	b.WriteString(f.Fechar())
	if b.String() != real.String() {
		t.Error("ida e volta em streaming não bateu")
	}
}

// O modelo às vezes cita só o domínio do e-mail mascarado: ele também tem que voltar ao real.
func TestDominioDoEmailVolta(t *testing.T) {
	m := novoTeste(t)
	mas, ents := m.Mascarar("o e-mail é joao.silva@empresa-ficticia.com.br")
	dom := mas[strings.LastIndexByte(mas, '@')+1:]
	if volta := NovaTabela(ents).Desmascarar("o domínio "+dom+" não aparece em vazamentos", false); !strings.Contains(volta, "empresa-ficticia.com.br") {
		t.Fatalf("domínio sozinho não voltou: %q", volta)
	}
}
