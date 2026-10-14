package mask

import (
	"fmt"
	"strings"
	"testing"
)

// Medições de tempo (go test -bench).

func denseText(seed int) string {
	var b strings.Builder
	for i := seed * 50; i < seed*50+50; i++ {
		fmt.Fprintf(&b, "2026-10-02 12:%02d:01 INFO job=%d status=200 owner=fulano%d@empresa-ficticia.com.br host=10.42.%d.%d srv=db%d.empresa-ficticia.intra latency=%dms path=/api/v1/recurso/%d\n", i%60, i, i%40, i%9, i%250, i%7, i%97, i)
	}
	return b.String()
}

func commonText(seed int) string {
	var b strings.Builder
	for i := seed * 50; i < seed*50+50; i++ {
		fmt.Fprintf(&b, "    if err := enc.Encode(v); err != nil { return nil, nil, err } // linha %d de codigo comum com status=200 e latency=%dms\n", i, i%97)
	}
	return b.String()
}

func BenchmarkColdDense(b *testing.B) {
	m := newTest(&testing.T{})
	n := 0
	for i := 0; i < b.N; i++ {
		s := denseText(i)
		n += len(s)
		m.Mask(s)
	}
	b.SetBytes(int64(n / b.N))
}

func BenchmarkColdCommon(b *testing.B) {
	m := newTest(&testing.T{})
	n := 0
	for i := 0; i < b.N; i++ {
		s := commonText(i)
		n += len(s)
		m.Mask(s)
	}
	b.SetBytes(int64(n / b.N))
}

// igual ao FrioComum, mas com valores já aprendidos (inclusive de antes de um reinício)
func BenchmarkColdCommonWithKnown(b *testing.B) {
	dir := b.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m, _ := NewMasker(newTest(&testing.T{}).cfg, testKey, nil, vs)
	m.Mask("senha: Xk9$mQ2vLp7wZt4 e cpf " + genCPF("529982247") + " e RG: 12.345.678-9")
	vs.SaveIfDirty()
	vs2, _ := LoadSeen(dir + "/v.json")
	ps, _ := LoadPeople(dir + "/p.json")
	ps.Import(NewPseudo(testKey), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	m2, _ := NewMasker(m.cfg, testKey, ps, vs2)
	n := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := commonText(i)
		n += len(s)
		m2.Mask(s)
	}
	b.SetBytes(int64(n / b.N))
}

// texto cheio de caminhos e URLs: o pior caso para o teste de pedaços de senha
func pathsText(seed int) string {
	var b strings.Builder
	for i := seed * 50; i < seed*50+50; i++ {
		fmt.Fprintf(&b, "GET https://api.exemplo.com/v1/recurso/%d/itens?pagina=%d&ordem=nome&filtro=ativo:sim#topo /home/app/src/modulo%d/arquivo_%d.go:12\n", i, i%9, i%7, i)
	}
	return b.String()
}

func benchPaths(b *testing.B, withPassword bool) {
	dir := b.TempDir()
	vs, _ := LoadSeen(dir + "/v.json")
	m, _ := NewMasker(newTest(&testing.T{}).cfg, testKey, nil, vs)
	if withPassword {
		m.Mask("DB_PASSWORD=Ficticia@2024")
	}
	n := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := pathsText(i)
		n += len(s)
		m.Mask(s)
	}
	b.SetBytes(int64(n / b.N))
}

func BenchmarkColdPathsNoPassword(b *testing.B) { benchPaths(b, false) }

func BenchmarkColdPathsWithPassword(b *testing.B) { benchPaths(b, true) }

func BenchmarkMask5KB(b *testing.B) {
	m := newTest(&testing.T{})
	base := strings.Repeat("linha de log comum com status=200 latency=12ms path=/api/v1/x ", 80)
	for i := 0; i < b.N; i++ {
		m.Detect(base + fmt.Sprint(i))
	}
}

// Line única longa (JSON minificado, resposta de API, HTML): o tempo tem que crescer de forma
// linear com o tamanho. Cada item é fictício.
func longLine(typ string, size int) string {
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		switch typ {
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
		case "urlcolada": // URLs sem separador entre elas
			b.WriteString("https://exemplo.com/a")
		case "esquemas": // só o separador de esquema, repetido
			b.WriteString("a://")
		case "conexao":
			fmt.Fprintf(&b, `{"c":"Server=srv-x%d;Database=db_x;User Id=u_x;"},`, i%50)
		}
	}
	return b.String()
}

func benchLongLine(b *testing.B, typ string, size int) {
	s := longLine(typ, size)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, _ := NewMasker(newTest(&testing.T{}).cfg, testKey, nil, nil)
		m.Mask(s)
	}
}

func BenchmarkLineURL1MB(b *testing.B)         { benchLongLine(b, "url", 1<<20) }
func BenchmarkLineURL512K(b *testing.B)        { benchLongLine(b, "url", 512<<10) }
func BenchmarkLineSQLJSON512K(b *testing.B)    { benchLongLine(b, "sqljson", 512<<10) }
func BenchmarkLineURN512K(b *testing.B)        { benchLongLine(b, "urn", 512<<10) }
func BenchmarkLineEmail512K(b *testing.B)      { benchLongLine(b, "email", 512<<10) }
func BenchmarkLineK8sDNS512K(b *testing.B)     { benchLongLine(b, "k8sdns", 512<<10) }
func BenchmarkLineConnection512K(b *testing.B) { benchLongLine(b, "conexao", 512<<10) }
func BenchmarkLineGluedURL256K(b *testing.B)   { benchLongLine(b, "urlcolada", 256<<10) }
func BenchmarkLineSchemes1MB(b *testing.B)     { benchLongLine(b, "esquemas", 1<<20) }
