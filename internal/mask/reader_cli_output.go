package mask

import (
	"regexp"
	"strings"
)

// Table de saída de ferramenta de linha de comando (kubectl get, oc get, docker ps, helm list,
// crictl ps...): cabeçalho em MAIÚSCULAS com colunas separadas por 2+ espaços ou tab, e as
// linhas alinhadas no começo de cada coluna. A coluna diz o que o valor é: NAME/NAMES é o nome
// do objeto, NAMESPACE o namespace, IMAGE a imagem, EXTERNAL-IP o IP público da infraestrutura.
// Nada de ferramenta específica: só a forma do cabeçalho.

var reCLIHeader = regexp.MustCompile(`^[A-Z][A-Z0-9()/_.-]*(?: [A-Z][A-Z0-9()/_.-]*)?$`)

// colunas que interessam -> tipo
var cliColumns = map[string]string{"NAME": "servico", "NAMES": "servico", "NAMESPACE": "namespace", "IMAGE": "imagem",
	"EXTERNAL-IP": "ip", "EXTERNAL IP": "ip", "PUBLIC-IP": "ip", "PUBLIC IP": "ip", "CONTAINER": "servico"}

type cliColumn struct {
	ini int // posição na linha (alinhado) ou índice (tab)
	ent string
}

// a linha de comando que gerou a tabela, logo acima dela no mesmo texto (terminal colado,
// README, doc). Sem ela a tabela não é tipada aqui: a saída de ferramenta sem o comando à vista
// fica com a dica do comando (ver call_command.go), e "NAME STATUS AGE" solto pode ser qualquer coisa.
var reCLICommand = regexp.MustCompile(`\b(?:kubectl|oc|docker|podman|nerdctl|crictl|helm|minikube|kind|k3s|microk8s|eksctl|az|gcloud|aws)[ \t]+[a-z]`)

// get ns: a coluna NAME é o namespace
var reNSCommand = regexp.MustCompile(`\bget[ \t]+(?:ns|namespaces?)\b`)

func findCLITable(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "NAME") || !reCLICommand.MatchString(s) {
		return
	}
	var cols []cliColumn
	tab := false
	nl, cmdNl, ns := 0, -10, false
	for a := 0; a < len(s); {
		b := strings.IndexByte(s[a:], '\n')
		if b < 0 {
			b = len(s)
		} else {
			b += a
		}
		line := strings.TrimRight(s[a:b], "\r")
		nl++
		switch {
		case strings.TrimSpace(line) == "":
			cols = nil
		case cols == nil:
			if reCLICommand.MatchString(line) {
				cmdNl, ns = nl, reNSCommand.MatchString(line)
			} else if nl-cmdNl <= 3 {
				cols, tab = cliHeader(line)
				for k := range cols {
					if ns && cols[k].ent == "servico" {
						cols[k].ent = "namespace"
					}
				}
			}
		default:
			cliLine2(s, a, line, cols, tab, add)
		}
		a = b + 1
	}
}

// cliHeader: as colunas de interesse, se a linha é um cabeçalho de tabela de saída.
func cliHeader(l string) ([]cliColumn, bool) {
	tab := strings.Contains(l, "\t")
	var names []string
	var starts []int
	if tab {
		for _, p := range strings.Split(l, "\t") {
			if t := strings.TrimSpace(p); t != "" {
				names = append(names, t)
				starts = append(starts, len(names)-1)
			}
		}
	} else {
		for i := 0; i < len(l); {
			for i < len(l) && l[i] == ' ' {
				i++
			}
			if i >= len(l) {
				break
			}
			j := i
			for j < len(l) && !(l[j] == ' ' && (j+1 >= len(l) || l[j+1] == ' ')) {
				j++
			}
			names = append(names, l[i:j])
			starts = append(starts, i)
			i = j
		}
	}
	if len(names) < 3 {
		return nil, false
	}
	var cols []cliColumn
	hasName := false
	for k, n := range names {
		if !reCLIHeader.MatchString(n) {
			return nil, false
		}
		if e, ok := cliColumns[n]; ok {
			cols = append(cols, cliColumn{starts[k], e})
			hasName = hasName || n == "NAME" || n == "NAMES" || n == "NAMESPACE"
		}
	}
	if !hasName {
		return nil, false
	}
	return cols, tab
}

func cliLine2(s string, base int, l string, cols []cliColumn, tab bool, add func(ObjFinding)) {
	var fields [][2]int // início e fim de cada campo na linha
	if tab {
		k := 0
		for _, p := range strings.Split(l, "\t") {
			if t := strings.TrimSpace(p); t != "" {
				i := k + strings.Index(p, t)
				fields = append(fields, [2]int{i, i + len(t)})
			}
			k += len(p) + 1
		}
	}
	for _, c := range cols {
		var i, j int
		if tab {
			if c.ini >= len(fields) {
				continue
			}
			i, j = fields[c.ini][0], fields[c.ini][1]
		} else {
			i = c.ini
			if i >= len(l) || l[i] == ' ' || i > 0 && l[i-1] != ' ' {
				continue // linha fora do alinhamento: não é linha da tabela
			}
			j = i
			for j < len(l) && l[j] != ' ' {
				j++
			}
		}
		v := l[i:j]
		if v == "" || v[0] == '<' || v == "-" {
			continue
		}
		a, b := base+i, base+j
		switch c.ent {
		case "imagem":
			addImage(s, a, b, "saída-cli", add)
		case "ip":
			for _, p := range strings.Split(v, ",") {
				if isIPv4(p) {
					add(ObjFinding{a, a + len(p), "servidor", "saída-cli", true})
				}
				a += len(p) + 1
			}
		default:
			// pod/nome, deployment.apps/nome: o nome é depois da barra; NAMES do docker: a,b
			for _, p0 := range strings.Split(v, ",") {
				x, p := a, p0
				if k := strings.LastIndexByte(p0, '/'); k >= 0 {
					x, p = a+k+1, p0[k+1:]
				}
				addName(s, x, x+len(p), c.ent, "saída-cli", true, add)
				a += len(p0) + 1
			}
		}
	}
}

// IP público que o contexto diz ser da infraestrutura: o endereço do balanceador de um
// Service (status.loadBalancer.ingress[].ip) e o endereço de um nó ({"type": "ExternalIP",
// "address": ...}). Fora desse contexto o IP público fica (opção ip_publico).
var (
	reIPIngress  = regexp.MustCompile(`(?s)loadBalancer:\s*\n\s*ingress:\s*\n((?:[ \t]*-?[ \t]*(?:ip|hostname):[ \t]*\S+[ \t]*\n?)+)`)
	reIPItem     = regexp.MustCompile(`\bip:[ \t]*(\d{1,3}(?:\.\d{1,3}){3})\b`)
	reIPInType   = regexp.MustCompile(`"type"\s*:\s*"(?:ExternalIP|InternalIP)"\s*,\s*"address"\s*:\s*"(\d{1,3}(?:\.\d{1,3}){3})"`)
	reIPInTypeAo = regexp.MustCompile(`"address"\s*:\s*"(\d{1,3}(?:\.\d{1,3}){3})"\s*,\s*"type"\s*:\s*"(?:ExternalIP|InternalIP)"`)
)

func findInfraIP(s string, add func(ObjFinding)) {
	if strings.Contains(s, "loadBalancer") {
		for _, m := range reIPIngress.FindAllStringSubmatchIndex(s, -1) {
			for _, n := range reIPItem.FindAllStringSubmatchIndex(s[m[2]:m[3]], -1) {
				add(ObjFinding{m[2] + n[2], m[2] + n[3], "servidor", "ip-infra", true})
			}
		}
	}
	if strings.Contains(s, "ExternalIP") || strings.Contains(s, "InternalIP") {
		for _, re := range []*regexp.Regexp{reIPInType, reIPInTypeAo} {
			for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
				add(ObjFinding{m[2], m[3], "servidor", "ip-infra", true})
			}
		}
	}
}
