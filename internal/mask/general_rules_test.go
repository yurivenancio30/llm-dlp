package mask

import (
	"strings"
	"testing"
	"time"
)

// Regras que valem para todos os leitores. Todos os nomes são fictícios.

// pseudoOf: o pseudônimo que substituiu real (pelas entradas).
func pseudoOf(m *Masker, s, real string) (ps, typ string) {
	_, ents := m.Mask(s)
	for _, e := range ents {
		if e.Real == real {
			return e.Pseudo, e.Type
		}
	}
	return "", ""
}

// Duas regras que discordam do tipo do mesmo nome. No mesmo texto, um tipo só; depois
// de aprendido, o tipo (e o pseudônimo) ficam, também depois de um reinício.
func TestTypeConsistentAcrossRules(t *testing.T) {
	const name = "xq-ped-api01"
	// leitores sintéticos: um acha o nome como pasta na 1ª ocorrência, o outro como serviço na 2ª
	m := newTest(t)
	m.UseReaders([]Reader{
		{Name: "a", Find: func(s string, add func(ObjFinding)) {
			if i := strings.Index(s, name); i >= 0 {
				add(ObjFinding{i, i + len(name), "pasta", "a", false})
			}
		}},
		{Name: "b", Find: func(s string, add func(ObjFinding)) {
			if i := strings.LastIndex(s, name); i >= 0 {
				add(ObjFinding{i, i + len(name), "servico", "b", true})
			}
		}},
	})
	out, _ := m.Mask("/srv/" + name + "/conf e deployment/" + name)
	if strings.Contains(out, name) {
		t.Fatalf("ficou legível: %s", out)
	}
	if strings.Count(out, "dir_")+strings.Count(out, "svc_") != 2 || strings.Contains(out, "dir_") && strings.Contains(out, "svc_") {
		t.Errorf("o mesmo nome com dois pseudônimos no mesmo texto: %s", out)
	}

	// regras reais: o nome aprendido como serviço (linha de comando) e depois achado como pasta
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m1, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	ps1, tp1 := pseudoOf(m1, "kubectl rollout restart deployment/"+name+" -n xq-ns01", name)
	if tp1 != "obj.servico" {
		t.Fatalf("tipo pela linha de comando: %q", tp1)
	}
	if ps2, tp2 := pseudoOf(m1, "ls /srv/"+name+"/conf", name); ps2 != ps1 || tp2 != tp1 {
		t.Errorf("pasta depois de aprendido como serviço: %q %q (esperado %q %q)", ps2, tp2, ps1, tp1)
	}
	// depois do reinício (só o vistos.json)
	vs.SaveIfDirty()
	vs2, _ := LoadSeen(dir + "/v.json")
	m2, _ := NewMasker(m1.cfg, testKey, nil, vs2)
	if ps3, tp3 := pseudoOf(m2, "ls /srv/"+name+"/conf", name); ps3 != ps1 || tp3 != tp1 {
		t.Errorf("depois do reinício: %q %q (esperado %q %q)", ps3, tp3, ps1, tp1)
	}
	// e na ordem contrária, no mesmo texto e sem nada aprendido: um pseudônimo só
	m3, _ := NewMasker(m1.cfg, testKey, nil, nil)
	out, ents := m3.Mask("ls /srv/" + name + "/conf\nkubectl rollout restart deployment/" + name + " -n xq-ns01")
	types := map[string]bool{}
	for _, e := range ents {
		if e.Real == name {
			types[e.Type] = true
		}
	}
	if len(types) != 1 || strings.Contains(out, name) {
		t.Errorf("mesmo texto, dois tipos %v: %s", types, out)
	}
}

// Prefixo de literal de string do Python não é o valor do campo; valor de 1 ou 2
// letras não é mascarado; padrão de regex no valor não é dado.
func TestLiteralPrefixField(t *testing.T) {
	m := newTest(t)
	q := "'"
	stays := []string{
		"PADROES = {'RACA': " + "r" + q + `(?!x)\b(branca|preta)\b` + q + ", 'RELIGIAO': " + "rb" + `"\w+"` + "}",
		`{"raca": ` + "f" + q + "{valor}" + q + "}",
		"raca: " + "M",
		"religiao: " + "pt",
		"etnia = " + "Rf" + q + `^\d+$` + q,
	}
	for _, s := range stays {
		if out, _ := m.Mask(s); out != s {
			t.Errorf("máscara a mais:\n   %q\n-> %q", s, out)
		}
	}
	// o valor de verdade, com ou sem prefixo, continua mascarado (e o prefixo fica)
	value := "Parda"
	for _, s := range []string{"{'RACA': " + "u" + q + value + q + "}", "{'RACA': " + q + value + q + "}", "raca: " + value} {
		out, _ := m.Mask(s)
		if strings.Contains(out, value) {
			t.Errorf("valor do campo ficou legível: %q -> %q", s, out)
		}
		if strings.Contains(s, "u"+q) && !strings.Contains(out, ": u"+q) {
			t.Errorf("o prefixo foi junto: %q -> %q", s, out)
		}
	}
}

// Name aprendido com ponto é reconhecido em prosa depois do reinício.
func TestDottedNameAfterRestart(t *testing.T) {
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	const name = "xq_topico01.eventos"
	if out, _ := m.Mask("KAFKA_TOPIC=" + name); strings.Contains(out, name) {
		t.Fatalf("não aprendeu: %s", out)
	}
	prose := "o tópico " + name + " parou de receber; veja " + name + "."
	if out, _ := m.Mask(prose); strings.Contains(out, "xq_topico01") {
		t.Fatalf("antes do reinício: %s", out)
	}
	vs.SaveIfDirty()
	vs2, _ := LoadSeen(dir + "/v.json")
	m2, _ := NewMasker(m.cfg, testKey, nil, vs2)
	out, _ := m2.Mask(prose)
	if strings.Contains(out, "xq_topico01") || strings.Contains(out, "eventos") {
		t.Errorf("depois do reinício: %s", out)
	}
	// o pedaço sozinho não é o nome: "xq_topico01" em outro contexto não vira o pseudônimo do nome com ponto
	if out, _ := m2.Mask("eventos de hoje"); out != "eventos de hoje" {
		t.Errorf("palavra comum mascarada: %s", out)
	}
}

// URLs coladas sem separador e "a://" repetido não são quadráticos.
func TestGluedURLsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("mede tempo")
	}
	m := newTest(t)
	for _, typ := range []string{"urlcolada", "esquemas"} {
		s := longLine(typ, 1<<20)
		ini := time.Now()
		m.Detect(s)
		if d := time.Since(ini); d > 10*time.Second {
			t.Errorf("%s 1 MB: %v", typ, d)
		}
	}
}

// DatabaseName= dentro de URL JDBC em string Java (a URI conta como o outro par).
func TestJDBCDatabaseNameInString(t *testing.T) {
	m := newTest(t)
	check(t, m, `props.put("url", "jdbc:sqlserver://xq-srv01;databaseName=xq_banco01");`, []string{"xq_banco01", "xq-srv01"}, []string{"props.put", "databaseName="})
	check(t, m, `String url = "jdbc:sqlserver://xq-srv01:1433;databaseName=xq_banco02;encrypt=true";`, []string{"xq_banco02", "xq-srv01"}, []string{"encrypt=true"})
	check(t, m, `url = 'jdbc:postgresql://xq-srv02:5432/xq_banco03?currentSchema=xq_esq01'`, []string{"xq_banco03", "xq_esq01", "xq-srv02"}, nil)
	// sem URI de banco, um par só não é string de conexão
	check(t, m, `veja o campo databaseName=exemplo no formulário`, nil, []string{"databaseName=exemplo"})
}
