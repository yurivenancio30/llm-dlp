package mask

import (
	"fmt"
	"strings"
	"testing"
)

// Medições de tempo (go test -bench).

func textoDenso(seed int) string {
	var b strings.Builder
	for i := seed * 50; i < seed*50+50; i++ {
		fmt.Fprintf(&b, "2026-10-02 12:%02d:01 INFO job=%d status=200 owner=fulano%d@empresa-ficticia.com.br host=10.42.%d.%d srv=db%d.empresa-ficticia.intra latency=%dms path=/api/v1/recurso/%d\n", i%60, i, i%40, i%9, i%250, i%7, i%97, i)
	}
	return b.String()
}

func textoComum(seed int) string {
	var b strings.Builder
	for i := seed * 50; i < seed*50+50; i++ {
		fmt.Fprintf(&b, "    if err := enc.Encode(v); err != nil { return nil, nil, err } // linha %d de codigo comum com status=200 e latency=%dms\n", i, i%97)
	}
	return b.String()
}

func BenchmarkFrioDenso(b *testing.B) {
	m := novoTeste(&testing.T{})
	n := 0
	for i := 0; i < b.N; i++ {
		s := textoDenso(i)
		n += len(s)
		m.Mascarar(s)
	}
	b.SetBytes(int64(n / b.N))
}

func BenchmarkFrioComum(b *testing.B) {
	m := novoTeste(&testing.T{})
	n := 0
	for i := 0; i < b.N; i++ {
		s := textoComum(i)
		n += len(s)
		m.Mascarar(s)
	}
	b.SetBytes(int64(n / b.N))
}

// igual ao FrioComum, mas com valores já aprendidos (inclusive de antes de um reinício)
func BenchmarkFrioComumComConhecidos(b *testing.B) {
	dir := b.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m, _ := NovoMasker(novoTeste(&testing.T{}).cfg, chaveTeste, nil, vs)
	m.Mascarar("senha: Xk9$mQ2vLp7wZt4 e cpf " + gerarCPF("529982247") + " e RG: 12.345.678-9")
	vs.SalvarSeSujo()
	vs2, _ := CarregarVistos(dir + "/v.json")
	ps, _ := CarregarPessoas(dir + "/p.json")
	ps.Importar(NovoPseudo(chaveTeste), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	m2, _ := NovoMasker(m.cfg, chaveTeste, ps, vs2)
	n := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := textoComum(i)
		n += len(s)
		m2.Mascarar(s)
	}
	b.SetBytes(int64(n / b.N))
}

// texto cheio de caminhos e URLs: o pior caso para o teste de pedaços de senha
func textoCaminhos(seed int) string {
	var b strings.Builder
	for i := seed * 50; i < seed*50+50; i++ {
		fmt.Fprintf(&b, "GET https://api.exemplo.com/v1/recurso/%d/itens?pagina=%d&ordem=nome&filtro=ativo:sim#topo /home/app/src/modulo%d/arquivo_%d.go:12\n", i, i%9, i%7, i)
	}
	return b.String()
}

func benchCaminhos(b *testing.B, comSenha bool) {
	dir := b.TempDir()
	vs, _ := CarregarVistos(dir + "/v.json")
	m, _ := NovoMasker(novoTeste(&testing.T{}).cfg, chaveTeste, nil, vs)
	if comSenha {
		m.Mascarar("DB_PASSWORD=Ficticia@2024")
	}
	n := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := textoCaminhos(i)
		n += len(s)
		m.Mascarar(s)
	}
	b.SetBytes(int64(n / b.N))
}

func BenchmarkFrioCaminhosSemSenha(b *testing.B) { benchCaminhos(b, false) }

func BenchmarkFrioCaminhosComSenha(b *testing.B) { benchCaminhos(b, true) }

func BenchmarkMascarar5KB(b *testing.B) {
	m := novoTeste(&testing.T{})
	base := strings.Repeat("linha de log comum com status=200 latency=12ms path=/api/v1/x ", 80)
	for i := 0; i < b.N; i++ {
		m.Detectar(base + fmt.Sprint(i))
	}
}

// Linha única longa (JSON minificado, resposta de API, HTML): o tempo tem que crescer de forma
// linear com o tamanho. Cada item é fictício.
func linhaLonga(tipo string, tam int) string {
	var b strings.Builder
	for i := 0; b.Len() < tam; i++ {
		switch tipo {
		case "url":
			fmt.Fprintf(&b, "https://site%d.example.com/a/b?q=%d ", i, i)
		case "sqljson":
			fmt.Fprintf(&b, `{"id":%d,"sql":"SELECT a, b FROM sch_x.tb_x%d WHERE c = 1 AND d = 2","ok":true},`, i, i%50)
		case "urn":
			fmt.Fprintf(&b, `{"urn":"urn:li:dataset:(urn:li:dataPlatform:mssql,db_x.sch_x.tb_x%d,PROD)"},`, i%50)
		case "email":
			fmt.Fprintf(&b, `{"de":"fulano%d`+"@"+`empresa-ficticia.com.br","n":%d},`, i%50, i)
		case "k8sdns":
			fmt.Fprintf(&b, `{"svc":"svc-x%d.ns-x.svc.cluster.local:8080"},`, i%50)
		case "conexao":
			fmt.Fprintf(&b, `{"c":"Server=srv-x%d;Database=db_x;User Id=u_x;"},`, i%50)
		}
	}
	return b.String()
}

func benchLinhaLonga(b *testing.B, tipo string, tam int) {
	s := linhaLonga(tipo, tam)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, _ := NovoMasker(novoTeste(&testing.T{}).cfg, chaveTeste, nil, nil)
		m.Mascarar(s)
	}
}

func BenchmarkLinhaURL1MB(b *testing.B)      { benchLinhaLonga(b, "url", 1<<20) }
func BenchmarkLinhaURL512K(b *testing.B)     { benchLinhaLonga(b, "url", 512<<10) }
func BenchmarkLinhaSQLJSON512K(b *testing.B) { benchLinhaLonga(b, "sqljson", 512<<10) }
func BenchmarkLinhaURN512K(b *testing.B)     { benchLinhaLonga(b, "urn", 512<<10) }
func BenchmarkLinhaEmail512K(b *testing.B)   { benchLinhaLonga(b, "email", 512<<10) }
func BenchmarkLinhaK8sDNS512K(b *testing.B)  { benchLinhaLonga(b, "k8sdns", 512<<10) }
func BenchmarkLinhaConexao512K(b *testing.B) { benchLinhaLonga(b, "conexao", 512<<10) }
