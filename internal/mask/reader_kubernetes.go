package mask

import (
	"regexp"
	"strings"
)

// Kubernetes, Helm and containers: manifests, kubeconfig, private registry images, service DNS name
// and environment variable in a list (see docs/structures.md).

// public registries: on Docker Hub, ghcr.io, quay.io and public.ecr.aws anyone publishes (the
// organization may be the customer's, see publicPath); on vendor ones (vendorRegistries) everything
// belongs to the vendor and the image stays.
var publicRegistries = map[string]bool{"docker.io": true, "index.docker.io": true, "ghcr.io": true, "quay.io": true,
	"registry.k8s.io": true, "k8s.gcr.io": true, "mcr.microsoft.com": true, "public.ecr.aws": true}

var vendorRegistries = map[string]bool{"registry.k8s.io": true, "k8s.gcr.io": true, "mcr.microsoft.com": true}

// addImage: [registry/]path[:tag][@digest]. Image without a registry (nginx, bitnami/redis) or from
// a public registry (quay.io, ghcr.io): see publicPath. Cloud registry: the host belongs to the
// provider and the customer is in the host (ECR account, ACR registry) or in the first piece of the
// path (GCR and Artifact Registry project). Private registry: the host is a server. Each piece of
// the path is a service; the tag and digest stay.
func addImage(s string, a, b int, rule string, add func(ObjFinding)) {
	v := s[a:b]
	if strings.ContainsAny(v, " {}$") {
		return
	}
	bar := strings.IndexByte(v, '/')
	if bar <= 0 || !strings.ContainsAny(v[:bar], ".:") {
		publicPath(s, a, b, rule, add)
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
	if lh == "localhost" || vendorRegistries[lh] {
		return
	}
	if publicRegistries[lh] {
		publicPath(s, a+bar+1, b, rule, add)
		return
	}
	end := imagePathEnd(v, bar)
	first := "service" // type of the first piece of the path
	switch {
	case lh == "gcr.io" || strings.HasSuffix(lh, ".gcr.io") || strings.HasSuffix(lh, ".pkg.dev"):
		first = "cloud_account" // gcr.io/<project>/..., <region>-docker.pkg.dev/<project>/<repo>/...
	case strings.HasSuffix(lh, ".amazonaws.com") && strings.Contains(lh, ".dkr.ecr."):
		if p := strings.IndexByte(host, '.'); p == 12 && allDigits(host[:p]) {
			addName(s, a, a+p, "cloud_account", rule, true, add)
		}
	case strings.HasSuffix(lh, ".azurecr.io"):
		addName(s, a, a+strings.IndexByte(host, '.'), "server", rule, true, add)
	default:
		if !isIPv4(host) {
			addName(s, a, a+len(host), "server", rule, true, add)
		}
	}
	k := a + bar + 1
	for n, p := range strings.Split(v[bar+1:end], "/") {
		ent := "service"
		if n == 0 {
			ent = first
		}
		addName(s, k, k+len(p), ent, rule, true, add)
		k += len(p) + 1
	}
}

// imagePathEnd: the end of the path of v (without the tag and digest); bar: the slash before the
// path.
func imagePathEnd(v string, bar int) int {
	end := len(v)
	if i := strings.IndexByte(v[max(bar, 0):], '@'); i >= 0 {
		end = max(bar, 0) + i
	}
	if i := strings.LastIndexByte(v[:end], ':'); i > bar {
		end = i
	}
	return end
}

// publicPath: the path of an image from Docker Hub or a public registry, s[a:b] =
// [org/]name[:tag][@digest]. Public only with proof: without an organization, a Docker Official
// Image (redis) or a word from the public reference; with an organization, both keys: the
// organization is public (reference or vendor) AND each piece of the name is public software
// (bitnami/redis, grafana/loki, apache/airflow). Otherwise the path belongs to the owner
// (vendashx/api-cobranca, a local image "api-x", and also bitnami/api-vendashx): each piece is a
// service.
func publicPath(s string, a, b int, rule string, add func(ObjFinding)) {
	v := s[a:b]
	end := imagePathEnd(v, strings.LastIndexByte(v, '/'))
	ps := strings.Split(v[:end], "/")
	if ps[0] == "" {
		return
	}
	org := strings.ToLower(ps[0])
	publicOne := len(ps) == 1 && (officialImages[org] || publicRef[org]) ||
		len(ps) == 2 && org == "library" && officialImages[strings.ToLower(ps[1])]
	if len(ps) > 1 && publicOrg(org) {
		publicOne = true
		for _, p := range ps[1:] {
			publicOne = publicOne && publicName(p)
		}
	}
	if publicOne {
		return
	}
	k := a
	for _, p := range ps {
		if p != "" {
			addName(s, k, k+len(p), "service", rule, true, add)
		}
		k += len(p) + 1
	}
}

// Image reference anywhere: image/repository key (YAML, JSON, Helm, chart values, manifest snippet
// without kind), Dockerfile FROM and docker/podman pull, push, run, tag. The shape of the reference
// (registry with a dot or port before the first slash) decides; the rest is addImage.
var (
	reImageRef    = `[A-Za-z0-9][A-Za-z0-9._\-]*(?::\d+)?/[A-Za-z0-9._\-/]*[A-Za-z0-9_](?::[A-Za-z0-9._\-]+)?(?:@sha256:[0-9a-f]{64})?`
	reImageKey    = regexp.MustCompile(`(?m)(?:^|[\s{,\[])["']?(?:image|imageName|imageRepository|repository)["']?[ \t]*[:=][ \t]*["']?(` + reImageRef + `)`)
	reFromImage   = regexp.MustCompile(`(?mi)^[ \t]*FROM[ \t]+(?:--platform=\S+[ \t]+)?(` + reImageRef + `)`)
	reDockerImage = regexp.MustCompile(`\b(?:docker|podman|nerdctl)[ \t]+(?:image[ \t]+)?(?:pull|push|run|tag|create)\b([^\n|;&]*)`)
	reImageTok    = regexp.MustCompile(`^` + reImageRef + `$`)
)

// hasFrom: s has the word FROM in any case (the Dockerfile regex is case-insensitive).
func hasFrom(s string) bool {
	for i := 0; i+3 < len(s); i++ {
		if c := s[i] | 0x20; c == 'f' && s[i+1]|0x20 == 'r' && s[i+2]|0x20 == 'o' && s[i+3]|0x20 == 'm' {
			return true
		}
	}
	return false
}

func findImages(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '/') < 0 {
		return
	}
	// each regex only runs if the text has the word it requires (without this, the reader cost a
	// quarter of the time of a common text)
	var res []*regexp.Regexp
	if strings.Contains(s, "mage") || strings.Contains(s, "repository") { // image, imageName, Image
		res = append(res, reImageKey)
	}
	if hasFrom(s) {
		res = append(res, reFromImage)
	}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			addImage(s, m[2], m[3], "image", add)
		}
	}
	if !strings.Contains(s, "docker") && !strings.Contains(s, "podman") && !strings.Contains(s, "nerdctl") {
		return
	}
	for _, m := range reDockerImage.FindAllStringSubmatchIndex(s, -1) {
		for p := m[2]; p < m[3]; {
			for p < m[3] && (s[p] == ' ' || s[p] == '\t') {
				p++
			}
			q := p
			for q < m[3] && s[q] != ' ' && s[q] != '\t' {
				q++
			}
			if q > p && s[p] != '-' && reImageTok.MatchString(s[p:q]) {
				addImage(s, p, q, "image", add)
			}
			p = q
		}
	}
}

// image in any shape, with or without "/" (redis:7, minio/minio, quay.io/x/y)
var reAnyImage = regexp.MustCompile(`(?m)(?:(?:^|[\s{,\[])["']?(?:image|imageName)["']?[ \t]*[:=]|^[ \t]*FROM(?:[ \t]+--platform=\S+)?)[ \t]*["']?([A-Za-z0-9][A-Za-z0-9._/:@\-]*)`)

// textSoftware: the names of public software the text runs as an official image — without an
// organization (redis, postgres, nginx) or with the organization equal to the name (minio/minio,
// hazelcast/hazelcast), on a public registry. The service, host or namespace with that name is the
// software, not the customer; an organization's image (acme/acme-api) does not count.
func textSoftware(s string) map[string]bool {
	if !strings.Contains(s, "image") && !strings.Contains(s, "FROM") {
		return nil
	}
	var sw map[string]bool
	for _, m := range reAnyImage.FindAllStringSubmatchIndex(s, -1) {
		ref := s[m[2]:m[3]]
		if k := strings.IndexByte(ref, '@'); k >= 0 {
			ref = ref[:k]
		}
		ps := strings.Split(ref, "/")
		if len(ps) > 1 && (strings.ContainsAny(ps[0], ".:") || ps[0] == "localhost") {
			if !publicRegistries[strings.ToLower(ps[0])] {
				continue
			}
			ps = ps[1:]
		}
		last := ps[len(ps)-1]
		if k := strings.IndexByte(last, ':'); k >= 0 {
			last = last[:k]
		}
		// only with proof that it is public software: official Docker image ("redis",
		// "library/redis") or organization equal to the name and a public word ("minio/minio"). An
		// image without an organization outside the list is a local image ("image: api-x" with
		// build:).
		org := ""
		if len(ps) == 2 {
			org = strings.ToLower(ps[0])
		}
		lv := strings.ToLower(last)
		if len(ps) > 2 || len(lv) < 2 || !((org == "" || org == "library") && officialImages[lv] || org == lv && publicOrg(org) && publicName(lv)) {
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
		return "user"
	}
	return "service"
}

func k8sRule(s string, e *yEnt, rel []string, kind, v string, add func(ObjFinding)) {
	n := len(rel)
	k := rel[n-1]
	prev := ""
	if n >= 2 {
		prev = rel[n-2]
	}
	if kind == "Config" { // kubeconfig
		kubeconfigRule(s, e, rel, v, add)
		return
	}
	if v == "" {
		return
	}
	a, b := e.vi, e.vf
	switch {
	case n == 2 && rel[0] == "metadata" && k == "name":
		addName(s, a, b, entKind(kind), "k8s", true, add)
	case k == "namespace" && prev == "metadata":
		addName(s, a, b, "namespace", "k8s", true, add)
	case k == "serviceName" || k == "claimName" || k == "secretName":
		addName(s, a, b, "service", "k8s", true, add)
	case k == "serviceAccountName" || k == "serviceAccount":
		addName(s, a, b, "user", "k8s", true, add)
	case k == "name" && (prev == "secretKeyRef" || prev == "configMapKeyRef" || prev == "secretRef" || prev == "configMapRef" || prev == "configMap" || prev == "service"):
		addName(s, a, b, "service", "k8s", true, add)
	case k == "name" && prev == "-" && n >= 3 && rel[n-3] == "imagePullSecrets":
		addName(s, a, b, "service", "k8s", true, add)
	case kind == "Ingress" && (k == "host" && n >= 3 && rel[n-3] == "rules" || k == "-" && prev == "hosts"):
		if strings.HasPrefix(v, "*.") {
			a += 2
		}
		addName(s, a, b, "server", "k8s", true, add)
	case k == "image":
		addImage(s, a, b, "image", add)
	case k8sLabels[k] && (prev == "labels" || prev == "matchLabels" || prev == "selector"):
		addName(s, a, b, "service", "k8s-label", false, add)
	}
}

func kubeconfigRule(s string, e *yEnt, rel []string, v string, add func(ObjFinding)) {
	if v == "" {
		return
	}
	p := strings.Join(rel, ".")
	switch p {
	case "clusters.-.name", "contexts.-.name", "current-context", "contexts.-.context.cluster":
		addARNOrName(s, e.vi, e.vf, "server", "kubeconfig", add)
	case "users.-.name", "contexts.-.context.user":
		addARNOrName(s, e.vi, e.vf, "user", "kubeconfig", add)
	case "contexts.-.context.namespace":
		addName(s, e.vi, e.vf, "namespace", "kubeconfig", true, add)
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
		addName(s, a, b, "server", "kubeconfig", true, add)
	}
}

func findK8sDNS(s string, add func(ObjFinding)) {
	const suf = ".svc.cluster.local"
	for i := 0; ; {
		j := strings.Index(s[i:], suf)
		if j < 0 {
			return
		}
		f := i + j
		i = f + len(suf)
		a := f
		for a > 0 && f-a < 253 && (isAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
			a--
		}
		ps := strings.Split(s[a:f], ".")
		if len(ps) < 2 {
			continue
		}
		ns := f - len(ps[len(ps)-1])
		sv := ns - 1 - len(ps[len(ps)-2])
		addName(s, ns, f, "namespace", "k8s-dns", true, add)
		addName(s, sv, ns-1, "service", "k8s-dns", true, add)
	}
}

// Environment variable in a list (Kubernetes, CI): "- name: DB_HOST" followed by "value: x" in the
// same item follows the same rule as "DB_HOST: x" (the variable name gives the value's type).
var reEnvList = regexp.MustCompile(`(?m)^([ \t]*)-[ \t]+name:[ \t]*["']?([A-Za-z_][\w.\-]*)["']?[ \t]*\r?\n[ \t]+value:[ \t]*(["']?)([^\s"'#]+)`)

func findEnvList(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "value:") || !strings.Contains(s, "name:") {
		return
	}
	for _, m := range reEnvList.FindAllStringSubmatchIndex(s, -1) {
		ent, strong := entKey(s[m[4]:m[5]])
		if ent == "" {
			continue
		}
		// the "value:" line has to be inside the item (more indented than the "-")
		if m[0] > 0 && strings.LastIndexByte(s[m[0]-1:m[8]], '\n') < 0 {
			continue
		}
		markValue(s, m[8], m[9], ent, "env-list", strong, add)
	}
}
