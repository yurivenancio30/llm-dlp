package mask

import (
	"regexp"
	"strings"
)

// Infraestrutura como código: Terraform/HCL, Bicep, ARM e CloudFormation (ver docs/pt-BR/estruturas.md).

// entAzure: entidade pelo tipo do recurso (Microsoft.Sql/servers/databases -> último pedaço).
func entAzure(sgm string) string {
	switch strings.ToLower(sgm) {
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

// addAzureName: o nome de um recurso ARM/Bicep; filho vem como "pai/filho", um pedaço por tipo.
func addAzureName(s string, a, b int, typ, rule string, add func(ObjFinding)) {
	sgmT := strings.Split(typ, "/")[1:]
	ps := strings.Split(s[a:b], "/")
	for k, p := range ps {
		ent := "servico"
		if t := len(sgmT) - len(ps) + k; t >= 0 && t < len(sgmT) {
			ent = entAzure(sgmT[t])
		}
		addName(s, a, a+len(p), ent, rule, true, add)
		a += len(p) + 1
	}
}

var (
	reHCLHdr  = regexp.MustCompile(`^[ \t]*(?:[+~-]|-/\+|\+/-|<=)?[ \t]*(resource|data)[ \t]+"([^"\s]+)"[ \t]+"[^"\s]+"[ \t]*\{`)
	reHCLHdr1 = regexp.MustCompile(`^[ \t]*(?:provider|backend)[ \t]+"[^"\s]+"[ \t]*\{`)
	reHCLAttr = regexp.MustCompile(`^[ \t]*(?:[+~-]|-/\+|\+/-)?[ \t]*"?([A-Za-z_][\w-]*)"?[ \t]*=[ \t]*`)
	reTagName = regexp.MustCompile(`"?\bName"?[ \t]*[=:][ \t]*"([^"\n]*)"`)
)

// núcleo do nome do atributo -> entidade
var hclCore = map[string]string{"bucket": "bucket", "database": "database", "db": "database", "server": "servidor",
	"instance": "servidor", "cluster": "servidor", "host": "servidor", "hostname": "servidor", "account": "conta_nuvem",
	"project": "conta_nuvem", "namespace": "namespace", "topic": "fila", "queue": "fila", "subscription": "fila",
	"repository": "repositorio", "repo": "repositorio", "dataset": "schema", "schema": "schema", "table": "tabela",
	"username": "usuario", "user": "usuario", "organization": "organizacao", "org": "organizacao", "folder": "pasta",
	"function": "servico"}

// entHCLType: a entidade do atributo name/identifier pelo tipo do recurso (aws_s3_bucket...).
func entHCLType(t string) string {
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

// entHCLAttr: (entidade, forte, ok) do atributo attr no recurso de tipo t, na profundidade prof.
func entHCLAttr(attr, t string, depth int, value string) (string, bool, bool) {
	ps := strings.Split(strings.ToLower(attr), "_")
	lastPos := ps[len(ps)-1]
	core := lastPos
	if lastPos == "name" || lastPos == "identifier" || lastPos == "id" || lastPos == "ids" {
		if len(ps) == 1 {
			if lastPos == "id" || lastPos == "ids" || t == "" || depth > 1 {
				return "", false, false
			}
			return entHCLType(t), depth == 1, true
		}
		core = ps[len(ps)-2]
	}
	ent, ok := hclCore[core]
	if !ok {
		return "", false, false
	}
	if core == "account" && !(allDigits(value) && len(value) == 12) {
		if strings.Contains(strings.ToLower(attr), "storage") {
			ent = "bucket"
		} else if allDigits(value) {
			return "", false, false
		}
	}
	return ent, depth == 1, true
}

func findTerraform(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '"') < 0 || strings.IndexByte(s, '{') < 0 {
		return
	}
	if !strings.Contains(s, `resource "`) && !strings.Contains(s, `data "`) && !strings.Contains(s, `provider "`) && !strings.Contains(s, `backend "`) {
		return
	}
	lines := wrapLines(s)
	for i := 0; i < len(lines); i++ {
		l := s[lines[i][0]:lines[i][1]]
		if !strings.Contains(l, `"`) || !strings.Contains(l, "{") {
			continue
		}
		typ := ""
		if m := reHCLHdr.FindStringSubmatch(l); m != nil {
			typ = m[2]
		} else if !reHCLHdr1.MatchString(l) {
			continue
		}
		stack := []string{""} // blocos abertos (nome); o do cabeçalho é ""
		stack = openCloseHCL(l[strings.IndexByte(l, '{')+1:], stack, "")
		j := i + 1
		for ; j < len(lines) && len(stack) > 0 && j-i < 5000; j++ {
			a, b := lines[j][0], lines[j][1]
			l := s[a:b]
			name := ""
			if m := reHCLAttr.FindStringSubmatchIndex(l); m != nil {
				name = l[m[2]:m[3]]
				hclAttr(s, a+m[1], b, name, typ, stack, add)
			} else if t := strings.TrimLeft(l, " \t+~-"); t != "" {
				k := 0
				for k < len(t) && (isIdent(t[k]) || t[k] == '-') {
					k++
				}
				name = t[:k]
			}
			stack = openCloseHCL(l, stack, name)
		}
		i = j - 1
	}
}

// openCloseHCL: atualiza a pilha de blocos com as chaves da linha (fora de strings).
func openCloseHCL(l string, stack []string, name string) []string {
	quotes := false
	for k := 0; k < len(l); k++ {
		switch c := l[k]; {
		case c == '\\' && quotes:
			k++
		case c == '"':
			quotes = !quotes
		case quotes:
		case c == '{':
			stack = append(stack, name)
		case c == '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return stack
}

// hclAttr: o lado direito de "attr = ..." (a partir de v): literal, literal -> literal, ou
// mapa de tags em linha.
func hclAttr(s string, v, end int, attr, typ string, stack []string, add func(ObjFinding)) {
	if v >= end {
		return
	}
	depth := len(stack)
	block := stack[len(stack)-1]
	// metadata { name = ... }: o bloco de metadados descreve o próprio recurso (provedor do
	// Kubernetes), então vale como o nível do recurso
	if depth == 2 && block == "metadata" {
		depth = 1
	}
	tags := block == "tags" || block == "tags_all" || block == "labels"
	if s[v] == '{' && (attr == "tags" || attr == "tags_all") {
		if m := reTagName.FindStringSubmatchIndex(s[v:end]); m != nil {
			addName(s, v+m[2], v+m[3], entHCLType(typ), "terraform", false, add)
		}
		return
	}
	for k := 0; k < 2 && v < end && s[v] == '"'; k++ {
		q := closeQuotes(s, v, end)
		if q < 0 {
			return
		}
		a, b := v+1, q
		val := s[a:b]
		if tags && attr == "Name" {
			addName(s, a, b, entHCLType(typ), "terraform", false, add)
		} else if ent, strong, ok := entHCLAttr(attr, typ, depth, val); ok {
			if strings.Contains(val, "${") || strings.Contains(val, "%{") {
				addSegments(s, a, b, ent, "terraform", add)
			} else {
				addName(s, a, b, ent, "terraform", strong, add)
			}
		}
		v = q + 1
		for v < end && s[v] == ' ' {
			v++
		}
		if !strings.HasPrefix(s[v:end], "->") {
			return
		}
		v += 2
		for v < end && s[v] == ' ' {
			v++
		}
	}
}

// wrapLines: [ini, fim) de cada linha (sem o \r).
func wrapLines(s string) [][2]int {
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
	reBicepName = regexp.MustCompile(`^[ \t]*name:[ \t]*'([^'\n]*)'`)
)

func findBicep(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "'Microsoft.") || !strings.Contains(s, "resource ") {
		return
	}
	ms := reBicepRes.FindAllStringSubmatchIndex(s, -1)
	for k, m := range ms {
		typ := s[m[2]:m[3]]
		lim := len(s) // o corpo vai no máximo até o próximo recurso
		if k+1 < len(ms) {
			lim = ms[k+1][0]
		}
		depth := 1
		for i := m[1]; i < lim && depth > 0; {
			j := strings.IndexByte(s[i:lim], '\n')
			f := lim
			if j >= 0 {
				f = i + j
			}
			l := s[i:f]
			if depth == 1 {
				if n := reBicepName.FindStringSubmatchIndex(l); n != nil {
					a, b := i+n[2], i+n[3]
					if strings.Contains(s[a:b], "${") {
						addSegments(s, a, b, entAzure(typ[strings.LastIndexByte(typ, '/')+1:]), "bicep", add)
					} else {
						addAzureName(s, a, b, typ, "bicep", add)
					}
				}
			}
			depth += strings.Count(l, "{") - strings.Count(l, "}")
			i = f + 1
		}
	}
}

// ---------------------------------------------------------------------------------------
// Jenkinsfile
