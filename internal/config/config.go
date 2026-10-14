// Package config loads the llm-dlp configuration and defines where its files live.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ExtraPattern is a user's own regex, with a label for the pseudonym.
type ExtraPattern struct {
	Label string `json:"label"`
	Regex string `json:"regex"`
}

// Term is a list of exact values (case-insensitive) that must be masked.
type Term struct {
	Label  string   `json:"label"`
	Values []string `json:"values"`
}

// Provider: another LLM API served on the same port, under the path "/<name>/..." (the client
// points the API address to http://127.0.0.1:<port>/<name>).
type Provider struct {
	// Where the request goes, without the "/<name>".
	Upstream string `json:"upstream"`
	// How to read the request and the response: "gemini", "codeassist" (Gemini CLI with the Google
	// account), "openai" (Chat Completions), "responses" (Codex) or "anthropic". Any other value:
	// generic mode (all request text is masked, and the streaming response is not unmasked). A path
	// ending in /chat/completions is always read as "openai", and one ending in /responses, as
	// "responses".
	Format string `json:"format"`
}

type Config struct {
	Port     int    `json:"port"`
	Upstream string `json:"upstream"`
	// Other APIs, by the name that goes in the path. What does not start with a name from here goes
	// to Upstream (the Anthropic API).
	Providers map[string]Provider `json:"providers"`

	// Hostnames and e-mails that contain one of these pieces are treated as internal.
	InternalDomains []string `json:"internal_domains"`
	// E-mails and domains that pass without masking.
	AllowedEmails       []string `json:"allowed_emails"`
	AllowedEmailDomains []string `json:"allowed_email_domains"`
	// First hostname label that may stay visible (e.g. "mysql" in mysql.company.intra).
	HostRoles []string `json:"host_roles"`

	ExtraPatterns []ExtraPattern `json:"extra_patterns"`
	Terms         []Term         `json:"terms"`

	// Detectors the user wants to turn off (e.g. "address").
	DisabledDetectors []string `json:"disabled_detectors"`
	// Optional detectors the user wants to turn on. Today: "quasi" (quasi-identifiers: sex, age,
	// marital status, profession, nationality, income, latitude/longitude).
	OptionalDetectors []string `json:"optional_detectors,omitempty"`

	// Extra words to recognize field names (CSV header, JSON key...). Types: cpf, cnpj, rg, cnh,
	// pis, doc, birthdate, phone, cep, address, user, account, card, sensitive, name, quasi;
	// "person" (whose field it is: "segurado") and "neutral" (a word that does not change what the
	// field holds: "bco", "sis").
	ExtraFields []ExtraField `json:"extra_fields,omitempty"`

	// CPF and CNPJ with digits only (no punctuation) are masked even without the word "cpf"/"cnpj"
	// nearby, as long as the check digit matches. About 1 in 100 random numbers of that length also
	// matches: those are masked in excess (when in doubt, mask).
	DocumentsWithoutContext bool `json:"documents_without_context"`

	// Blocks (502) requests that llm-dlp cannot mask, instead of letting them through.
	FailClosed bool `json:"fail_closed"`

	OCR OCR `json:"ocr"`

	// Tools whose input is NEVER unmasked: they go to the internet, so they must carry only
	// pseudonyms (this keeps a malicious page from inducing the model to send the real data in a
	// URL).
	ToolsWithoutUnmask []string `json:"tools_never_unmasked"`

	// Names of internal resources (server, database, schema, table, column...), recognized by the
	// structure of the content. See docs/configuration.md.
	Objects Objects `json:"objects"`
}

// Objects turns masking and propagation on and off, per entity type. A type that is not in the maps
// stays at the default: mask all; propagate all except column and index.
type Objects struct {
	Enabled   bool            `json:"enabled"`
	Mask      map[string]bool `json:"mask,omitempty"`
	Propagate map[string]bool `json:"propagate,omitempty"`
	// Public IPv4 and IPv6 become a server name (off: only the private IP is masked).
	PublicIP bool `json:"public_ip"`
}

// ConnectorPrefix: tools of the claude.ai connectors (Gmail, Drive...). They run outside the
// machine, on Anthropic's servers: their input also goes with pseudonyms. Always applies, even if
// config.json has its own list.
const ConnectorPrefix = "mcp__claude_ai_"

func (c Config) NoUnmask(tool string) bool {
	if strings.HasPrefix(tool, ConnectorPrefix) {
		return true
	}
	for _, f := range c.ToolsWithoutUnmask {
		if f == tool {
			return true
		}
	}
	return false
}

// OCR controls images and PDFs sent to the LLM.
type OCR struct {
	// "mask" (OCR, and covers what is sensitive), "block" (does not let it out) or "allow" (up to
	// 0.8.0: "mascarar", "bloquear", "permitir"; still accepted, see Read).
	Mode      string            `json:"mode"`
	Tesseract string            `json:"tesseract"`
	PDFToText string            `json:"pdftotext"`
	PDFToPPM  string            `json:"pdftoppm"`
	Language  string            `json:"language"`
	Env       map[string]string `json:"env"` // ex.: LD_LIBRARY_PATH, TESSDATA_PREFIX
	MaxPages  int               `json:"max_pages"`
	// A PDF with text and nothing sensitive goes out as the original; with true, it always goes out
	// as extracted text.
	StrictPDF bool `json:"strict_pdf"`
}

// DefaultProviders: the APIs that come configured.
func DefaultProviders() map[string]Provider {
	return map[string]Provider{
		"gemini": {Upstream: "https://generativelanguage.googleapis.com", Format: "gemini"},
		"openai": {Upstream: "https://api.openai.com", Format: "openai"},
		"xai":    {Upstream: "https://api.x.ai", Format: "openai"},
		// the subscriptions: Gemini CLI with the Google account and Codex with the ChatGPT account
		"codeassist": {Upstream: "https://cloudcode-pa.googleapis.com", Format: "codeassist"},
		"chatgpt":    {Upstream: "https://chatgpt.com/backend-api/codex", Format: "responses"},
	}
}

func Default() Config {
	return Config{
		Port:                8787,
		Objects:             Objects{Enabled: true},
		Upstream:            "https://api.anthropic.com",
		Providers:           DefaultProviders(),
		InternalDomains:     []string{},
		AllowedEmails:       []string{},
		AllowedEmailDomains: []string{"example.com", "example.org", "example.net", "users.noreply.github.com", "anthropic.com"},
		HostRoles: []string{"mysql", "postgres", "redis", "kafka", "zookeeper", "elasticsearch", "opensearch", "neo4j",
			"nginx", "gms", "frontend", "dh-gms", "dh-frontend", "schema-registry", "actions", "mae", "mce", "api", "db", "web"},
		FailClosed:              true,
		DocumentsWithoutContext: true,
		OCR: OCR{Mode: "mask", Tesseract: "tesseract", PDFToText: "pdftotext", PDFToPPM: "pdftoppm",
			Language: "por", Env: map[string]string{}, MaxPages: 30},
		ToolsWithoutUnmask: []string{"WebFetch", "WebSearch"},
	}
}

// Dir is the llm-dlp folder: $LLM_DLP_HOME or ~/.config/llm-dlp.
func Dir() string {
	if d := os.Getenv("LLM_DLP_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "llm-dlp")
}

func Path(name string) string { return filepath.Join(Dir(), name) }

// Load reads config.json; if it does not exist, writes the default for the user to edit.
func Load() (Config, error) {
	c := Default()
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return c, err
	}
	b, err := os.ReadFile(Path("config.json"))
	if errors.Is(err, os.ErrNotExist) {
		out, _ := json.MarshalIndent(c, "", "  ")
		return c, os.WriteFile(Path("config.json"), append(out, '\n'), 0o600)
	}
	if err != nil {
		return c, err
	}
	if err := Read(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

type ExtraField struct {
	Type  string   `json:"type"`
	Words []string `json:"words"`
}

func (c Config) Optional(detector string) bool {
	for _, d := range c.OptionalDetectors {
		if d == detector {
			return true
		}
	}
	return false
}

func (c Config) Disabled(detector string) bool {
	for _, d := range c.DisabledDetectors {
		if d == detector {
			return true
		}
	}
	return false
}
