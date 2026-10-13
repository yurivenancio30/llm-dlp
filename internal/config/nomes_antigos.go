package config

import (
	"encoding/json"
	"os"
	"time"
)

// Os campos do config.json eram em português até a 0.8.0. Os nomes antigos continuam sendo
// lidos (viram os novos ao carregar), e o arquivo é sempre gravado com os novos. O "llm-dlp
// update" passa o arquivo para os nomes da versão que põe no ar (MigrarArquivo): os novos, ou
// os antigos ao voltar para uma versão anterior, que ignoraria os campos com nome novo e
// deixaria de mascarar o que eles pedem (domínios internos, nomes protegidos...).

var (
	camposAntigos = map[string]string{
		"porta": "port", "provedores": "providers", "dominios_internos": "internal_domains",
		"emails_liberados": "allowed_emails", "dominios_email_liberados": "allowed_email_domains",
		"papeis_host": "host_roles", "padroes_extras": "extra_patterns", "termos": "terms",
		"detectores_desligados": "disabled_detectors", "detectores_opcionais": "optional_detectors",
		"campos_extras": "extra_fields", "documentos_sem_contexto": "documents_without_context",
		"falhar_fechado": "fail_closed", "ferramentas_sem_desmascarar": "tools_never_unmasked",
		"objetos": "objects",
	}
	camposAntigosProvedor = map[string]string{"formato": "format"}
	// por lista, pelo nome novo da lista
	camposAntigosLista = map[string]map[string]string{
		"extra_patterns": {"rotulo": "label"},
		"terms":          {"rotulo": "label", "valores": "values"},
		"extra_fields":   {"tipo": "type", "palavras": "words"},
	}
	camposAntigosOCR = map[string]string{"modo": "mode", "idioma": "language", "max_paginas": "max_pages",
		"pdf_estrito": "strict_pdf"}
	camposAntigosObjetos = map[string]string{"ligado": "enabled", "mascarar": "mask", "propagar": "propagate",
		"ip_publico": "public_ip"}
	// valores antigos de ocr.mode
	modosOCRAntigos = map[string]string{"mascarar": "mask", "bloquear": "block", "permitir": "allow"}
)

// Ler interpreta o conteúdo de um config.json (com os nomes novos ou os antigos) sobre c.
func Ler(b []byte, c *Config) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if migrarNomes(m, false) {
		var err error
		if b, err = json.Marshal(m); err != nil {
			return err
		}
	}
	return json.Unmarshal(b, c)
}

// migrarNomes troca, em m, os nomes antigos pelos novos (ou os novos pelos antigos, com
// paraAntigos) e diz se trocou algum. Se os dois estão lá, fica o do destino.
func migrarNomes(m map[string]any, paraAntigos bool) bool {
	nomes := func(n map[string]string) map[string]string {
		if !paraAntigos {
			return n
		}
		inv := make(map[string]string, len(n))
		for velho, novo := range n {
			inv[novo] = velho
		}
		return inv
	}
	// o que está dentro primeiro, achado pelo nome novo: indo para os antigos, o nome de
	// fora ainda é o novo; vindo deles, já foi trocado
	achou := false
	if !paraAntigos {
		achou = renomear(m, nomes(camposAntigos))
	}
	if ps, ok := m["providers"].(map[string]any); ok {
		for _, p := range ps {
			if pm, ok := p.(map[string]any); ok {
				achou = renomear(pm, nomes(camposAntigosProvedor)) || achou
			}
		}
	}
	for campo, n := range camposAntigosLista {
		if l, ok := m[campo].([]any); ok {
			for _, x := range l {
				if xm, ok := x.(map[string]any); ok {
					achou = renomear(xm, nomes(n)) || achou
				}
			}
		}
	}
	if o, ok := m["ocr"].(map[string]any); ok {
		achou = renomear(o, nomes(camposAntigosOCR)) || achou
		modo := "mode"
		if paraAntigos {
			modo = "modo"
		}
		if s, ok := o[modo].(string); ok {
			if outro, ok := nomes(modosOCRAntigos)[s]; ok {
				o[modo], achou = outro, true
			}
		}
	}
	if o, ok := m["objects"].(map[string]any); ok {
		achou = renomear(o, nomes(camposAntigosObjetos)) || achou
	}
	if paraAntigos {
		achou = renomear(m, nomes(camposAntigos)) || achou
	}
	return achou
}

func renomear(m map[string]any, nomes map[string]string) bool {
	achou := false
	for de, para := range nomes {
		if v, ok := m[de]; ok {
			if _, tem := m[para]; !tem {
				m[para] = v
			}
			delete(m, de)
			achou = true
		}
	}
	return achou
}

// MigrarArquivo passa o config.json para os nomes novos (ou para os antigos, com
// paraAntigos), se houver o que trocar, guardando o anterior ao lado (config.json.bak-DATA).
// Devolve o caminho da cópia ("" se não havia o que trocar). Campos que este programa não
// conhece ficam como estão.
func MigrarArquivo(paraAntigos bool) (string, error) {
	p := Caminho("config.json")
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
	if !migrarNomes(m, paraAntigos) {
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
