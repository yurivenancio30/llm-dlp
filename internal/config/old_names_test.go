package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// A 0.8.0 config.json (all fields with the old name and non-default values).
const oldConfig = `{
  "porta": 9999,
  "upstream": "https://api.anthropic.com",
  "provedores": {"gemini": {"upstream": "https://g.example", "formato": "gemini"}},
  "dominios_internos": ["acme"],
  "emails_liberados": ["a@b.c"],
  "dominios_email_liberados": ["example.com"],
  "papeis_host": ["mysql"],
  "padroes_extras": [{"rotulo": "CONTRATO", "regex": "CT-[0-9]+"}],
  "termos": [{"rotulo": "nome", "valores": ["SGP", "Acme"]}],
  "detectores_desligados": ["endereco"],
  "detectores_opcionais": ["quase"],
  "campos_extras": [{"tipo": "cpf", "palavras": ["docto"]}],
  "documentos_sem_contexto": false,
  "falhar_fechado": false,
  "ocr": {"modo": "bloquear", "tesseract": "t", "pdftotext": "p", "pdftoppm": "q", "idioma": "eng",
          "env": {}, "max_paginas": 7, "pdf_estrito": true},
  "ferramentas_sem_desmascarar": ["WebFetch"],
  "objetos": {"ligado": false, "mascarar": {"coluna": false}, "propagar": {"tabela": false}, "ip_publico": true},
  "campo_que_nao_existe": 1
}`

func expectedFromOld() Config {
	c := Default()
	c.Port = 9999
	c.Providers["gemini"] = Provider{Upstream: "https://g.example", Format: "gemini"} // adds to the defaults
	c.InternalDomains, c.EmailsLiberados, c.AllowedEmailDomains = []string{"acme"}, []string{"a@b.c"}, []string{"example.com"}
	c.HostRoles = []string{"mysql"}
	c.ExtraPatterns = []ExtraPattern{{Label: "CONTRATO", Regex: "CT-[0-9]+"}}
	c.Terms = []Term{{Label: "nome", Values: []string{"SGP", "Acme"}}}
	c.DisabledDetectors, c.OptionalDetectors = []string{"endereco"}, []string{"quase"}
	c.ExtraFields = []ExtraField{{Type: "cpf", Words: []string{"docto"}}}
	c.DocumentsWithoutContext, c.FailClosed = false, false
	c.OCR = OCR{Mode: "block", Tesseract: "t", PDFToText: "p", PDFToPPM: "q", Language: "eng", Env: map[string]string{},
		MaxPages: 7, StrictPDF: true}
	c.ToolsWithoutUnmask = []string{"WebFetch"}
	c.Objects = Objects{Enabled: false, Mask: map[string]bool{"coluna": false}, Propagate: map[string]bool{"tabela": false},
		PublicIP: true}
	return c
}

// The old names still apply: the 0.8.0 file is read in full.
func TestConfigWithOldNames(t *testing.T) {
	c := Default()
	if err := Read([]byte(oldConfig), &c); err != nil {
		t.Fatal(err)
	}
	if want := expectedFromOld(); !reflect.DeepEqual(c, want) {
		t.Fatalf("lido:\n%+v\nesperado:\n%+v", c, want)
	}
}

// update: the file moves to the new names (and is read the same) and, when going back to an earlier
// version, back to the old ones, which 0.8.0 read (a reader with only the old names finds
// everything). An unknown field stays.
func TestMigrateFileRoundTrip(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	p := Path("config.json")
	os.WriteFile(p, []byte(oldConfig), 0o600)

	bak, err := MigrateFile(false)
	if err != nil || bak == "" {
		t.Fatalf("ida: bak=%q err=%v", bak, err)
	}
	b, _ := os.ReadFile(p)
	for stale := range oldFields {
		if strings.Contains(string(b), `"`+stale+`"`) {
			t.Errorf("o nome antigo %q ficou no arquivo", stale)
		}
	}
	if !strings.Contains(string(b), `"mode": "block"`) || !strings.Contains(string(b), "campo_que_nao_existe") {
		t.Errorf("ocr.mode ou o campo desconhecido não ficou como devia:\n%s", b)
	}
	c, err := Load()
	if err != nil || !reflect.DeepEqual(c, expectedFromOld()) {
		t.Fatalf("depois da ida, lido diferente (err %v):\n%+v", err, c)
	}
	if bak2, _ := MigrateFile(false); bak2 != "" {
		t.Errorf("segunda ida devia não ter o que trocar")
	}

	if _, err := MigrateFile(true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	var old struct {
		Port    int      `json:"porta"`
		Domains []string `json:"dominios_internos"`
		Terms   []struct {
			Values []string `json:"valores"`
		} `json:"termos"`
		OCR struct {
			Mode string `json:"modo"`
		} `json:"ocr"`
		Objects struct {
			Enabled *bool `json:"ligado"`
		} `json:"objetos"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		t.Fatal(err)
	}
	if old.Port != 9999 || len(old.Domains) != 1 || len(old.Terms) != 1 || len(old.Terms[0].Values) != 2 ||
		old.OCR.Mode != "bloquear" || old.Objects.Enabled == nil || *old.Objects.Enabled {
		t.Errorf("a volta não deixou o arquivo como a 0.8.0 lê:\n%s", b)
	}
}
