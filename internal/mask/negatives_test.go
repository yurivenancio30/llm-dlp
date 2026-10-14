package mask

import "testing"

// Nada mascarado a mais em texto comum (prosa em português e inglês, código Go e Python,
// YAML público, logs comuns, saídas de ps, ls e git log), com e sem a extensão da dica de uma
// saída de comando.
var t8Negatives = []struct{ name, text string }{
	{"prosa pt", "Hoje revisei o relatório de vendas com a equipe e combinamos de rodar a carga de novo amanhã cedo.\nO resultado ficou melhor que o da semana passada.\n"},
	{"prosa en", "We reviewed the quarterly report with the team and agreed to rerun the load tomorrow morning.\nThe numbers look better than last week.\n"},
	{"go", "package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc main() {\n\tnomes := []string{\"a\", \"b\"}\n\tfmt.Println(strings.Join(nomes, \",\"))\n}\n"},
	{"python", "import json\n\ndef carregar(caminho):\n    with open(caminho) as f:\n        return json.load(f)\n\nif __name__ == \"__main__\":\n    dados = carregar(\"config.json\")\n    print(len(dados))\n"},
	{"yaml público", "version: 2\nupdates:\n  - package-ecosystem: gomod\n    directory: /\n    schedule:\n      interval: weekly\n"},
	{"log", "2026-10-05 10:11:12 INFO starting server on :8080\n2026-10-05 10:11:13 INFO ready\n2026-10-05 10:12:00 WARN slow request took 1200ms\n"},
	{"ps", "  PID TTY          TIME CMD\n 4211 pts/0    00:00:00 bash\n 4302 pts/0    00:00:01 python3\n 4410 pts/0    00:00:00 ps\n"},
	{"ls", "README.md\ncmd\ndocs\ngo.mod\ngo.sum\ninternal\nscripts\n"},
	{"git log", "a1b2c3d corrige o cabeçalho do relatório\ne4f5a6b acrescenta a carga mensal\n0c9d8e7 atualiza dependências\n"},
}

func TestT8Negatives(t *testing.T) {
	for _, c := range t8Negatives {
		for _, hint := range []string{"", WithExtension("", "s;p=1")} {
			m := newTest(t)
			m.cfg.InternalDomains, m.cfg.Terms = nil, nil
			r, _ := m.maskD(c.text, true, hint)
			if r.text != c.text {
				t.Errorf("%s (dica %q): mascarou\n%s", c.name, hint, r.text)
			}
		}
	}
}
