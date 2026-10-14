package config

import (
	"encoding/json"
	"os"
	"time"
)

// Os campos do config.json eram em português até a 0.8.0. Os nomes antigos continuam sendo
// lidos (viram os novos ao carregar), e o arquivo é sempre gravado com os novos. O "llm-dlp
// update" passa o arquivo para os nomes da versão que põe no ar (MigrateFile): os novos, ou
// os antigos ao voltar para uma versão anterior, que ignoraria os campos com nome novo e
// deixaria de mascarar o que eles pedem (domínios internos, nomes protegidos...).

var (
	oldFields = map[string]string{
		"porta": "port", "provedores": "providers", "dominios_internos": "internal_domains",
		"emails_liberados": "allowed_emails", "dominios_email_liberados": "allowed_email_domains",
		"papeis_host": "host_roles", "padroes_extras": "extra_patterns", "termos": "terms",
		"detectores_desligados": "disabled_detectors", "detectores_opcionais": "optional_detectors",
		"campos_extras": "extra_fields", "documentos_sem_contexto": "documents_without_context",
		"falhar_fechado": "fail_closed", "ferramentas_sem_desmascarar": "tools_never_unmasked",
		"objetos": "objects",
	}
	oldProviderFields = map[string]string{"formato": "format"}
	// por lista, pelo nome novo da lista
	oldFieldsList = map[string]map[string]string{
		"extra_patterns": {"rotulo": "label"},
		"terms":          {"rotulo": "label", "valores": "values"},
		"extra_fields":   {"tipo": "type", "palavras": "words"},
	}
	oldOCRFields = map[string]string{"modo": "mode", "idioma": "language", "max_paginas": "max_pages",
		"pdf_estrito": "strict_pdf"}
	oldObjectFields = map[string]string{"ligado": "enabled", "mascarar": "mask", "propagar": "propagate",
		"ip_publico": "public_ip"}
	// valores antigos de ocr.mode
	oldOCRModes = map[string]string{"mascarar": "mask", "bloquear": "block", "permitir": "allow"}
)

// Read interpreta o conteúdo de um config.json (com os nomes novos ou os antigos) sobre c.
func Read(b []byte, c *Config) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if migrateNames(m, false) {
		var err error
		if b, err = json.Marshal(m); err != nil {
			return err
		}
	}
	return json.Unmarshal(b, c)
}

// migrateNames troca, em m, os nomes antigos pelos novos (ou os novos pelos antigos, com
// toOld) e diz se trocou algum. Se os dois estão lá, fica o do destino.
func migrateNames(m map[string]any, toOld bool) bool {
	names := func(n map[string]string) map[string]string {
		if !toOld {
			return n
		}
		inv := make(map[string]string, len(n))
		for stale, newOne := range n {
			inv[newOne] = stale
		}
		return inv
	}
	// o que está dentro primeiro, achado pelo nome novo: indo para os antigos, o nome de
	// fora ainda é o novo; vindo deles, já foi trocado
	foundIt := false
	if !toOld {
		foundIt = rename(m, names(oldFields))
	}
	if ps, ok := m["providers"].(map[string]any); ok {
		for _, p := range ps {
			if pm, ok := p.(map[string]any); ok {
				foundIt = rename(pm, names(oldProviderFields)) || foundIt
			}
		}
	}
	for field, n := range oldFieldsList {
		if l, ok := m[field].([]any); ok {
			for _, x := range l {
				if xm, ok := x.(map[string]any); ok {
					foundIt = rename(xm, names(n)) || foundIt
				}
			}
		}
	}
	if o, ok := m["ocr"].(map[string]any); ok {
		foundIt = rename(o, names(oldOCRFields)) || foundIt
		mode := "mode"
		if toOld {
			mode = "modo"
		}
		if s, ok := o[mode].(string); ok {
			if other, ok := names(oldOCRModes)[s]; ok {
				o[mode], foundIt = other, true
			}
		}
	}
	if o, ok := m["objects"].(map[string]any); ok {
		foundIt = rename(o, names(oldObjectFields)) || foundIt
	}
	if toOld {
		foundIt = rename(m, names(oldFields)) || foundIt
	}
	return foundIt
}

func rename(m map[string]any, names map[string]string) bool {
	foundIt := false
	for de, to := range names {
		if v, ok := m[de]; ok {
			if _, has := m[to]; !has {
				m[to] = v
			}
			delete(m, de)
			foundIt = true
		}
	}
	return foundIt
}

// MigrateFile passa o config.json para os nomes novos (ou para os antigos, com
// toOld), se houver o que trocar, guardando o anterior ao lado (config.json.bak-DATA).
// Devolve o caminho da cópia ("" se não havia o que trocar). Campos que este programa não
// conhece ficam como estão.
func MigrateFile(toOld bool) (string, error) {
	p := Path("config.json")
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	if !migrateNames(m, toOld) {
		return "", nil
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	bak := p + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(bak, b, 0o600); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return "", err
	}
	return bak, os.Rename(tmp, p)
}
