package mask

import (
	"strings"
	"testing"
	"time"
)

// Rules that apply to every reader. All names are fictitious.

// pseudoOf: the pseudonym that replaced real (by the entries).
func pseudoOf(m *Masker, s, real string) (ps, typ string) {
	_, ents := m.Mask(s)
	for _, e := range ents {
		if e.Real == real {
			return e.Pseudo, e.Type
		}
	}
	return "", ""
}

// Two rules that disagree on the type of the same name. In the same text, one type only; once
// learned, the type (and the pseudonym) stay, also after a restart.
func TestTypeConsistentAcrossRules(t *testing.T) {
	const name = "xq-ped-api01"
	// synthetic readers: one finds the name as a folder in the 1st occurrence, the other as a
	// service in the 2nd
	m := newTest(t)
	m.UseReaders([]Reader{
		{Name: "a", Find: func(s string, add func(ObjFinding)) {
			if i := strings.Index(s, name); i >= 0 {
				add(ObjFinding{i, i + len(name), "folder", "a", false})
			}
		}},
		{Name: "b", Find: func(s string, add func(ObjFinding)) {
			if i := strings.LastIndex(s, name); i >= 0 {
				add(ObjFinding{i, i + len(name), "service", "b", true})
			}
		}},
	})
	out, _ := m.Mask("/srv/" + name + "/conf e deployment/" + name)
	if strings.Contains(out, name) {
		t.Fatalf("stayed readable: %s", out)
	}
	if strings.Count(out, "dir_")+strings.Count(out, "svc_") != 2 || strings.Contains(out, "dir_") && strings.Contains(out, "svc_") {
		t.Errorf("the same name with two pseudonyms in the same text: %s", out)
	}

	// real rules: the name learned as a service (command line) and later found as a folder
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m1, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	ps1, tp1 := pseudoOf(m1, "kubectl rollout restart deployment/"+name+" -n xq-ns01", name)
	if tp1 != "obj.service" {
		t.Fatalf("type by the command line: %q", tp1)
	}
	if ps2, tp2 := pseudoOf(m1, "ls /srv/"+name+"/conf", name); ps2 != ps1 || tp2 != tp1 {
		t.Errorf("folder after being learned as a service: %q %q (expected %q %q)", ps2, tp2, ps1, tp1)
	}
	// after the restart (only vistos.json)
	vs.SaveIfDirty()
	vs2, _ := LoadSeen(dir + "/v.json")
	m2, _ := NewMasker(m1.cfg, testKey, nil, vs2)
	if ps3, tp3 := pseudoOf(m2, "ls /srv/"+name+"/conf", name); ps3 != ps1 || tp3 != tp1 {
		t.Errorf("after the restart: %q %q (expected %q %q)", ps3, tp3, ps1, tp1)
	}
	// and in the opposite order, in the same text and with nothing learned: one pseudonym only
	m3, _ := NewMasker(m1.cfg, testKey, nil, nil)
	out, ents := m3.Mask("ls /srv/" + name + "/conf\nkubectl rollout restart deployment/" + name + " -n xq-ns01")
	types := map[string]bool{}
	for _, e := range ents {
		if e.Real == name {
			types[e.Type] = true
		}
	}
	if len(types) != 1 || strings.Contains(out, name) {
		t.Errorf("same text, two types %v: %s", types, out)
	}
}

// A Python string literal prefix is not the field value; a value of 1 or 2 letters is not masked; a
// regex pattern in the value is not data.
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
			t.Errorf("extra mask:\n   %q\n-> %q", s, out)
		}
	}
	// the real value, with or without prefix, stays masked (and the prefix stays)
	value := "Parda"
	for _, s := range []string{"{'RACA': " + "u" + q + value + q + "}", "{'RACA': " + q + value + q + "}", "raca: " + value} {
		out, _ := m.Mask(s)
		if strings.Contains(out, value) {
			t.Errorf("field value stayed readable: %q -> %q", s, out)
		}
		if strings.Contains(s, "u"+q) && !strings.Contains(out, ": u"+q) {
			t.Errorf("the prefix went along: %q -> %q", s, out)
		}
	}
}

// A name learned with a dot is recognized in prose after the restart.
func TestDottedNameAfterRestart(t *testing.T) {
	dir := t.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m, _ := NewMasker(newTest(t).cfg, testKey, nil, vs)
	const name = "xq_topico01.eventos"
	if out, _ := m.Mask("KAFKA_TOPIC=" + name); strings.Contains(out, name) {
		t.Fatalf("did not learn: %s", out)
	}
	prose := "o tópico " + name + " parou de receber; veja " + name + "."
	if out, _ := m.Mask(prose); strings.Contains(out, "xq_topico01") {
		t.Fatalf("before the restart: %s", out)
	}
	vs.SaveIfDirty()
	vs2, _ := LoadSeen(dir + "/v.json")
	m2, _ := NewMasker(m.cfg, testKey, nil, vs2)
	out, _ := m2.Mask(prose)
	if strings.Contains(out, "xq_topico01") || strings.Contains(out, "eventos") {
		t.Errorf("after the restart: %s", out)
	}
	// the piece alone is not the name: "xq_topico01" in another context does not become the
	// pseudonym of the dotted name
	if out, _ := m2.Mask("eventos de hoje"); out != "eventos de hoje" {
		t.Errorf("common word masked: %s", out)
	}
}

// URLs glued without a separator and repeated "a://" are not quadratic.
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

// DatabaseName= inside a JDBC URL in a Java string (the URI counts as the other pair).
func TestJDBCDatabaseNameInString(t *testing.T) {
	m := newTest(t)
	check(t, m, `props.put("url", "jdbc:sqlserver://xq-srv01;databaseName=xq_banco01");`, []string{"xq_banco01", "xq-srv01"}, []string{"props.put", "databaseName="})
	check(t, m, `String url = "jdbc:sqlserver://xq-srv01:1433;databaseName=xq_banco02;encrypt=true";`, []string{"xq_banco02", "xq-srv01"}, []string{"encrypt=true"})
	check(t, m, `url = 'jdbc:postgresql://xq-srv02:5432/xq_banco03?currentSchema=xq_esq01'`, []string{"xq_banco03", "xq_esq01", "xq-srv02"}, nil)
	// without a database URI, a single pair is not a connection string
	check(t, m, `veja o campo databaseName=exemplo no formulário`, nil, []string{"databaseName=exemplo"})
}
