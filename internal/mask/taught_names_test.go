package mask

import (
	"regexp"
	"strings"
	"testing"
)

// Names a structure teaches for the rest of the text: what a masked e-mail says (the organization's
// domain and the person's name), the user and the host of a terminal prompt, the project folder right
// under the home. And two names that are NOT the customer's: a public software with proof, which the
// accumulated memory no longer applies, and a Python built-in exception in a customer frame.

func TestEmailTeachesDomainAndName(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"domain in a config", "aprovadores = [\"bruno.tavares@acmehx.io\"]\ntenant_id = \"acmehx.io\"\nauthority = f\"https://login.microsoftonline.com/acmehx.io\"\nUPN: BRUNO.TAVARES@ACMEHX.IO, dominio ACMEHX.IO\n",
			[]string{"acmehx.io", "ACMEHX.IO"}, []string{"tenant_id", "login.microsoftonline.com"}},
		{"name in a git log", "ac7d828 2026-06-21 Bruno Tavares fix: telas\nautor: bruno.tavares@acmehx.io\nrevisor: BRUNO TAVARES\n",
			[]string{"Bruno Tavares", "BRUNO TAVARES"}, []string{"fix", "telas"}},
		// a personal mail domain is public; the person still is the person
		{"personal mail", "contato bruno.tavares@gmail.com; o gmail.com bloqueou o envio; Bruno Tavares respondeu\n",
			[]string{"Bruno Tavares"}, []string{"gmail.com"}},
		// escaped code: "\n@pytest.fixture" and "x@main.py" look like an e-mail, but the domain is
		// not under a real TLD (or the TLD is a file extension)
		{"code that looks like an e-mail", "{\"code\": \"import pytest\\n@pytest.fixture\\ndef db():\\n    return pytest.fixture, 'x@main.py', main.py\"}",
			nil, []string{"pytest.fixture", "main.py"}},
		// a role mailbox is not a person; a longer domain that ends differently is another domain
		{"role mailbox", "suporte: support.team@acmehx.io; Support Team responde; site acmehx.io.br\n",
			nil, []string{"Support Team", "acmehx.io.br"}},
		{"area mailbox", "relatórios: bi.vendashx@acmehx.io; o workspace BI Vendashx foi criado\n",
			nil, []string{"BI Vendashx"}},
	})

	// the name and the e-mail of the person get the same identifier, and the domain the same one
	// it has after the "@"
	m := newTest(t)
	s := "Bruno Tavares aprovou (bruno.tavares@acmehx.io); tenant acmehx.io"
	out, ents := m.Mask(s)
	person := regexp.MustCompile(`Person ([a-z2-7]{8})`).FindStringSubmatch(out)
	mail := regexp.MustCompile(`p\.([a-z2-7]{8})@(d[a-z2-7]{4}\.invalid)`).FindStringSubmatch(out)
	if person == nil || mail == nil || person[1] != mail[1] {
		t.Errorf("name and e-mail should have the same identifier: %q", out)
	}
	if mail != nil && !strings.Contains(out, "tenant "+mail[2]) {
		t.Errorf("the domain alone should have the pseudonym of the e-mail's domain: %q", out)
	}
	if back := NewTable(ents).Unmask(out, false); back != s {
		t.Errorf("way back:\n   %q\n   %q", s, back)
	}
	// in a later text, the name is found in any case (people registry)
	if out, _ := m.Mask("a bruno tavares saiu de férias"); strings.Contains(out, "bruno tavares") {
		t.Errorf("name learned from the e-mail, in lowercase: %q", out)
	}
}

func TestPromptAndHomeProject(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"bash prompt", "yvxhx@vdi-prd-hx01:~/MB.Vendas.Portal$ git status\nclone ao lado do `MB.Vendas.Portal`\n",
			[]string{"yvxhx", "vdi-prd-hx01", "MB.Vendas.Portal"}, []string{"git", "status", "clone"}},
		{"venv prompt", "(venv) yvxhx@vdi-prd-hx01:/opt/app# ls\n", []string{"yvxhx", "vdi-prd-hx01"}, []string{"venv", "ls"}},
		// a public user, and the EC2 host name left to the IP detector (it keeps the shape)
		{"public user", "root@ip-10-1-2-3:~# ls\nubuntu@ip-10-1-2-3:/tmp$ ls\n", []string{"10-1-2-3"}, []string{"root", "ubuntu"}},
		// the project folder right under the home is learned; one made of words and the ones below
		// it are masked only in the path
		{"home project", "cd ~/acme-etl-hx && ls ~/my-scripts/tools_hx/\no acme-etl-hx usa my-scripts e tools_hx\n",
			[]string{"acme-etl-hx"}, []string{"my-scripts", "o", "usa"}},
		// a name cut at an accented letter is a piece, not the project: it is not learned
		{"accented home project", "cd ~/acmehx-gestão/src\nveja acmehx-gest depois\n", nil, []string{"acmehx-gest"}},
		{"not at the start of the line", "fale com yvxhx@vdi-prd-hx01:~/x$ depois\n", nil, []string{"yvxhx"}},
	})
	if e := entOf(t, "yvxhx@vdi-prd-hx01:~/x$ ls", "yvxhx"); e != "user" {
		t.Errorf("prompt user masked as %q, want user", e)
	}
	if e := entOf(t, "cd ~/MB.Vendas.Portal\nMB.Vendas.Portal", "MB.Vendas.Portal"); e != "folder" {
		t.Errorf("home project masked as %q, want folder", e)
	}
}

func TestProvenSoftwareIsNotReapplied(t *testing.T) {
	m := newTest(t)
	m.known.learnIt(objTypePrefix+"repository", "DataHub") // learned before (RAM or seen.json)
	if out, _ := m.Mask("o DataHub ingere metadados"); strings.Contains(out, "DataHub") {
		t.Fatalf("premise: without the proof, the learned name is masked: %q", out)
	}
	m.Mask("pip install 'acryl-datahub[mssql]'\nhelm install datahub datahub/datahub")
	if out, _ := m.Mask("o DataHub ingere metadados de novo"); !strings.Contains(out, "DataHub") {
		t.Errorf("public software with proof should not be reapplied by the memory: %q", out)
	}
	// where a reader decides it, it stays masked (the database of the DSN)
	if out, _ := m.Mask("mysql://datahub:" + weakOnes[0] + "@mysqlhx01:3306/datahub_hx_prod"); strings.Contains(out, "datahub_hx_prod") {
		t.Errorf("the reader's own decision should stay: %q", out)
	}
}

func TestPythonExceptionInCustomerFrame(t *testing.T) {
	tb := "Traceback (most recent call last):\n  File \"/opt/app/carga_sinistros_hx.py\", line 10, in processar_lote_hx\n" +
		"    raise RuntimeError(f\"lote {lote_id_hx} falhou\")\nRuntimeError: lote 7 falhou\n"
	checkShapes(t, []shapeCase{
		{"built-in exception", tb, []string{"processar_lote_hx", "lote_id_hx", "carga_sinistros_hx"}, []string{"RuntimeError"}},
		{"customer exception", strings.Replace(tb, "RuntimeError", "LoteInvalidoHxError", 1), []string{"LoteInvalidoHxError"}, nil},
	})
}

// After a restart, a text that already went out is not detected again: what it taught (the e-mail's
// domain, the software proof) is recorded when it is read back from sent.log.
func TestTaughtAfterRestart(t *testing.T) {
	dir := t.TempDir()
	m, vs := diskMasker(t, dir)
	old := "aprovador: bruno.tavares@acmehx.io\nhelm install datahub datahub/datahub"
	l := m.NewBatch()
	l.Mask(old, false, posA)
	l.Freeze()
	m.Persist()
	vs.SaveIfDirty()

	m2, _ := diskMasker(t, dir)
	m2.known.learnIt(objTypePrefix+"repository", "DataHub")
	m2.NewBatch().Mask(old, false, posA) // read back from sent.log
	if out, _ := m2.Mask("tenant acmehx.io"); strings.Contains(out, "acmehx.io") {
		t.Errorf("after the restart, the domain of the e-mail already sent was not learned: %q", out)
	}
	if out, _ := m2.Mask("o DataHub ingere"); !strings.Contains(out, "DataHub") {
		t.Errorf("after the restart, the proof of the text already sent was not recorded: %q", out)
	}
}
