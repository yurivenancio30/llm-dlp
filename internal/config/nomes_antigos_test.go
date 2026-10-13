package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Um config.json da 0.8.0 (todos os campos com o nome antigo e valores fora do padrão).
const configAntigo = `{
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

func esperadoDoAntigo() Config {
	c := Padrao()
	c.Porta = 9999
	c.Provedores["gemini"] = Provedor{Upstream: "https://g.example", Formato: "gemini"} // soma aos padrões
	c.DominiosInternos, c.EmailsLiberados, c.DominiosEmailLiberados = []string{"acme"}, []string{"a@b.c"}, []string{"example.com"}
	c.PapeisHost = []string{"mysql"}
	c.PadroesExtras = []PadraoExtra{{Rotulo: "CONTRATO", Regex: "CT-[0-9]+"}}
	c.Termos = []Termo{{Rotulo: "nome", Valores: []string{"SGP", "Acme"}}}
	c.DetectoresDesligados, c.DetectoresOpcionais = []string{"endereco"}, []string{"quase"}
	c.CamposExtras = []CampoExtra{{Tipo: "cpf", Palavras: []string{"docto"}}}
	c.DocumentosSemContexto, c.FalharFechado = false, false
	c.OCR = OCR{Modo: "block", Tesseract: "t", PDFToText: "p", PDFToPPM: "q", Idioma: "eng", Env: map[string]string{},
		MaxPaginas: 7, PDFEstrito: true}
	c.FerramentasSemDesmascarar = []string{"WebFetch"}
	c.Objetos = Objetos{Ligado: false, Mascarar: map[string]bool{"coluna": false}, Propagar: map[string]bool{"tabela": false},
		IPPublico: true}
	return c
}

// Os nomes antigos continuam valendo: o arquivo da 0.8.0 é lido inteiro.
func TestConfigComNomesAntigos(t *testing.T) {
	c := Padrao()
	if err := Ler([]byte(configAntigo), &c); err != nil {
		t.Fatal(err)
	}
	if want := esperadoDoAntigo(); !reflect.DeepEqual(c, want) {
		t.Fatalf("lido:\n%+v\nesperado:\n%+v", c, want)
	}
}

// update: o arquivo passa para os nomes novos (e é lido igual) e, ao voltar para uma versão
// anterior, de volta para os antigos, que a 0.8.0 lia (um leitor só com os nomes antigos acha
// tudo). Campo desconhecido fica.
func TestMigrarArquivoIdaEVolta(t *testing.T) {
	t.Setenv("LLM_DLP_HOME", t.TempDir())
	p := Caminho("config.json")
	os.WriteFile(p, []byte(configAntigo), 0o600)

	bak, err := MigrarArquivo(false)
	if err != nil || bak == "" {
		t.Fatalf("ida: bak=%q err=%v", bak, err)
	}
	b, _ := os.ReadFile(p)
	for velho := range camposAntigos {
		if strings.Contains(string(b), `"`+velho+`"`) {
			t.Errorf("o nome antigo %q ficou no arquivo", velho)
		}
	}
	if !strings.Contains(string(b), `"mode": "block"`) || !strings.Contains(string(b), "campo_que_nao_existe") {
		t.Errorf("ocr.mode ou o campo desconhecido não ficou como devia:\n%s", b)
	}
	c, err := Carregar()
	if err != nil || !reflect.DeepEqual(c, esperadoDoAntigo()) {
		t.Fatalf("depois da ida, lido diferente (err %v):\n%+v", err, c)
	}
	if bak2, _ := MigrarArquivo(false); bak2 != "" {
		t.Errorf("segunda ida devia não ter o que trocar")
	}

	if _, err := MigrarArquivo(true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	var antigo struct {
		Porta    int                          `json:"porta"`
		Dominios []string                     `json:"dominios_internos"`
		Termos   []struct{ Valores []string } `json:"termos"`
		OCR      struct {
			Modo string `json:"modo"`
		} `json:"ocr"`
		Objetos struct {
			Ligado *bool `json:"ligado"`
		} `json:"objetos"`
	}
	if err := json.Unmarshal(b, &antigo); err != nil {
		t.Fatal(err)
	}
	if antigo.Porta != 9999 || len(antigo.Dominios) != 1 || len(antigo.Termos) != 1 || len(antigo.Termos[0].Valores) != 2 ||
		antigo.OCR.Modo != "bloquear" || antigo.Objetos.Ligado == nil || *antigo.Objetos.Ligado {
		t.Errorf("a volta não deixou o arquivo como a 0.8.0 lê:\n%s", b)
	}
}
