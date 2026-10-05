package mask

import "testing"

// Os leitores nunca podem quebrar com entrada estranha (o proxy recusaria a mensagem).
func FuzzLeitores(f *testing.F) {
	for _, s := range []string{"SELECT a FROM `x..y`", "SELECT [", "Server=;Database=", "a|b\n---\n|", "jdbc:oracle:thin:@", "{{ ref('') }}", "urn:li:dataset:(urn:li:dataPlatform:x,,PROD)", "CREATE TABLE \"\" (", "relation \"\" does"} {
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
