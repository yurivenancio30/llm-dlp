package mask

import "testing"

// Os leitores nunca podem quebrar com entrada estranha (o proxy recusaria a mensagem).
func FuzzLeitores(f *testing.F) {
	for _, s := range []string{"SELECT a FROM `x..y`", "SELECT [", "Server=;Database=", "a|b\n---\n|", "jdbc:oracle:thin:@", "{{ ref('') }}", "urn:li:dataset:(urn:li:dataPlatform:x,,PROD)", "CREATE TABLE \"\" (", "relation \"\" does"} {
		f.Add(s)
	}
	// DevOps
	for _, s := range []string{manifestoK8s, "apiVersion: v1\nkind: Config\nclusters:\n- name: arn:\n  cluster:\n    server: https://\n",
		"apiVersion: v1\nkind: Pod\nmetadata: {name: [a, {b: }], namespace: 'x''}\n- - -\n  - image: a.b/\n",
		`{"apiVersion": "v1", "kind": "X", "metadata": {"name": "a"}, "items": [{"image": "h.x:/"}]]}`,
		"services:\n  a:\n    image: x.y/z\n    volumes:\n      - :\nvolumes:\n  :\n",
		"resource \"a_b\" \"c\" {\n  name = \"${\" -> \"\n  tags = { Name = \"\n}\n", "  ~ resource \"x\" \"y\" {\n      ~ bucket = \"a\" -> \"b",
		"[g]\nh[01:\nansible_host=\n[g:vars]\nansible_user=''\n", "all:\n  hosts:\n    :\n", "- hosts: a,:&!\n  tasks: []\n",
		"Resources:\n  X:\n    Properties:\n      BucketName: !Sub '${'\n", `{"resources": [{"type": "Microsoft.A/b/c", "name": "/a//"}]}`,
		"resource s 'Microsoft.X/y@1' = {\n  name: '${'\n", "pipeline {\n agent { label '' }\n image 'a.b/'\n}", ".svc.cluster.local", "a..svc.cluster.local",
		"jobs:\n  b:\n    runs-on: [\n    environment: {name: }\n    steps:\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, l := range leitoresPadrao() {
			l.Achar(s, func(o ObjAchado) {
				if o.Ini < 0 || o.Fim > len(s) || o.Fim < o.Ini {
					t.Fatalf("%s: trecho fora do texto %d..%d de %d", l.Nome, o.Ini, o.Fim, len(s))
				}
			})
		}
	})
}
