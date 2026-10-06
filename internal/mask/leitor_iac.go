package mask

import (
	"regexp"
	"strings"
)

// Infraestrutura como código: Terraform/HCL, Bicep, ARM e CloudFormation (ver docs/estruturas.md).

// entAzure: entidade pelo tipo do recurso (Microsoft.Sql/servers/databases -> último pedaço).
func entAzure(seg string) string {
	switch strings.ToLower(seg) {
	case "servers", "flexibleservers", "managedclusters", "clusters", "virtualmachines", "registries", "managedinstances":
		return "servidor"
	case "databases", "databaseaccounts":
		return "database"
	case "storageaccounts", "containers":
		return "bucket"
	case "queues", "topics", "subscriptions", "eventhubs":
		return "fila"
	case "namespaces":
		return "namespace"
	}
	return "servico"
}

// addNomeAzure: o nome de um recurso ARM/Bicep; filho vem como "pai/filho", um pedaço por tipo.
func addNomeAzure(s string, a, b int, tipo, regra string, add func(ObjAchado)) {
	segT := strings.Split(tipo, "/")[1:]
	ps := strings.Split(s[a:b], "/")
	for k, p := range ps {
		ent := "servico"
		if t := len(segT) - len(ps) + k; t >= 0 && t < len(segT) {
			ent = entAzure(segT[t])
		}
		addNome(s, a, a+len(p), ent, regra, true, add)
		a += len(p) + 1
	}
}

var (
	reCabHCL   = regexp.MustCompile(`^[ \t]*(?:[+~-]|-/\+|\+/-|<=)?[ \t]*(resource|data)[ \t]+"([^"\s]+)"[ \t]+"[^"\s]+"[ \t]*\{`)
	reCabHCL1  = regexp.MustCompile(`^[ \t]*(?:provider|backend)[ \t]+"[^"\s]+"[ \t]*\{`)
	reAtribHCL = regexp.MustCompile(`^[ \t]*(?:[+~-]|-/\+|\+/-)?[ \t]*"?([A-Za-z_][\w-]*)"?[ \t]*=[ \t]*`)
	reTagNome  = regexp.MustCompile(`"?\bName"?[ \t]*[=:][ \t]*"([^"\n]*)"`)
)

// núcleo do nome do atributo -> entidade
var nucleoHCL = map[string]string{"bucket": "bucket", "database": "database", "db": "database", "server": "servidor",
	"instance": "servidor", "cluster": "servidor", "host": "servidor", "hostname": "servidor", "account": "conta_nuvem",
	"project": "conta_nuvem", "namespace": "namespace", "topic": "fila", "queue": "fila", "subscription": "fila",
	"repository": "repositorio", "repo": "repositorio", "dataset": "schema", "schema": "schema", "table": "tabela",
	"username": "usuario", "user": "usuario", "organization": "organizacao", "org": "organizacao", "folder": "pasta",
	"function": "servico"}

// entTipoHCL: a entidade do atributo name/identifier pelo tipo do recurso (aws_s3_bucket...).
func entTipoHCL(t string) string {
	t = strings.ToLower(t)
	for _, p := range [][2]string{{"storage_account", "bucket"}, {"bucket", "bucket"}, {"service_account", "usuario"},
		{"sql_database", "database"}, {"database", "database"}, {"db_instance", "database"}, {"rds_cluster", "database"},
		{"dataset", "schema"}, {"table", "tabela"}, {"namespace", "namespace"}, {"topic", "fila"}, {"queue", "fila"},
		{"subscription", "fila"}, {"repository", "repositorio"}, {"project", "conta_nuvem"}, {"folder", "pasta"},
		{"organization", "organizacao"}, {"user", "usuario"}, {"role", "usuario"}, {"cluster", "servidor"},
		{"server", "servidor"}, {"instance", "servidor"}, {"host", "servidor"}} {
		if strings.Contains(t, p[0]) {
			return p[1]
		}
	}
	return "servico"
}

// entAtribHCL: (entidade, forte, ok) do atributo attr no recurso de tipo t, na profundidade prof.
func entAtribHCL(attr, t string, prof int, valor string) (string, bool, bool) {
	ps := strings.Split(strings.ToLower(attr), "_")
	ult := ps[len(ps)-1]
	nucleo := ult
	if ult == "name" || ult == "identifier" || ult == "id" || ult == "ids" {
		if len(ps) == 1 {
			if ult == "id" || ult == "ids" || t == "" || prof > 1 {
				return "", false, false
			}
			return entTipoHCL(t), prof == 1, true
		}
		nucleo = ps[len(ps)-2]
	}
	ent, ok := nucleoHCL[nucleo]
	if !ok {
		return "", false, false
	}
	if nucleo == "account" && !(todoDigitos(valor) && len(valor) == 12) {
		if strings.Contains(strings.ToLower(attr), "storage") {
			ent = "bucket"
		} else if todoDigitos(valor) {
			return "", false, false
		}
	}
	return ent, prof == 1, true
}

func acharTerraform(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '"') < 0 || strings.IndexByte(s, '{') < 0 {
		return
	}
	if !strings.Contains(s, `resource "`) && !strings.Contains(s, `data "`) && !strings.Contains(s, `provider "`) && !strings.Contains(s, `backend "`) {
		return
	}
	linhas := quebraLinhas(s)
	for i := 0; i < len(linhas); i++ {
		l := s[linhas[i][0]:linhas[i][1]]
		if !strings.Contains(l, `"`) || !strings.Contains(l, "{") {
			continue
		}
		tipo := ""
		if m := reCabHCL.FindStringSubmatch(l); m != nil {
			tipo = m[2]
		} else if !reCabHCL1.MatchString(l) {
			continue
		}
		pilha := []string{""} // blocos abertos (nome); o do cabeçalho é ""
		pilha = abreFechaHCL(l[strings.IndexByte(l, '{')+1:], pilha, "")
		j := i + 1
		for ; j < len(linhas) && len(pilha) > 0 && j-i < 5000; j++ {
			a, b := linhas[j][0], linhas[j][1]
			l := s[a:b]
			nome := ""
			if m := reAtribHCL.FindStringSubmatchIndex(l); m != nil {
				nome = l[m[2]:m[3]]
				atribHCL(s, a+m[1], b, nome, tipo, pilha, add)
			} else if t := strings.TrimLeft(l, " \t+~-"); t != "" {
				k := 0
				for k < len(t) && (ehIdent(t[k]) || t[k] == '-') {
					k++
				}
				nome = t[:k]
			}
			pilha = abreFechaHCL(l, pilha, nome)
		}
		i = j - 1
	}
}

// abreFechaHCL: atualiza a pilha de blocos com as chaves da linha (fora de strings).
func abreFechaHCL(l string, pilha []string, nome string) []string {
	aspas := false
	for k := 0; k < len(l); k++ {
		switch c := l[k]; {
		case c == '\\' && aspas:
			k++
		case c == '"':
			aspas = !aspas
		case aspas:
		case c == '{':
			pilha = append(pilha, nome)
		case c == '}':
			if len(pilha) > 0 {
				pilha = pilha[:len(pilha)-1]
			}
		}
	}
	return pilha
}

// atribHCL: o lado direito de "attr = ..." (a partir de v): literal, literal -> literal, ou
// mapa de tags em linha.
func atribHCL(s string, v, fim int, attr, tipo string, pilha []string, add func(ObjAchado)) {
	if v >= fim {
		return
	}
	prof := len(pilha)
	bloco := pilha[len(pilha)-1]
	// metadata { name = ... }: o bloco de metadados descreve o próprio recurso (provedor do
	// Kubernetes), então vale como o nível do recurso
	if prof == 2 && bloco == "metadata" {
		prof = 1
	}
	tags := bloco == "tags" || bloco == "tags_all" || bloco == "labels"
	if s[v] == '{' && (attr == "tags" || attr == "tags_all") {
		if m := reTagNome.FindStringSubmatchIndex(s[v:fim]); m != nil {
			addNome(s, v+m[2], v+m[3], entTipoHCL(tipo), "terraform", false, add)
		}
		return
	}
	for k := 0; k < 2 && v < fim && s[v] == '"'; k++ {
		q := fechaAspas(s, v, fim)
		if q < 0 {
			return
		}
		a, b := v+1, q
		val := s[a:b]
		if tags && attr == "Name" {
			addNome(s, a, b, entTipoHCL(tipo), "terraform", false, add)
		} else if ent, forte, ok := entAtribHCL(attr, tipo, prof, val); ok {
			if strings.Contains(val, "${") || strings.Contains(val, "%{") {
				addSegmentos(s, a, b, ent, "terraform", add)
			} else {
				addNome(s, a, b, ent, "terraform", forte, add)
			}
		}
		v = q + 1
		for v < fim && s[v] == ' ' {
			v++
		}
		if !strings.HasPrefix(s[v:fim], "->") {
			return
		}
		v += 2
		for v < fim && s[v] == ' ' {
			v++
		}
	}
}

// quebraLinhas: [ini, fim) de cada linha (sem o \r).
func quebraLinhas(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		f := len(s)
		if j >= 0 {
			f = i + j
		}
		e := f
		if e > i && s[e-1] == '\r' {
			e--
		}
		out = append(out, [2]int{i, e})
		i = f + 1
	}
	return out
}

// ---------------------------------------------------------------------------------------
// Ansible: inventário INI

var (
	reBicepRes  = regexp.MustCompile(`(?m)^[ \t]*resource[ \t]+\w+[ \t]+'([A-Za-z0-9.]+/[A-Za-z0-9./]+)@[^'\n]*'[ \t]+(?:existing[ \t]+)?=[ \t]*\{`)
	reBicepNome = regexp.MustCompile(`^[ \t]*name:[ \t]*'([^'\n]*)'`)
)

func acharBicep(s string, add func(ObjAchado)) {
	if !strings.Contains(s, "'Microsoft.") || !strings.Contains(s, "resource ") {
		return
	}
	ms := reBicepRes.FindAllStringSubmatchIndex(s, -1)
	for k, m := range ms {
		tipo := s[m[2]:m[3]]
		lim := len(s) // o corpo vai no máximo até o próximo recurso
		if k+1 < len(ms) {
			lim = ms[k+1][0]
		}
		prof := 1
		for i := m[1]; i < lim && prof > 0; {
			j := strings.IndexByte(s[i:lim], '\n')
			f := lim
			if j >= 0 {
				f = i + j
			}
			l := s[i:f]
			if prof == 1 {
				if n := reBicepNome.FindStringSubmatchIndex(l); n != nil {
					a, b := i+n[2], i+n[3]
					if strings.Contains(s[a:b], "${") {
						addSegmentos(s, a, b, entAzure(tipo[strings.LastIndexByte(tipo, '/')+1:]), "bicep", add)
					} else {
						addNomeAzure(s, a, b, tipo, "bicep", add)
					}
				}
			}
			prof += strings.Count(l, "{") - strings.Count(l, "}")
			i = f + 1
		}
	}
}

// ---------------------------------------------------------------------------------------
// Jenkinsfile
