package mask

import (
	"regexp"
	"strings"
)

// Tabela de saída de ferramenta de linha de comando (kubectl get, oc get, docker ps, helm list,
// crictl ps...): cabeçalho em MAIÚSCULAS com colunas separadas por 2+ espaços ou tab, e as
// linhas alinhadas no começo de cada coluna. A coluna diz o que o valor é: NAME/NAMES é o nome
// do objeto, NAMESPACE o namespace, IMAGE a imagem, EXTERNAL-IP o IP público da infraestrutura.
// Nada de ferramenta específica: só a forma do cabeçalho.

var reCabecalhoCLI = regexp.MustCompile(`^[A-Z][A-Z0-9()/_.-]*(?: [A-Z][A-Z0-9()/_.-]*)?$`)

// colunas que interessam -> tipo
var colunasCLI = map[string]string{"NAME": "servico", "NAMES": "servico", "NAMESPACE": "namespace", "IMAGE": "imagem",
	"EXTERNAL-IP": "ip", "EXTERNAL IP": "ip", "PUBLIC-IP": "ip", "PUBLIC IP": "ip", "CONTAINER": "servico"}

type colunaCLI struct {
	ini int // posição na linha (alinhado) ou índice (tab)
	ent string
}

// a linha de comando que gerou a tabela, logo acima dela no mesmo texto (terminal colado,
// README, doc). Sem ela a tabela não é tipada aqui: a saída de ferramenta sem o comando à vista
// fica com a dica do comando (ver chamada_comando.go), e "NAME STATUS AGE" solto pode ser qualquer coisa.
var reComandoCLI = regexp.MustCompile(`\b(?:kubectl|oc|docker|podman|nerdctl|crictl|helm|minikube|kind|k3s|microk8s|eksctl|az|gcloud|aws)[ \t]+[a-z]`)

// get ns: a coluna NAME é o namespace
var reComandoNS = regexp.MustCompile(`\bget[ \t]+(?:ns|namespaces?)\b`)

func acharTabelaCLI(s string, add func(ObjAchado)) {
	if !strings.Contains(s, "NAME") || !reComandoCLI.MatchString(s) {
		return
	}
	var cols []colunaCLI
	tab := false
	nl, cmdNl, ns := 0, -10, false
	for a := 0; a < len(s); {
		b := strings.IndexByte(s[a:], '\n')
		if b < 0 {
			b = len(s)
		} else {
			b += a
		}
		linha := strings.TrimRight(s[a:b], "\r")
		nl++
		switch {
		case strings.TrimSpace(linha) == "":
			cols = nil
		case cols == nil:
			if reComandoCLI.MatchString(linha) {
				cmdNl, ns = nl, reComandoNS.MatchString(linha)
			} else if nl-cmdNl <= 3 {
				cols, tab = cabecalhoCLI(linha)
				for k := range cols {
					if ns && cols[k].ent == "servico" {
						cols[k].ent = "namespace"
					}
				}
			}
		default:
			linhaCLI2(s, a, linha, cols, tab, add)
		}
		a = b + 1
	}
}

// cabecalhoCLI: as colunas de interesse, se a linha é um cabeçalho de tabela de saída.
func cabecalhoCLI(l string) ([]colunaCLI, bool) {
	tab := strings.Contains(l, "\t")
	var nomes []string
	var inis []int
	if tab {
		for _, p := range strings.Split(l, "\t") {
			if t := strings.TrimSpace(p); t != "" {
				nomes = append(nomes, t)
				inis = append(inis, len(nomes)-1)
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
			nomes = append(nomes, l[i:j])
			inis = append(inis, i)
			i = j
		}
	}
	if len(nomes) < 3 {
		return nil, false
	}
	var cols []colunaCLI
	temNome := false
	for k, n := range nomes {
		if !reCabecalhoCLI.MatchString(n) {
			return nil, false
		}
		if e, ok := colunasCLI[n]; ok {
			cols = append(cols, colunaCLI{inis[k], e})
			temNome = temNome || n == "NAME" || n == "NAMES" || n == "NAMESPACE"
		}
	}
	if !temNome {
		return nil, false
	}
	return cols, tab
}

func linhaCLI2(s string, base int, l string, cols []colunaCLI, tab bool, add func(ObjAchado)) {
	var campos [][2]int // início e fim de cada campo na linha
	if tab {
		k := 0
		for _, p := range strings.Split(l, "\t") {
			if t := strings.TrimSpace(p); t != "" {
				i := k + strings.Index(p, t)
				campos = append(campos, [2]int{i, i + len(t)})
			}
			k += len(p) + 1
		}
	}
	for _, c := range cols {
		var i, j int
		if tab {
			if c.ini >= len(campos) {
				continue
			}
			i, j = campos[c.ini][0], campos[c.ini][1]
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
			addImagem(s, a, b, "saída-cli", add)
		case "ip":
			for _, p := range strings.Split(v, ",") {
				if ehIPv4(p) {
					add(ObjAchado{a, a + len(p), "servidor", "saída-cli", true})
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
				addNome(s, x, x+len(p), c.ent, "saída-cli", true, add)
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
	reIPNoTipo   = regexp.MustCompile(`"type"\s*:\s*"(?:ExternalIP|InternalIP)"\s*,\s*"address"\s*:\s*"(\d{1,3}(?:\.\d{1,3}){3})"`)
	reIPNoTipoAo = regexp.MustCompile(`"address"\s*:\s*"(\d{1,3}(?:\.\d{1,3}){3})"\s*,\s*"type"\s*:\s*"(?:ExternalIP|InternalIP)"`)
)

func acharIPInfra(s string, add func(ObjAchado)) {
	if strings.Contains(s, "loadBalancer") {
		for _, m := range reIPIngress.FindAllStringSubmatchIndex(s, -1) {
			for _, n := range reIPItem.FindAllStringSubmatchIndex(s[m[2]:m[3]], -1) {
				add(ObjAchado{m[2] + n[2], m[2] + n[3], "servidor", "ip-infra", true})
			}
		}
	}
	if strings.Contains(s, "ExternalIP") || strings.Contains(s, "InternalIP") {
		for _, re := range []*regexp.Regexp{reIPNoTipo, reIPNoTipoAo} {
			for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
				add(ObjAchado{m[2], m[3], "servidor", "ip-infra", true})
			}
		}
	}
}
