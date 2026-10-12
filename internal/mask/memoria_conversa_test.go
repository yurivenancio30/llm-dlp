package mask

import (
	"crypto/sha256"
	"strings"
	"testing"
)

// Memória da conversa (memoria_conversa.go): nomes inventados, montados por partes.

var (
	nsComum  = "pag" + "amentos" // namespace de palavra comum
	tabComum = "fat" + "uras"    // tabela de palavra comum
)

func posTeste(s string) Posicao { return sha256.Sum256([]byte("pos\x00" + s)) }

// lote como o proxy usa: aquece, monta a memória e mascara em ordem
func loteCom(m *Masker, textos ...string) (*Lote, []string, []Entrada) {
	l := m.NovoLote()
	itens := make([]ItemLote, len(textos))
	for i, s := range textos {
		itens[i] = ItemLote{S: s, Pos: posTeste(s)}
	}
	l.Aquecer(itens)
	l.Memoria(itens, nil)
	var outs []string
	var ents []Entrada
	for _, it := range itens {
		o, e := l.MascararDica(it.S, false, it.Pos, "")
		outs = append(outs, o)
		ents = append(ents, e...)
	}
	return l, outs, ents
}

func TestMemoriaPalavraInteiraECaixa(t *testing.T) {
	m := novoTeste(t)
	inv := "kubectl logs -n " + nsComum + " deploy/web --tail 10"
	sql := "SELECT valor FROM " + tabComum + " WHERE id = 1 AND x = 2"
	prosa := "o " + nsComum + " caiu; " + strings.ToUpper(nsComum) + " é outro; " + nsComum + "X não; " +
		"e a tabela " + strings.ToUpper(tabComum) + " cresceu, " + tabComum + "_old não"
	_, outs, ents := loteCom(m, inv, sql, prosa)
	p := outs[2]
	if strings.Contains(p, " "+nsComum+" ") {
		t.Errorf("namespace solto ficou em claro: %s", p)
	}
	if !strings.Contains(p, strings.ToUpper(nsComum)+" é") {
		t.Errorf("namespace é exato: a grafia em maiúsculas é outra palavra: %s", p)
	}
	if !strings.Contains(p, nsComum+"X") || !strings.Contains(p, tabComum+"_old") {
		t.Errorf("palavra inteira: pedaço de outra palavra foi trocado: %s", p)
	}
	if strings.Contains(p, strings.ToUpper(tabComum)) {
		t.Errorf("tabela é sem caixa: a grafia em maiúsculas devia sair mascarada: %s", p)
	}
	if v := NovaTabela(ents).Desmascarar(p, false); v != prosa {
		t.Fatalf("ida e volta: %q", v)
	}
}

// Sem o inventário no pedido (conversa nova), a palavra comum fica (no nível do lote).
func TestMemoriaSoNoPedido(t *testing.T) {
	m := novoTeste(t)
	loteCom(m, "kubectl logs -n "+nsComum+" deploy/web --tail 10", "o "+nsComum+" caiu")
	_, outs, _ := loteCom(m, "o "+nsComum+" caiu de novo")
	if !strings.Contains(outs[0], nsComum) {
		t.Fatalf("outra conversa: a palavra comum foi mascarada: %s", outs[0])
	}
}

// Palavra genérica (referência pública) é mascarada onde foi decidida, mas não entra na
// memória (D4).
func TestMemoriaGenericaFora(t *testing.T) {
	ant := refPublica
	defer func() { refPublica = ant }()
	refPublica = map[string]bool{nsComum: true}
	m := novoTeste(t)
	_, outs, _ := loteCom(m, "kubectl logs -n "+nsComum+" deploy/web --tail 10", "o "+nsComum+" caiu")
	if !strings.Contains(outs[1], nsComum) {
		t.Fatalf("genérica entrou na memória: %s", outs[1])
	}
}

// Texto já enviado (congelado) sai igual, mesmo que a memória agora conheça o nome.
func TestMemoriaNaoReescreveOPassado(t *testing.T) {
	m := novoTeste(t)
	velho := "o " + nsComum + " caiu ontem"
	l1, o1, _ := loteCom(m, velho)
	l1.Congelar()
	inv := "kubectl logs -n " + nsComum + " deploy/web --tail 10"
	l2, o2, _ := loteCom(m, velho, inv, "e o "+nsComum+" hoje")
	if o2[0] != o1[0] || !strings.Contains(o2[0], nsComum) {
		t.Fatalf("texto enviado mudou: %q -> %q", o1[0], o2[0])
	}
	if strings.Contains(o2[2], nsComum) {
		t.Fatalf("texto novo sem a memória: %s", o2[2])
	}
	// o congelamento grava o texto com a memória aplicada: o reenvio sai igual
	l2.Congelar()
	_, o3, _ := loteCom(m, velho, inv, "e o "+nsComum+" hoje")
	for i := range o3 {
		if o3[i] != o2[i] {
			t.Fatalf("reenvio mudou o texto %d: %q -> %q", i, o2[i], o3[i])
		}
	}
}

// Quem escreveu: o texto do assistente volta como a API o mandou; só o trecho traduzido é
// pseudônimo, a mesma palavra escrita pelo modelo fica (no nível do lote); a palavra
// traduzida entra na memória.
func TestMemoriaQuemEscreveu(t *testing.T) {
	m := novoTeste(t)
	inv := "kubectl logs -n " + nsComum + " deploy/web --tail 10"
	_, outs, ents := loteCom(m, inv)
	tab := NovaTabela(ents)
	ps := ""
	for _, e := range ents {
		if e.Real == nsComum && casePrincipal(e.Pseudo, e.Real, e.Tipo) {
			ps = e.Pseudo
		}
	}
	if ps == "" || strings.Contains(outs[0], nsComum) {
		t.Fatalf("premissa: o inventário não mascarou: %s", outs[0])
	}
	orig := "o namespace " + ps + " tem o serviço " + nsComum + " do modelo"
	des := m.RegistrarResposta(orig, tab)
	l := m.NovoLote()
	d, ok := l.Escrito(des)
	if !ok || len(d) != 1 || d[0].Nome != nsComum || d[0].Ent != "namespace" || d[0].Regra != "traduzida" {
		t.Fatalf("decisões traduzidas: %+v %v", d, ok)
	}
	saida := "logs de " + nsComum + " ok"
	itens := []ItemLote{{S: saida, Pos: posTeste(saida)}}
	l.Aquecer(itens)
	l.Memoria(itens, d)
	o, _ := l.MascararEscrito(des, false, posTeste(des), "")
	if o != orig {
		t.Fatalf("texto do assistente: %q, esperado %q", o, orig)
	}
	if so, _ := l.MascararDica(saida, false, posTeste(saida), ""); strings.Contains(so, nsComum) {
		t.Fatalf("palavra traduzida não entrou na memória: %s", so)
	}
}

// O registro de quem escreveu sobrevive a um reinício (enviados.log, só HMAC e pseudônimos).
func TestQuemEscreveuEmDisco(t *testing.T) {
	dir := t.TempDir()
	m := novoTeste(t)
	if err := m.UsarEnviados(dir+"/e.log", "x"); err != nil {
		t.Fatal(err)
	}
	_, ents := m.Mascarar("kubectl logs -n " + nsComum + " deploy/web --tail 10")
	tab := NovaTabela(ents)
	ps := ""
	for _, e := range ents {
		if e.Real == nsComum && casePrincipal(e.Pseudo, e.Real, e.Tipo) {
			ps = e.Pseudo
		}
	}
	orig := "veja " + ps + " e " + nsComum
	des := m.RegistrarResposta(orig, tab)
	m.Persistir()
	m2 := novoTeste(t)
	if err := m2.UsarEnviados(dir+"/e.log", "x"); err != nil {
		t.Fatal(err)
	}
	if o, ok := m2.OriginalDe(des); !ok || o != orig {
		t.Fatalf("depois do reinício: %q %v", o, ok)
	}
	tr, ok := m2.Traduzidas(des)
	if !ok || len(tr) != 1 || tr[0].Tipo != prefTipoObj+"namespace" {
		t.Fatalf("Traduzidas depois do reinício: %+v", tr)
	}
}

// A memória varre cada texto uma vez (uma consulta no índice por palavra): o custo cresce com
// o tamanho do texto, não com o número de nomes.
func BenchmarkMemoriaVarrer1MB(b *testing.B) {
	m := novoTeste(b)
	var decs []Decisao
	for i := 0; i < 2000; i++ {
		decs = append(decs, Decisao{Nome: "nome" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)), Ent: []string{"namespace", "tabela"}[i%2]})
	}
	mm := m.novaMemoria(decs)
	linha := "o servico nomexab respondeu em 12ms para Tabela NOMEXXBC; outra palavra comum aqui e ali\n"
	s := strings.Repeat(linha, (1<<20)/len(linha))
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm.varrer(s, nil)
	}
}

// O marcador de comentário só vale fora de aspas; "--x" colado numa letra é opção de comando e
// "#3" é numeração.
func TestEmComentario(t *testing.T) {
	casos := map[string]bool{
		"# carrega as faturas":                    true,
		"select 1 -- faturas":                     true,
		"x = 1 // faturas":                        true,
		`print("a # b") # faturas`:                true,
		"pg_dump --schema=faturas":                false,
		`rotulo = "lote #3: faturas"`:             false,
		`msg = "total // faturas"`:                false,
		"ver o item #3 faturas":                   false,
		"SELECT '--' || faturas FROM t":           false,
		"echo 'a -- b' faturas":                   false,
		"# comentário com 'aspas' antes: faturas": true,
	}
	for s, quer := range casos {
		if got := emComentario(s, strings.LastIndex(s, "faturas")); got != quer {
			t.Errorf("%q: emComentario=%v, esperado %v", s, got, quer)
		}
	}
}

// Procedência: num dado (saída de ferramenta, arquivo lido), o nome de recurso que a requisição
// provou continua nome dentro de um comentário; na mensagem do usuário, o comentário colado
// continua fora do alcance da memória, como antes.
func TestProcedenciaEmComentarioDoDado(t *testing.T) {
	m := novoTeste(t)
	sql := "SELECT valor FROM " + tabComum + " WHERE id = 1 AND x = 2"
	arquivo := "def carregar():\n    # carrega as " + tabComum + " do dia\n    return 1\n"
	l, outs, _ := loteCom(m, sql, arquivo)
	if strings.Contains(outs[1], tabComum) {
		t.Fatalf("o nome provado no SQL ficou em claro no comentário do arquivo: %s", outs[1])
	}
	colado := "olha isto:\n# carrega as " + tabComum + " do dia"
	if o, _ := l.MascararDicaProsa(colado, false, posTeste(colado), ""); !strings.Contains(o, tabComum) {
		t.Fatalf("na mensagem do usuário o comentário devia ficar como antes: %s", o)
	}
}

// Arquivo de documentação (README, prompt de agente) é prosa fora dos blocos de código: a
// palavra comum que a memória traz fica como palavra; no bloco de código e num arquivo de
// código, a procedência vale.
func TestDocumentoEProsaForaDoCodigo(t *testing.T) {
	tab := "pro" + "postas" // tabela de palavra comum (está na lista)
	if !palavraComum(tab) {
		t.Fatalf("premissa: %q devia ser palavra comum", tab)
	}
	m := novoTeste(t)
	sql := "SELECT valor FROM " + tab + " WHERE id = 1 AND x = 2"
	doc := "# Guia\n\nO time revisa as " + tab + " toda semana.\n\n```sql\nselect * from " + tab + " -- as " + tab + " do dia\n```\n"
	codigo := "def carregar():\n    # carrega as " + tab + " do dia\n    return 1\n"
	itens := []ItemLote{
		{S: sql, Pos: posTeste(sql)},
		{S: doc, Pos: posTeste(doc), Dica: ComFonte("readme.md", "")},
		{S: codigo, Pos: posTeste(codigo), Dica: ComFonte("carga.py", "")},
	}
	l := m.NovoLote()
	l.Aquecer(itens)
	l.Memoria(itens, nil)
	var outs []string
	for _, it := range itens {
		o, _ := l.MascararDica(it.S, false, it.Pos, it.Dica)
		outs = append(outs, o)
	}
	if !strings.Contains(outs[1], "revisa as "+tab+" toda") {
		t.Errorf("na prosa do README a palavra comum devia ficar: %s", outs[1])
	}
	if strings.Contains(outs[1], "from "+tab) || strings.Contains(outs[1], "-- as "+tab) {
		t.Errorf("no bloco de código do README o nome devia ser mascarado: %s", outs[1])
	}
	if strings.Contains(outs[2], tab) {
		t.Errorf("no arquivo de código o nome devia ser mascarado: %s", outs[2])
	}
}
