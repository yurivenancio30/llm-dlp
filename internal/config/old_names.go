package config

import (
	"encoding/json"
	"os"
	"time"
)

// The config.json fields were in Portuguese up to 0.8.0. The old names are still read (they become
// the new ones on load), and the file is always written with the new ones. "llm-dlp update" moves
// the file to the names of the version it puts online (MigrateFile): the new ones, or the old ones
// when going back to an earlier version, which would ignore the fields with a new name and stop
// masking what they ask for (internal domains, protected names...).

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
	// by list, by the new name of the list
	oldFieldsList = map[string]map[string]string{
		"extra_patterns": {"rotulo": "label"},
		"terms":          {"rotulo": "label", "valores": "values"},
		"extra_fields":   {"tipo": "type", "palavras": "words"},
	}
	oldOCRFields = map[string]string{"modo": "mode", "idioma": "language", "max_paginas": "max_pages",
		"pdf_estrito": "strict_pdf"}
	oldObjectFields = map[string]string{"ligado": "enabled", "mascarar": "mask", "propagar": "propagate",
		"ip_publico": "public_ip"}
	// old values of ocr.mode
	oldOCRModes = map[string]string{"mascarar": "mask", "bloquear": "block", "permitir": "allow"}
)

// Read interprets the content of a config.json (with the new or the old names) over c.
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

// migrateNames swaps, in m, the old names for the new ones (or the new for the old, with toOld) and
// says whether it swapped any. If both are there, the destination's stays.
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
	// what is inside first, found by the new name: going to the old names, the outer name is still
	// the new one; coming from them, it was already swapped
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

// MigrateFile moves config.json to the new names (or to the old ones, with toOld), if there is
// something to swap, keeping the previous one alongside (config.json.bak-DATE). Returns the path of
// the copy ("" if there was nothing to swap). Fields this program does not know stay as they are.
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
