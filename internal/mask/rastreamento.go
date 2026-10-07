package mask

import (
	"regexp"
	"sort"
	"strings"
)

// Rastreamento (ver docs/estruturas.md): cada nome do cliente é decidido onde ENTRA na conversa
// e, a partir daí, é mascarado em todo lugar. Três regras:
//
//  1. Entrada: só os dados (saída de comando, arquivo lido) e o texto do usuário trazem nome do
//     cliente. O texto que o assistente escreveu não é fonte: o modelo só conhece o cliente pelo
//     que o proxy deixou passar; palavra nova dele é conhecimento público (MascararContagio).
//  2. Contágio: o valor decidido vale em todas as ocorrências, em qualquer texto e formato.
//  3. Limpeza: vocabulário público nunca é marcado (publicoSistema, publicoGeral).
//
// Cada marca leva a origem: decidida com prova direta (leitor, decisor) ou deduzida (âncora por
// posição). Dedução não gera dedução (sem cascata), e só vale com sementes provadas na mesma
// fonte (o arquivo ou recurso que o comando leu): quatro nomes de tabela que coincidem com
// pacotes de um constraints.txt não fazem a lista inteira virar tabela. Palavra comum provada numa
// fonte não contamina outro arquivo lido; na conversa e nas saídas sem arquivo ela é o nome.

// adicionar: põe d na memória (sem repetir). Devolve se entrou.
func (mm *memoria) adicionar(m *Masker, d Decisao) bool {
	if d.Generica || len(d.Nome) < memMin || publicoGeral(d.Nome) || !m.objMascara(d.Ent) || ehPseudoObj(d.Nome) {
		return false
	}
	if mm.tem == nil {
		mm.tem = map[string]bool{}
	}
	k := d.Chave()
	if mm.tem[k] {
		return false
	}
	i := 0
	for i < len(d.Nome) && !ehPalavra(d.Nome[i]) {
		i++
	}
	j := i
	for j < len(d.Nome) && ehPalavra(d.Nome[j]) {
		j++
	}
	if i == j {
		return false
	}
	mm.tem[k] = true
	nm := nomeMem{nome: d.Nome, off: i, ent: d.Ent, semCaixa: entSQL[d.Ent]}
	idx, w := mm.exata, d.Nome[i:j]
	if nm.semCaixa {
		idx, w = mm.semCaixa, strings.ToLower(w)
		mm.tamSemCaixa |= bitTam(j - i)
	} else {
		mm.tamExata |= bitTam(j - i)
	}
	ns := append(idx[w], nm)
	sort.SliceStable(ns, func(a, b int) bool { return len(ns[a].nome) > len(ns[b].nome) })
	idx[w] = ns
	mm.n++
	return true
}

// aprenderAncora: os trechos novos de ts2 (fora de antes) são decisões de valor (regra 2):
// entram na memória e ficam marcados como deduzidos. Devolve as que entraram.
func (l *Lote) aprenderAncora(s string, antes, ts2 []trecho) []Decisao {
	ja := map[[2]int]bool{}
	for _, t := range antes {
		ja[[2]int{t.Ini, t.Fim}] = true
	}
	var out []Decisao
	for _, t := range ts2 {
		if ja[[2]int{t.Ini, t.Fim}] || !ehObjeto(t.Tipo) || t.Tipo == prefTipoObj+entGenerica {
			continue
		}
		v := s[t.Ini:t.Fim]
		if l.doModelo(v) {
			continue
		}
		d := Decisao{Nome: v, Ent: t.Tipo[len(prefTipoObj):], Regra: "âncora", Generica: ehGenerica(v)}
		l.marcarDeduzido(v)
		if l.mem.adicionar(l.m, d) {
			out = append(out, d)
		}
	}
	return out
}

func (l *Lote) marcarDeduzido(v string) {
	if l.deduzidos == nil {
		l.deduzidos = map[string]bool{}
	}
	l.deduzidos[strings.ToLower(v)] = true
}

func (l *Lote) deduzido(v string) bool { return l.deduzidos[strings.ToLower(v)] }

// MascararContagio (regra 1): texto que o assistente escreveu e que não bate com o registro do
// que o proxy traduziu. Só recebe o contágio: os detectores de formato (CPF, chave, e-mail:
// valem por si), os valores já marcados e a memória da conversa. Nenhum leitor de estrutura nem
// decisor roda aqui, e nada é aprendido.
func (l *Lote) MascararContagio(s string, daWeb bool, pos Posicao) (string, []Entrada) {
	if len(s) < 4 {
		return s, nil
	}
	if r, ok := l.m.congelado(pos, s); ok {
		return r.texto, r.entradas
	}
	out := l.m.detectarBase(s)
	numLongo, _ := perfilNumerico(s)
	l.m.acharConhecidos(s, numLongo, func(ini, fim int, tipo string) {
		out = append(out, Achado{ini, fim, tipo, s[ini:fim]})
	})
	l.m.unificarObjetos(out)
	texto, ents, ts := l.m.aplicarT(s, out)
	l.escrevendo, l.fonteAtual = true, ""
	r := l.comMemoria(s, resultado{texto: texto, entradas: ents, trechos: ts, decididos: []Decisao{}}, "")
	l.escrevendo = false
	if _, ja := l.saidas[pos]; !ja {
		l.saidas[pos] = r
	}
	return r.texto, r.entradas
}

// publicoSistema (regra 3): vocabulário de sistema (SQL, desenvolvimento, devops, bancos); nunca
// é mascarado, nem no lugar.
func publicoSistema(v string) bool {
	return publicoSQL(v) || publicoDev(v) || publicoDevops(v) || esquemasBanco[strings.ToLower(v)]
}

// publicoGeral: o de sistema e a referência medida (refPublica). A referência, com prova, é
// mascarada no lugar, mas não se espalha.
func publicoGeral(v string) bool { return publicoSistema(v) || refPublica[strings.ToLower(v)] }

// valorInteiro (só o valor inteiro é candidato): s[a:b] é o valor inteiro do seu token, e não um
// pedaço de uma expressão. Um literal entre aspas é um valor; fora disso, o token vai até espaço
// ou separador de campo (, ; | / : =), sem as aspas e os parênteses que o embrulham.
// "pagamentos" numa coluna é; "CPF" em '.*CPF.*' ou em (CPF|CNPJ) não é.
func valorInteiro(s string, a, b int) bool {
	if a > 0 && b < len(s) && s[a-1] == s[b] && strings.IndexByte("'\"`", s[b]) >= 0 {
		return true
	}
	sep := func(c byte) bool { return strings.IndexByte(" \t\r\n,;|/:=", c) >= 0 }
	i, j := a, b
	for i > 0 && !sep(s[i-1]) {
		i--
	}
	for j < len(s) && !sep(s[j]) {
		j++
	}
	for i < a && strings.IndexByte("'\"`([{", s[i]) >= 0 {
		i++
	}
	for j > b && strings.IndexByte("'\"`)]}.", s[j-1]) >= 0 {
		j--
	}
	return i == a && j == b
}

// contaForaAspas: quantos c há em l fora de aspas simples ou duplas (fechadas na linha).
func contaForaAspas(l string, c byte) int {
	n := 0
	var q byte
	for i := 0; i < len(l); i++ {
		switch {
		case q != 0:
			if l[i] == q {
				q = 0
			}
		case l[i] == '"' || l[i] == '\'':
			if strings.IndexByte(l[i+1:], l[i]) >= 0 {
				q = l[i]
			}
		case l[i] == c:
			n++
		}
	}
	return n
}

// semSobreporLongo: os trechos em ordem, sem sobreposição; no conflito fica o que cobre mais
// (nunca se descarta o resultado inteiro por causa de um conflito).
func semSobreporLongo(ts []trecho) []trecho {
	ordenarTrechos(ts)
	out := make([]trecho, 0, len(ts))
	for _, t := range ts {
		if n := len(out); n > 0 && t.Ini < out[n-1].Fim {
			if t.Fim-t.Ini > out[n-1].Fim-out[n-1].Ini {
				out[n-1] = t
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

// ultimoQueCobre: o último trecho (em ordem) que se sobrepõe a [a, b), ou -1.
func ultimoQueCobre(ts []trecho, a, b int) int {
	i := sort.Search(len(ts), func(k int) bool { return ts[k].Ini >= b })
	if k := i - 1; k >= 0 && ts[k].Fim > a {
		return k
	}
	return -1
}

// ---- fonte -------------------------------------------------------------------------------

// sepFonte: a fonte vai na frente da dica do resultado (ComFonte); o Lote a separa na entrada.
const sepFonte = "\x1d"

// ComFonte: a dica com a fonte (os arquivos ou o recurso do comando) na frente.
func ComFonte(fonte, dica string) string {
	if fonte == "" {
		return dica
	}
	return sepFonte + fonte + sepFonte + dica
}

// SepararFonte: o inverso de ComFonte.
func SepararFonte(d string) (fonte, dica string) {
	if !strings.HasPrefix(d, sepFonte) {
		return "", d
	}
	f, resto, _ := strings.Cut(d[len(sepFonte):], sepFonte)
	return f, resto
}

func semFontes(itens []ItemLote) []ItemLote {
	out := make([]ItemLote, len(itens))
	for i, it := range itens {
		_, it.Dica = SepararFonte(it.Dica)
		out[i] = it
	}
	return out
}

// registrarFontes: as decisões com prova direta de cada texto ficam ligadas à fonte dele.
func (l *Lote) registrarFontes(itens []ItemLote, fontes []string) {
	if l.fontes == nil {
		l.fontes = map[string]map[string]bool{}
	}
	for i, it := range itens {
		if fontes[i] == "" || it.DaWeb {
			continue
		}
		ds, _ := l.m.decididosMemo(it.S, it.Dica)
		for _, d := range ds {
			if d.Regra == "âncora" {
				continue
			}
			k := strings.ToLower(d.Nome)
			if l.fontes[k] == nil {
				l.fontes[k] = map[string]bool{}
			}
			for _, f := range strings.Split(fontes[i], ",") {
				l.fontes[k][f] = true
			}
		}
	}
}

// provadoNaFonte: v foi decidido com prova no próprio texto ou numa fonte do texto atual.
func (l *Lote) provadoNaFonte(v string) bool {
	k := strings.ToLower(v)
	if l.provAtual[k] {
		return true
	}
	if l.fonteAtual == "" {
		return false
	}
	for _, f := range strings.Split(l.fonteAtual, ",") {
		if l.fontes[k][f] {
			return true
		}
	}
	return false
}

var reFonteArq = regexp.MustCompile(`[\w~./\-]*[\w\-]\.[A-Za-z][\w]{0,5}\b`)

// FonteDoComando: os arquivos que o comando cita (nome base, minúsculas); sem arquivo, o
// programa e o primeiro argumento que não é opção ("kubectl get").
func FonteDoComando(cmd string) string {
	vis := map[string]bool{}
	var fs []string
	for _, a := range reFonteArq.FindAllString(cmd, 64) {
		b := strings.ToLower(a[strings.LastIndexAny(a, "/\\")+1:])
		if !vis[b] {
			vis[b] = true
			fs = append(fs, b)
		}
	}
	if len(fs) == 0 {
		var ws []string
		for _, w := range strings.Fields(cmd) {
			if !strings.HasPrefix(w, "-") {
				ws = append(ws, strings.ToLower(w))
			}
			if len(ws) == 2 {
				break
			}
		}
		if len(ws) > 0 {
			fs = append(fs, "cmd:"+strings.Join(ws, " "))
		}
	}
	sort.Strings(fs)
	return strings.Join(fs, ",")
}

// fonteArquivo: a fonte cita arquivo (e não só um comando sem arquivo, nem nada).
func fonteArquivo(f string) bool {
	for _, x := range strings.Split(f, ",") {
		if x != "" && !strings.HasPrefix(x, "cmd:") {
			return true
		}
	}
	return false
}

// FonteSistema: a fonte de uma saída que descreve ou lista um catálogo de sistema (DESC/SHOW em
// SNOWFLAKE.ACCOUNT_USAGE, information_schema...). A estrutura listada é documentação pública:
// a saída não é fonte de decisão (como o texto do assistente) e só recebe o contágio dos nomes
// do cliente já conhecidos.
const FonteSistema = "sistema"

// AlvoDeSistema: o comando descreve ou lista um objeto de catálogo de sistema.
func AlvoDeSistema(cmd string) bool { return alvoDeSistema(cmd) }

// UsarPublicos: as palavras (minúsculas) que o modelo escreveu antes de um dado trazê-las nesta
// conversa (anterioridade, no proxy). São conhecimento dele: nenhum caminho as marca aqui.
func (l *Lote) UsarPublicos(p map[string]bool) { l.publicos = p }

// doModelo: v é conhecimento do modelo nesta conversa (e não contém termo cadastrado: o
// cadastro sempre vence).
func (l *Lote) doModelo(v string) bool {
	if len(l.publicos) == 0 {
		return false
	}
	return l.publicos[strings.ToLower(v)] && !l.m.temTermo(v)
}

// semPublicos: os trechos de objeto cujo valor é conhecimento do modelo saem, e os de palavra
// comum na prosa do modelo (os de formato, como CPF e chave, ficam: valem por si).
func (l *Lote) semPublicos(s string, ts []trecho) []trecho {
	out := ts[:0:0]
	for _, t := range ts {
		if v := s[t.Ini:t.Fim]; ehObjeto(t.Tipo) && (l.doModelo(v) || l.prosa && l.m.comumLivre(v)) {
			continue
		}
		out = append(out, t)
	}
	return out
}
