// Package config carrega a configuração do llm-dlp e define onde ficam os arquivos dele.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// PadraoExtra é uma regex própria do usuário, com um rótulo para o pseudônimo.
type PadraoExtra struct {
	Rotulo string `json:"rotulo"`
	Regex  string `json:"regex"`
}

// Termo é uma lista de valores exatos (sem diferenciar maiúsculas) que devem ser mascarados.
type Termo struct {
	Rotulo  string   `json:"rotulo"`
	Valores []string `json:"valores"`
}

type Config struct {
	Porta    int    `json:"porta"`
	Upstream string `json:"upstream"`

	// Hostnames e e-mails que contêm um destes trechos são tratados como internos.
	DominiosInternos []string `json:"dominios_internos"`
	// E-mails e domínios que passam sem máscara.
	EmailsLiberados        []string `json:"emails_liberados"`
	DominiosEmailLiberados []string `json:"dominios_email_liberados"`
	// Primeiro rótulo de hostname que pode ficar visível (ex.: "mysql" em mysql.empresa.intra).
	PapeisHost []string `json:"papeis_host"`

	PadroesExtras []PadraoExtra `json:"padroes_extras"`
	Termos        []Termo       `json:"termos"`

	// Detectores que o usuário quer desligar (ex.: "endereco").
	DetectoresDesligados []string `json:"detectores_desligados"`
	// Detectores opcionais que o usuário quer ligar. Hoje: "quase" (quase identificadores:
	// sexo, idade, estado civil, profissão, nacionalidade, renda, latitude/longitude).
	DetectoresOpcionais []string `json:"detectores_opcionais,omitempty"`

	// Palavras a mais para reconhecer nomes de campo (cabeçalho de CSV, chave de JSON...).
	// Tipos: cpf, cnpj, rg, cnh, pis, doc, nascimento, telefone, cep, endereco, usuario,
	// conta, cartao, sensivel, nome, quase; "pessoa" (de quem é o campo: "segurado") e
	// "neutra" (palavra que não muda o que o campo guarda: "bco", "sis").
	CamposExtras []CampoExtra `json:"campos_extras,omitempty"`

	// CPF e CNPJ só com dígitos (sem pontuação) são mascarados mesmo sem a palavra "cpf"/"cnpj"
	// por perto, desde que o dígito verificador confira. Cerca de 1 em cada 100 números
	// aleatórios desse tamanho também confere: esses são mascarados a mais (na dúvida, mascara).
	DocumentosSemContexto bool `json:"documentos_sem_contexto"`

	// Bloqueia (502) requisições que o llm-dlp não sabe mascarar, em vez de deixá-las passar.
	FalharFechado bool `json:"falhar_fechado"`

	OCR OCR `json:"ocr"`

	// Ferramentas cuja entrada NUNCA é desmascarada: vão para a internet, então devem
	// levar só pseudônimos (evita que uma página maliciosa induza o modelo a mandar o
	// dado real numa URL).
	FerramentasSemDesmascarar []string `json:"ferramentas_sem_desmascarar"`

	// Nomes de recursos internos (servidor, banco, schema, tabela, coluna...), reconhecidos
	// pela estrutura do conteúdo. Ver docs/configuracao.md.
	Objetos Objetos `json:"objetos"`
}

// Objetos liga e desliga, por tipo de entidade, o mascaramento e a propagação. Tipo que não
// estiver nos mapas fica no padrão: mascarar todos; propagar todos menos coluna e índice.
type Objetos struct {
	Ligado   bool            `json:"ligado"`
	Mascarar map[string]bool `json:"mascarar,omitempty"`
	Propagar map[string]bool `json:"propagar,omitempty"`
}

// PrefixoConectores: ferramentas dos conectores do claude.ai (Gmail, Drive...). Rodam fora
// da máquina, nos servidores da Anthropic: a entrada delas também segue com pseudônimos.
// Vale sempre, mesmo que o config.json tenha a própria lista.
const PrefixoConectores = "mcp__claude_ai_"

func (c Config) SemDesmascarar(ferramenta string) bool {
	if strings.HasPrefix(ferramenta, PrefixoConectores) {
		return true
	}
	for _, f := range c.FerramentasSemDesmascarar {
		if f == ferramenta {
			return true
		}
	}
	return false
}

// OCR controla imagens e PDFs enviados à LLM.
type OCR struct {
	// "mascarar" (OCR e cobre o que é sensível), "bloquear" (não deixa sair) ou "permitir".
	Modo       string            `json:"modo"`
	Tesseract  string            `json:"tesseract"`
	PDFToText  string            `json:"pdftotext"`
	PDFToPPM   string            `json:"pdftoppm"`
	Idioma     string            `json:"idioma"`
	Env        map[string]string `json:"env"` // ex.: LD_LIBRARY_PATH, TESSDATA_PREFIX
	MaxPaginas int               `json:"max_paginas"`
	// PDF com texto e sem nada sensível sai original; com true, sai sempre como texto extraído.
	PDFEstrito bool `json:"pdf_estrito"`
}

func Padrao() Config {
	return Config{
		Porta:                  8787,
		Objetos:                Objetos{Ligado: true},
		Upstream:               "https://api.anthropic.com",
		DominiosInternos:       []string{},
		EmailsLiberados:        []string{},
		DominiosEmailLiberados: []string{"example.com", "example.org", "example.net", "users.noreply.github.com", "anthropic.com"},
		PapeisHost: []string{"mysql", "postgres", "redis", "kafka", "zookeeper", "elasticsearch", "opensearch", "neo4j",
			"nginx", "gms", "frontend", "dh-gms", "dh-frontend", "schema-registry", "actions", "mae", "mce", "api", "db", "web"},
		FalharFechado:         true,
		DocumentosSemContexto: true,
		OCR: OCR{Modo: "mascarar", Tesseract: "tesseract", PDFToText: "pdftotext", PDFToPPM: "pdftoppm",
			Idioma: "por", Env: map[string]string{}, MaxPaginas: 30},
		FerramentasSemDesmascarar: []string{"WebFetch", "WebSearch"},
	}
}

// Dir é a pasta do llm-dlp: $LLM_DLP_HOME ou ~/.config/llm-dlp.
func Dir() string {
	if d := os.Getenv("LLM_DLP_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "llm-dlp")
}

func Caminho(nome string) string { return filepath.Join(Dir(), nome) }

// Carregar lê config.json; se não existir, grava o padrão para o usuário editar.
func Carregar() (Config, error) {
	c := Padrao()
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return c, err
	}
	b, err := os.ReadFile(Caminho("config.json"))
	if errors.Is(err, os.ErrNotExist) {
		out, _ := json.MarshalIndent(c, "", "  ")
		return c, os.WriteFile(Caminho("config.json"), append(out, '\n'), 0o600)
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

type CampoExtra struct {
	Tipo     string   `json:"tipo"`
	Palavras []string `json:"palavras"`
}

func (c Config) Opcional(detector string) bool {
	for _, d := range c.DetectoresOpcionais {
		if d == detector {
			return true
		}
	}
	return false
}

func (c Config) Desligado(detector string) bool {
	for _, d := range c.DetectoresDesligados {
		if d == detector {
			return true
		}
	}
	return false
}
