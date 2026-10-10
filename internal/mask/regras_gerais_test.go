package mask

import (
	"strings"
	"testing"
	"time"
)

// Regras que valem para todos os leitores. Todos os nomes são fictícios.

// pseudoDe: o pseudônimo que substituiu real (pelas entradas).
func pseudoDe(m *Masker, s, real string) (ps, tipo string) {
	_, ents := m.Mascarar(s)
	for _, e := range ents {
		if e.Real == real {
			return e.Pseudo, e.Tipo
		}
	}
	return "", ""
}

// Duas regras que discordam do tipo do mesmo nome. No mesmo texto, um tipo só; depois
// de aprendido, o tipo (e o pseudônimo) ficam, também depois de um reinício.
func TestTipoConsistenteEntreRegras(t *testing.T) {
	const nome = "xq-ped-api01"
	// leitores sintéticos: um acha o nome como pasta na 1ª ocorrência, o outro como serviço na 2ª
	m := novoTeste(t)
	m.UsarLeitores([]Leitor{
		{Nome: "a", Achar: func(s string, add func(ObjAchado)) {
			if i := strings.Index(s, nome); i >= 0 {
				add(ObjAchado{i, i + len(nome), "pasta", "a", false})
			}
		}},
		{Nome: "b", Achar: func(s string, add func(ObjAchado)) {
			if i := strings.LastIndex(s, nome); i >= 0 {
				add(ObjAchado{i, i + len(nome), "servico", "b", true})
			}
		}},
	})
	out, _ := m.Mascarar("/srv/" + nome + "/conf e deployment/" + nome)
	if strings.Contains(out, nome) {
		t.Fatalf("ficou legível: %s", out)
	}
	if strings.Count(out, "dir_")+strings.Count(out, "svc_") != 2 || strings.Contains(out, "dir_") && strings.Contains(out, "svc_") {
		t.Errorf("o mesmo nome com dois pseudônimos no mesmo texto: %s", out)
	}

	// regras reais: o nome aprendido como serviço (linha de comando) e depois achado como pasta
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m1, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	ps1, tp1 := pseudoDe(m1, "kubectl rollout restart deployment/"+nome+" -n xq-ns01", nome)
	if tp1 != "obj.servico" {
		t.Fatalf("tipo pela linha de comando: %q", tp1)
	}
	if ps2, tp2 := pseudoDe(m1, "ls /srv/"+nome+"/conf", nome); ps2 != ps1 || tp2 != tp1 {
		t.Errorf("pasta depois de aprendido como serviço: %q %q (esperado %q %q)", ps2, tp2, ps1, tp1)
	}
	// depois do reinício (só o vistos.json)
	vs.SalvarSeSujo()
	vs2, _ := CarregarVistos(dir + "/v.json")
	m2, _ := NovoMasker(m1.cfg, chaveTeste, nil, vs2)
	if ps3, tp3 := pseudoDe(m2, "ls /srv/"+nome+"/conf", nome); ps3 != ps1 || tp3 != tp1 {
		t.Errorf("depois do reinício: %q %q (esperado %q %q)", ps3, tp3, ps1, tp1)
	}
	// e na ordem contrária, no mesmo texto e sem nada aprendido: um pseudônimo só
	m3, _ := NovoMasker(m1.cfg, chaveTeste, nil, nil)
	out, ents := m3.Mascarar("ls /srv/" + nome + "/conf\nkubectl rollout restart deployment/" + nome + " -n xq-ns01")
	tipos := map[string]bool{}
	for _, e := range ents {
		if e.Real == nome {
			tipos[e.Tipo] = true
		}
	}
	if len(tipos) != 1 || strings.Contains(out, nome) {
		t.Errorf("mesmo texto, dois tipos %v: %s", tipos, out)
	}
}

// Prefixo de literal de string do Python não é o valor do campo; valor de 1 ou 2
// letras não é mascarado; padrão de regex no valor não é dado.
func TestCampoPrefixoLiteral(t *testing.T) {
	m := novoTeste(t)
	q := "'"
	fica := []string{
		"PADROES = {'RACA': " + "r" + q + `(?!x)\b(branca|preta)\b` + q + ", 'RELIGIAO': " + "rb" + `"\w+"` + "}",
		`{"raca": ` + "f" + q + "{valor}" + q + "}",
		"raca: " + "M",
		"religiao: " + "pt",
		"etnia = " + "Rf" + q + `^\d+$` + q,
	}
	for _, s := range fica {
		if out, _ := m.Mascarar(s); out != s {
			t.Errorf("máscara a mais:\n   %q\n-> %q", s, out)
		}
	}
	// o valor de verdade, com ou sem prefixo, continua mascarado (e o prefixo fica)
	valor := "Parda"
	for _, s := range []string{"{'RACA': " + "u" + q + valor + q + "}", "{'RACA': " + q + valor + q + "}", "raca: " + valor} {
		out, _ := m.Mascarar(s)
		if strings.Contains(out, valor) {
			t.Errorf("valor do campo ficou legível: %q -> %q", s, out)
		}
		if strings.Contains(s, "u"+q) && !strings.Contains(out, ": u"+q) {
			t.Errorf("o prefixo foi junto: %q -> %q", s, out)
		}
	}
}

// Nome aprendido com ponto é reconhecido em prosa depois do reinício.
func TestNomeComPontoDepoisDoReinicio(t *testing.T) {
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	const nome = "xq_topico01.eventos"
	if out, _ := m.Mascarar("KAFKA_TOPIC=" + nome); strings.Contains(out, nome) {
		t.Fatalf("não aprendeu: %s", out)
	}
	prosa := "o tópico " + nome + " parou de receber; veja " + nome + "."
	if out, _ := m.Mascarar(prosa); strings.Contains(out, "xq_topico01") {
		t.Fatalf("antes do reinício: %s", out)
	}
	vs.SalvarSeSujo()
	vs2, _ := CarregarVistos(dir + "/v.json")
	m2, _ := NovoMasker(m.cfg, chaveTeste, nil, vs2)
	out, _ := m2.Mascarar(prosa)
	if strings.Contains(out, "xq_topico01") || strings.Contains(out, "eventos") {
		t.Errorf("depois do reinício: %s", out)
	}
	// o pedaço sozinho não é o nome: "xq_topico01" em outro contexto não vira o pseudônimo do nome com ponto
	if out, _ := m2.Mascarar("eventos de hoje"); out != "eventos de hoje" {
		t.Errorf("palavra comum mascarada: %s", out)
	}
}

// URLs coladas sem separador e "a://" repetido não são quadráticos.
func TestURLsColadasLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("mede tempo")
	}
	m := novoTeste(t)
	for _, tipo := range []string{"urlcolada", "esquemas"} {
		s := linhaLonga(tipo, 1<<20)
		ini := time.Now()
		m.Detectar(s)
		if d := time.Since(ini); d > 10*time.Second {
			t.Errorf("%s 1 MB: %v", tipo, d)
		}
	}
}

// DatabaseName= dentro de URL JDBC em string Java (a URI conta como o outro par).
func TestJDBCDatabaseNameEmString(t *testing.T) {
	m := novoTeste(t)
	confere(t, m, `props.put("url", "jdbc:sqlserver://xq-srv01;databaseName=xq_banco01");`, []string{"xq_banco01", "xq-srv01"}, []string{"props.put", "databaseName="})
	confere(t, m, `String url = "jdbc:sqlserver://xq-srv01:1433;databaseName=xq_banco02;encrypt=true";`, []string{"xq_banco02", "xq-srv01"}, []string{"encrypt=true"})
	confere(t, m, `url = 'jdbc:postgresql://xq-srv02:5432/xq_banco03?currentSchema=xq_esq01'`, []string{"xq_banco03", "xq_esq01", "xq-srv02"}, nil)
	// sem URI de banco, um par só não é string de conexão
	confere(t, m, `veja o campo databaseName=exemplo no formulário`, nil, []string{"databaseName=exemplo"})
}
