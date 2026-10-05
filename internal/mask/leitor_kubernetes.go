package mask

import (
	"regexp"
	"strings"
)

// Kubernetes, Helm e contêineres: manifestos, kubeconfig, imagens de registro privado, nome
// DNS de serviço e variável de ambiente em lista (ver docs/estruturas.md).

// registros públicos oficiais: a imagem fica
var registrosPublicos = map[string]bool{"docker.io": true, "ghcr.io": true, "quay.io": true, "gcr.io": true,
	"registry.k8s.io": true, "mcr.microsoft.com": true, "public.ecr.aws": true}

// addImagem: [registro/]caminho[:tag][@digest]. Só registro privado (primeiro pedaço com ponto
// ou porta, fora da lista pública): o registro é servidor, cada pedaço do caminho é serviço; a
// tag e o digest ficam.
func addImagem(s string, a, b int, regra string, add func(ObjAchado)) {
	v := s[a:b]
	if strings.ContainsAny(v, " {}$") {
		return
	}
	bar := strings.IndexByte(v, '/')
	if bar <= 0 {
		return
	}
	reg := v[:bar]
	if !strings.ContainsAny(reg, ".:") {
		return
	}
	host := reg
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if strings.EqualFold(host, "localhost") || registrosPublicos[strings.ToLower(host)] {
		return
	}
	fim := len(v)
	if strings.IndexByte(reg, '@') >= 0 {
		return
	}
	if i := strings.IndexByte(v[bar:], '@'); i >= 0 {
		fim = bar + i
	}
	if i := strings.LastIndexByte(v[:fim], ':'); i > bar {
		fim = i
	}
	if !ehIPv4(host) {
		addNome(s, a, a+len(host), "servidor", regra, true, add)
	}
	k := a + bar + 1
	for _, p := range strings.Split(v[bar+1:fim], "/") {
		addNome(s, k, k+len(p), "servico", regra, true, add)
		k += len(p) + 1
	}
}

func entKind(kind string) string {
	switch kind {
	case "Namespace":
		return "namespace"
	case "ServiceAccount":
		return "usuario"
	}
	return "servico"
}

func regraK8s(s string, e *yEnt, rel []string, kind, v string, add func(ObjAchado)) {
	n := len(rel)
	k := rel[n-1]
	ant := ""
	if n >= 2 {
		ant = rel[n-2]
	}
	if kind == "Config" { // kubeconfig
		regraKubeconfig(s, e, rel, v, add)
		return
	}
	if v == "" {
		return
	}
	a, b := e.vi, e.vf
	switch {
	case n == 2 && rel[0] == "metadata" && k == "name":
		addNome(s, a, b, entKind(kind), "k8s", true, add)
	case k == "namespace" && ant == "metadata":
		addNome(s, a, b, "namespace", "k8s", true, add)
	case k == "serviceName" || k == "claimName" || k == "secretName":
		addNome(s, a, b, "servico", "k8s", true, add)
	case k == "serviceAccountName" || k == "serviceAccount":
		addNome(s, a, b, "usuario", "k8s", true, add)
	case k == "name" && (ant == "secretKeyRef" || ant == "configMapKeyRef" || ant == "secretRef" || ant == "configMapRef" || ant == "configMap" || ant == "service"):
		addNome(s, a, b, "servico", "k8s", true, add)
	case k == "name" && ant == "-" && n >= 3 && rel[n-3] == "imagePullSecrets":
		addNome(s, a, b, "servico", "k8s", true, add)
	case kind == "Ingress" && (k == "host" && n >= 3 && rel[n-3] == "rules" || k == "-" && ant == "hosts"):
		if strings.HasPrefix(v, "*.") {
			a += 2
		}
		addNome(s, a, b, "servidor", "k8s", true, add)
	case k == "image":
		addImagem(s, a, b, "imagem", add)
	case rotulosK8s[k] && (ant == "labels" || ant == "matchLabels" || ant == "selector"):
		addNome(s, a, b, "servico", "k8s-label", false, add)
	}
}

func regraKubeconfig(s string, e *yEnt, rel []string, v string, add func(ObjAchado)) {
	if v == "" {
		return
	}
	p := strings.Join(rel, ".")
	switch p {
	case "clusters.-.name", "contexts.-.name", "current-context", "contexts.-.context.cluster":
		addARNouNome(s, e.vi, e.vf, "servidor", "kubeconfig", add)
	case "users.-.name", "contexts.-.context.user":
		addARNouNome(s, e.vi, e.vf, "usuario", "kubeconfig", add)
	case "contexts.-.context.namespace":
		addNome(s, e.vi, e.vf, "namespace", "kubeconfig", true, add)
	case "clusters.-.cluster.server":
		i := strings.Index(v, "://")
		if i < 0 {
			return
		}
		a := e.vi + i + 3
		b := a
		for b < e.vf && s[b] != ':' && s[b] != '/' {
			b++
		}
		addNome(s, a, b, "servidor", "kubeconfig", true, add)
	}
}

func acharDNSK8s(s string, add func(ObjAchado)) {
	const suf = ".svc.cluster.local"
	for i := 0; ; {
		j := strings.Index(s[i:], suf)
		if j < 0 {
			return
		}
		f := i + j
		i = f + len(suf)
		a := f
		for a > 0 && f-a < 253 && (ehAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
			a--
		}
		ps := strings.Split(s[a:f], ".")
		if len(ps) < 2 {
			continue
		}
		ns := f - len(ps[len(ps)-1])
		sv := ns - 1 - len(ps[len(ps)-2])
		addNome(s, ns, f, "namespace", "k8s-dns", true, add)
		addNome(s, sv, ns-1, "servico", "k8s-dns", true, add)
	}
}

// Variável de ambiente em lista (Kubernetes, CI): "- name: DB_HOST" seguido de "value: x" no
// mesmo item segue a mesma regra de "DB_HOST: x" (o nome da variável diz o tipo do valor).
var reEnvLista = regexp.MustCompile(`(?m)^([ \t]*)-[ \t]+name:[ \t]*["']?([A-Za-z_][\w.\-]*)["']?[ \t]*\r?\n[ \t]+value:[ \t]*(["']?)([^\s"'#]+)`)

func acharEnvLista(s string, add func(ObjAchado)) {
	if !strings.Contains(s, "value:") || !strings.Contains(s, "name:") {
		return
	}
	for _, m := range reEnvLista.FindAllStringSubmatchIndex(s, -1) {
		ent, forte := entChave(s[m[4]:m[5]])
		if ent == "" {
			continue
		}
		// a linha "value:" tem que estar dentro do item (mais recuada que o "-")
		ini := strings.LastIndexByte(s[:m[8]], '\n') + 1
		if ini-m[0] < 0 {
			continue
		}
		marcarValor(s, m[8], m[9], ent, "env-lista", forte, add)
	}
}
