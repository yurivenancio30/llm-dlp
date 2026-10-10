package mask

import (
	"regexp"
	"strings"
)

// Kubernetes, Helm e contêineres: manifestos, kubeconfig, imagens de registro privado, nome
// DNS de serviço e variável de ambiente em lista (ver docs/estruturas.md).

// registros públicos: no Docker Hub, ghcr.io, quay.io e public.ecr.aws qualquer um publica (a
// organização pode ser do cliente, ver caminhoPublico); nos de fornecedor (registrosFornecedor)
// tudo é do fornecedor e a imagem fica.
var registrosPublicos = map[string]bool{"docker.io": true, "index.docker.io": true, "ghcr.io": true, "quay.io": true,
	"registry.k8s.io": true, "k8s.gcr.io": true, "mcr.microsoft.com": true, "public.ecr.aws": true}

var registrosFornecedor = map[string]bool{"registry.k8s.io": true, "k8s.gcr.io": true, "mcr.microsoft.com": true}

// addImagem: [registro/]caminho[:tag][@digest]. Imagem sem registro (nginx, bitnami/redis) ou de
// registro público (quay.io, ghcr.io): ver caminhoPublico. Registro de nuvem: o host é do
// provedor e o cliente está no host (conta do ECR, registro do ACR) ou no primeiro pedaço do
// caminho (projeto do GCR e do Artifact Registry). Registro privado: o host é servidor. Cada
// pedaço do caminho é serviço; a tag e o digest ficam.
func addImagem(s string, a, b int, regra string, add func(ObjAchado)) {
	v := s[a:b]
	if strings.ContainsAny(v, " {}$") {
		return
	}
	bar := strings.IndexByte(v, '/')
	if bar <= 0 || !strings.ContainsAny(v[:bar], ".:") {
		caminhoPublico(s, a, b, regra, add)
		return
	}
	reg := v[:bar]
	if strings.IndexByte(reg, '@') >= 0 {
		return
	}
	host := reg
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	lh := strings.ToLower(host)
	if lh == "localhost" || registrosFornecedor[lh] {
		return
	}
	if registrosPublicos[lh] {
		caminhoPublico(s, a+bar+1, b, regra, add)
		return
	}
	fim := fimCaminhoImagem(v, bar)
	primeiro := "servico" // tipo do primeiro pedaço do caminho
	switch {
	case lh == "gcr.io" || strings.HasSuffix(lh, ".gcr.io") || strings.HasSuffix(lh, ".pkg.dev"):
		primeiro = "conta_nuvem" // gcr.io/<projeto>/..., <região>-docker.pkg.dev/<projeto>/<repo>/...
	case strings.HasSuffix(lh, ".amazonaws.com") && strings.Contains(lh, ".dkr.ecr."):
		if p := strings.IndexByte(host, '.'); p == 12 && todoDigitos(host[:p]) {
			addNome(s, a, a+p, "conta_nuvem", regra, true, add)
		}
	case strings.HasSuffix(lh, ".azurecr.io"):
		addNome(s, a, a+strings.IndexByte(host, '.'), "servidor", regra, true, add)
	default:
		if !ehIPv4(host) {
			addNome(s, a, a+len(host), "servidor", regra, true, add)
		}
	}
	k := a + bar + 1
	for n, p := range strings.Split(v[bar+1:fim], "/") {
		ent := "servico"
		if n == 0 {
			ent = primeiro
		}
		addNome(s, k, k+len(p), ent, regra, true, add)
		k += len(p) + 1
	}
}

// fimCaminhoImagem: o fim do caminho de v (sem a tag e o digest); bar: a barra antes do caminho.
func fimCaminhoImagem(v string, bar int) int {
	fim := len(v)
	if i := strings.IndexByte(v[max(bar, 0):], '@'); i >= 0 {
		fim = max(bar, 0) + i
	}
	if i := strings.LastIndexByte(v[:fim], ':'); i > bar {
		fim = i
	}
	return fim
}

// caminhoPublico: o caminho de uma imagem do Docker Hub ou de um registro público, s[a:b] =
// [org/]nome[:tag][@digest]. Público só com prova: sem organização, uma Imagem Oficial do
// Docker (redis) ou palavra da referência pública; com organização, as duas chaves: a
// organização é pública (referência ou fornecedor) E cada pedaço do nome é software público
// (bitnami/redis, grafana/loki, apache/airflow). Fora disso o caminho é do dono
// (vendashx/api-cobranca, uma imagem local "api-x", e também bitnami/api-vendashx): cada
// pedaço é serviço.
func caminhoPublico(s string, a, b int, regra string, add func(ObjAchado)) {
	v := s[a:b]
	fim := fimCaminhoImagem(v, strings.LastIndexByte(v, '/'))
	ps := strings.Split(v[:fim], "/")
	if ps[0] == "" {
		return
	}
	org := strings.ToLower(ps[0])
	publica := len(ps) == 1 && (imagensOficiais[org] || refPublica[org]) ||
		len(ps) == 2 && org == "library" && imagensOficiais[strings.ToLower(ps[1])]
	if len(ps) > 1 && orgPublica(org) {
		publica = true
		for _, p := range ps[1:] {
			publica = publica && nomePublico(p)
		}
	}
	if publica {
		return
	}
	k := a
	for _, p := range ps {
		if p != "" {
			addNome(s, k, k+len(p), "servico", regra, true, add)
		}
		k += len(p) + 1
	}
}

// Referência de imagem em qualquer lugar: chave image/repository (YAML, JSON, Helm, valores de
// chart, trecho de manifesto sem kind), FROM do Dockerfile e docker/podman pull, push, run, tag.
// A forma da referência (registro com ponto ou porta antes da primeira barra) decide; o resto
// é o addImagem.
var (
	reRefImagem    = `[A-Za-z0-9][A-Za-z0-9._\-]*(?::\d+)?/[A-Za-z0-9._\-/]*[A-Za-z0-9_](?::[A-Za-z0-9._\-]+)?(?:@sha256:[0-9a-f]{64})?`
	reImagemChave  = regexp.MustCompile(`(?m)(?:^|[\s{,\[])["']?(?:image|imageName|imageRepository|repository)["']?[ \t]*[:=][ \t]*["']?(` + reRefImagem + `)`)
	reImagemFrom   = regexp.MustCompile(`(?mi)^[ \t]*FROM[ \t]+(?:--platform=\S+[ \t]+)?(` + reRefImagem + `)`)
	reImagemDocker = regexp.MustCompile(`\b(?:docker|podman|nerdctl)[ \t]+(?:image[ \t]+)?(?:pull|push|run|tag|create)\b([^\n|;&]*)`)
	reTokImagem    = regexp.MustCompile(`^` + reRefImagem + `$`)
)

// temFrom: s tem a palavra FROM em qualquer caixa (a regex do Dockerfile não diferencia).
func temFrom(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if c := s[i] | 0x20; c == 'f' && s[i+1]|0x20 == 'r' && s[i+2]|0x20 == 'o' && s[i+3]|0x20 == 'm' {
			return true
		}
	}
	return false
}

func acharImagens(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '/') < 0 {
		return
	}
	// cada regex só roda se o texto tem a palavra que ela exige (sem isso, o leitor custava um
	// quarto do tempo de um texto comum)
	var res []*regexp.Regexp
	if strings.Contains(s, "mage") || strings.Contains(s, "repository") { // image, imageName, Image
		res = append(res, reImagemChave)
	}
	if temFrom(s) {
		res = append(res, reImagemFrom)
	}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			addImagem(s, m[2], m[3], "imagem", add)
		}
	}
	if !strings.Contains(s, "docker") && !strings.Contains(s, "podman") && !strings.Contains(s, "nerdctl") {
		return
	}
	for _, m := range reImagemDocker.FindAllStringSubmatchIndex(s, -1) {
		for p := m[2]; p < m[3]; {
			for p < m[3] && (s[p] == ' ' || s[p] == '\t') {
				p++
			}
			q := p
			for q < m[3] && s[q] != ' ' && s[q] != '\t' {
				q++
			}
			if q > p && s[p] != '-' && reTokImagem.MatchString(s[p:q]) {
				addImagem(s, p, q, "imagem", add)
			}
			p = q
		}
	}
}

// imagem em qualquer forma, com ou sem "/" (redis:7, minio/minio, quay.io/x/y)
var reImagemQualquer = regexp.MustCompile(`(?m)(?:(?:^|[\s{,\[])["']?(?:image|imageName)["']?[ \t]*[:=]|^[ \t]*FROM(?:[ \t]+--platform=\S+)?)[ \t]*["']?([A-Za-z0-9][A-Za-z0-9._/:@\-]*)`)

// softwareDoTexto: os nomes de software público que o texto roda como imagem oficial — sem
// organização (redis, postgres, nginx) ou com a organização igual ao nome (minio/minio,
// hazelcast/hazelcast), num registro público. O serviço, host ou namespace com esse nome é o
// software, não o cliente; imagem de organização (acme/acme-api) não conta.
func softwareDoTexto(s string) map[string]bool {
	if !strings.Contains(s, "image") && !strings.Contains(s, "FROM") {
		return nil
	}
	var sw map[string]bool
	for _, m := range reImagemQualquer.FindAllStringSubmatchIndex(s, -1) {
		ref := s[m[2]:m[3]]
		if k := strings.IndexByte(ref, '@'); k >= 0 {
			ref = ref[:k]
		}
		ps := strings.Split(ref, "/")
		if len(ps) > 1 && (strings.ContainsAny(ps[0], ".:") || ps[0] == "localhost") {
			if !registrosPublicos[strings.ToLower(ps[0])] {
				continue
			}
			ps = ps[1:]
		}
		last := ps[len(ps)-1]
		if k := strings.IndexByte(last, ':'); k >= 0 {
			last = last[:k]
		}
		// só com prova de que é software público: imagem oficial do Docker ("redis",
		// "library/redis") ou organização igual ao nome e de palavra pública ("minio/minio").
		// Imagem sem organização fora da lista é imagem local ("image: api-x" com build:).
		org := ""
		if len(ps) == 2 {
			org = strings.ToLower(ps[0])
		}
		lv := strings.ToLower(last)
		if len(ps) > 2 || len(lv) < 2 || !((org == "" || org == "library") && imagensOficiais[lv] || org == lv && orgPublica(org) && nomePublico(lv)) {
			continue
		}
		if sw == nil {
			sw = map[string]bool{}
		}
		sw[lv] = true
	}
	return sw
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
		if m[0] > 0 && strings.LastIndexByte(s[m[0]-1:m[8]], '\n') < 0 {
			continue
		}
		marcarValor(s, m[8], m[9], ent, "env-lista", forte, add)
	}
}
