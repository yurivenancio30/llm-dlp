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
	Rotulo string `json:"label"`
	Regex  string `json:"regex"`
}

// Termo é uma lista de valores exatos (sem diferenciar maiúsculas) que devem ser mascarados.
type Termo struct {
	Rotulo  string   `json:"label"`
	Valores []string `json:"values"`
}

// Provedor: outra API de LLM atendida na mesma porta, pelo caminho "/<nome>/..." (o cliente
// aponta o endereço da API para http://127.0.0.1:<porta>/<nome>).
type Provedor struct {
	// Para onde a requisição vai, sem o "/<nome>".
	Upstream string `json:"upstream"`
	// Como ler a requisição e a resposta: "gemini", "codeassist" (o Gemini CLI com a conta
	// Google), "openai" (Chat Completions), "responses" (o Codex) ou "anthropic". Outro valor:
	// modo genérico (todo texto da requisição é mascarado, e a resposta em streaming não é
	// desmascarada). Um caminho terminado em /chat/completions é sempre lido como "openai", e um
	// terminado em /responses, como "responses".
	Formato string `json:"format"`
}

type Config struct {
	Porta    int    `json:"port"`
	Upstream string `json:"upstream"`
	// Outras APIs, pelo nome que vai no caminho. O que não começa com um nome daqui vai para
	// Upstream (a API da Anthropic).
	Provedores map[string]Provedor `json:"providers"`

	// Hostnames e e-mails que contêm um destes trechos são tratados como internos.
	DominiosInternos []string `json:"internal_domains"`
	// E-mails e domínios que passam sem máscara.
	EmailsLiberados        []string `json:"allowed_emails"`
	DominiosEmailLiberados []string `json:"allowed_email_domains"`
	// Primeiro rótulo de hostname que pode ficar visível (ex.: "mysql" em mysql.empresa.intra).
	PapeisHost []string `json:"host_roles"`

	PadroesExtras []PadraoExtra `json:"extra_patterns"`
	Termos        []Termo       `json:"terms"`

	// Detectores que o usuário quer desligar (ex.: "endereco").
	DetectoresDesligados []string `json:"disabled_detectors"`
	// Detectores opcionais que o usuário quer ligar. Hoje: "quase" (quase identificadores:
	// sexo, idade, estado civil, profissão, nacionalidade, renda, latitude/longitude).
	DetectoresOpcionais []string `json:"optional_detectors,omitempty"`

	// Palavras a mais para reconhecer nomes de campo (cabeçalho de CSV, chave de JSON...).
	// Tipos: cpf, cnpj, rg, cnh, pis, doc, nascimento, telefone, cep, endereco, usuario,
	// conta, cartao, sensivel, nome, quase; "pessoa" (de quem é o campo: "segurado") e
	// "neutra" (palavra que não muda o que o campo guarda: "bco", "sis").
	CamposExtras []CampoExtra `json:"extra_fields,omitempty"`

	// CPF e CNPJ só com dígitos (sem pontuação) são mascarados mesmo sem a palavra "cpf"/"cnpj"
	// por perto, desde que o dígito verificador confira. Cerca de 1 em cada 100 números
	// aleatórios desse tamanho também confere: esses são mascarados a mais (na dúvida, mascara).
	DocumentosSemContexto bool `json:"documents_without_context"`

	// Bloqueia (502) requisições que o llm-dlp não sabe mascarar, em vez de deixá-las passar.
	FalharFechado bool `json:"fail_closed"`

	OCR OCR `json:"ocr"`

	// Ferramentas cuja entrada NUNCA é desmascarada: vão para a internet, então devem
	// levar só pseudônimos (evita que uma página maliciosa induza o modelo a mandar o
	// dado real numa URL).
	FerramentasSemDesmascarar []string `json:"tools_never_unmasked"`

	// Nomes de recursos internos (servidor, banco, schema, tabela, coluna...), reconhecidos
	// pela estrutura do conteúdo. Ver docs/pt-BR/configuracao.md.
	Objetos Objetos `json:"objects"`
}

// Objetos liga e desliga, por tipo de entidade, o mascaramento e a propagação. Tipo que não
// estiver nos mapas fica no padrão: mascarar todos; propagar todos menos coluna e índice.
type Objetos struct {
	Ligado   bool            `json:"enabled"`
	Mascarar map[string]bool `json:"mask,omitempty"`
	Propagar map[string]bool `json:"propagate,omitempty"`
	// IPv4 público e IPv6 viram nome de servidor (desligado: só o IP privado é mascarado).
	IPPublico bool `json:"public_ip"`
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
	// "mask" (OCR e cobre o que é sensível), "block" (não deixa sair) ou "allow" (até a 0.8.0:
	// "mascarar", "bloquear", "permitir"; ainda aceitos, ver Ler).
	Modo       string            `json:"mode"`
	Tesseract  string            `json:"tesseract"`
	PDFToText  string            `json:"pdftotext"`
	PDFToPPM   string            `json:"pdftoppm"`
	Idioma     string            `json:"language"`
	Env        map[string]string `json:"env"` // ex.: LD_LIBRARY_PATH, TESSDATA_PREFIX
	MaxPaginas int               `json:"max_pages"`
	// PDF com texto e sem nada sensível sai original; com true, sai sempre como texto extraído.
	PDFEstrito bool `json:"strict_pdf"`
}

// ProvedoresPadrao: as APIs que já vêm configuradas.
func ProvedoresPadrao() map[string]Provedor {
	return map[string]Provedor{
		"gemini": {Upstream: "https://generativelanguage.googleapis.com", Formato: "gemini"},
		"openai": {Upstream: "https://api.openai.com", Formato: "openai"},
		"xai":    {Upstream: "https://api.x.ai", Formato: "openai"},
		// as assinaturas: o Gemini CLI com a conta Google e o Codex com a conta do ChatGPT
		"codeassist": {Upstream: "https://cloudcode-pa.googleapis.com", Formato: "codeassist"},
		"chatgpt":    {Upstream: "https://chatgpt.com/backend-api/codex", Formato: "responses"},
	}
}

func Padrao() Config {
	return Config{
		Porta:                  8787,
		Objetos:                Objetos{Ligado: true},
		Upstream:               "https://api.anthropic.com",
		Provedores:             ProvedoresPadrao(),
		DominiosInternos:       []string{},
		EmailsLiberados:        []string{},
		DominiosEmailLiberados: []string{"example.com", "example.org", "example.net", "users.noreply.github.com", "anthropic.com"},
		PapeisHost: []string{"mysql", "postgres", "redis", "kafka", "zookeeper", "elasticsearch", "opensearch", "neo4j",
			"nginx", "gms", "frontend", "dh-gms", "dh-frontend", "schema-registry", "actions", "mae", "mce", "api", "db", "web"},
		FalharFechado:         true,
		DocumentosSemContexto: true,
		OCR: OCR{Modo: "mask", Tesseract: "tesseract", PDFToText: "pdftotext", PDFToPPM: "pdftoppm",
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
	if err := Ler(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

type CampoExtra struct {
	Tipo     string   `json:"type"`
	Palavras []string `json:"words"`
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
