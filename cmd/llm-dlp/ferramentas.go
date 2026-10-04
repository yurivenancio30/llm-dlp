package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
)

// Comandos de apoio: importar-pessoas, testar, testar-midia, medir, colunas.

func importarPessoas(args []string) error {
	fs := flag.NewFlagSet("importar-pessoas", flag.ExitOnError)
	var grupos multi
	fs.Var(&grupos, "grupo", "COD:NOME:EMAIL (colunas da mesma pessoa; qualquer uma pode ficar vazia)")
	sep := fs.String("separador-nome", "", `o nome vem como "PESSOA/ÁREA": só a parte antes deste separador é o nome`)
	if len(args) == 0 {
		return errors.New("informe o arquivo CSV")
	}
	arq := args[0]
	fs.Parse(args[1:])
	if len(grupos) == 0 {
		return errors.New("informe ao menos um --grupo")
	}
	_, chave, pessoas, _, _, err := carregarTudo()
	if err != nil {
		return err
	}
	p := mask.NovoPseudo(chave)
	f, err := os.Open(arq)
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	cab, err := r.Read()
	if err != nil {
		return err
	}
	col := map[string]int{}
	for i, h := range cab {
		col[strings.TrimPrefix(strings.TrimSpace(h), "\uFEFF")] = i
	}
	pega := func(row []string, nome string) string {
		if i, ok := col[nome]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	antes, linhas := pessoas.Total(), 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		linhas++
		for _, g := range grupos {
			c := strings.SplitN(g+"::", ":", 4)
			nome := pega(row, c[1])
			if *sep != "" {
				nome = strings.TrimSpace(strings.SplitN(nome, *sep, 2)[0])
			}
			pessoas.Importar(p, nome, pega(row, c[2]), pega(row, c[0]))
		}
	}
	if err := pessoas.Salvar(); err != nil {
		return err
	}
	// o que já saiu à API fica congelado (ver enviados.go); com nomes novos, o histórico
	// passa a ser mascarado de novo, com eles
	os.Remove(config.Caminho("enviados.log"))
	fmt.Printf("%d linhas lidas; variantes de nome conhecidas: %d (antes: %d). Nada em texto puro foi gravado.\n",
		linhas, pessoas.Total(), antes)
	// o proxy lê o registro ao iniciar: reinicia para os nomes novos valerem já
	if cfg, err := config.Carregar(); err == nil && saudavel(cfg) {
		if err := parar(); err == nil {
			if err := garantir(); err != nil {
				return fmt.Errorf("nomes importados, mas o proxy não voltou: %w", err)
			}
			fmt.Println("proxy reiniciado para usar os nomes novos.")
		}
	}
	return nil
}

type multi []string

func (m *multi) String() string { return strings.Join(*m, ",") }

func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func testar() error {
	_, _, _, _, m, err := carregarTudo()
	if err != nil {
		return err
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	out, ents := m.Mascarar(string(b))
	fmt.Print(out)
	tipos := map[string]int{}
	for _, e := range ents {
		tipos[e.Tipo]++
	}
	fmt.Fprintf(os.Stderr, "\n[llm-dlp] %d substituições: %v\n", len(ents), tipos)
	return nil
}

// testarMidia processa uma imagem/PDF como o proxy faria e grava o que sairia em DIR.
func testarMidia(args []string) error {
	if len(args) < 2 {
		return errors.New("uso: llm-dlp testar-midia ARQUIVO DIR_SAIDA")
	}
	cfg, _, _, _, m, err := carregarTudo()
	if err != nil {
		return err
	}
	bin, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	tipo := http.DetectContentType(bin)
	if strings.HasSuffix(strings.ToLower(args[0]), ".pdf") {
		tipo = "application/pdf"
	}
	bloco := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": tipo,
		"data": base64.StdEncoding.EncodeToString(bin)}}
	if tipo == "application/pdf" {
		bloco["type"] = "document"
	}
	md := proxy.NovaMidia(cfg, m)
	t0 := time.Now()
	blocos, ents, err := md.Processar(bloco)
	frio := time.Since(t0)
	if err != nil {
		return err
	}
	t1 := time.Now()
	md.Processar(bloco)
	quente := time.Since(t1)
	os.MkdirAll(args[1], 0o700)
	for i, b := range blocos {
		bl := b.(map[string]any)
		src, _ := bl["source"].(map[string]any)
		switch {
		case bl["type"] == "text":
			fmt.Printf("  bloco %d: nota: %s\n", i, bl["text"])
		case src != nil && src["type"] == "text":
			os.WriteFile(filepath.Join(args[1], fmt.Sprintf("bloco%d.txt", i)), []byte(src["data"].(string)), 0o600)
			fmt.Printf("  bloco %d: texto extraído e mascarado (bloco%d.txt)\n", i, i)
		case src != nil && src["type"] == "base64":
			d, _ := base64.StdEncoding.DecodeString(src["data"].(string))
			ext := ".png"
			if src["media_type"] == "application/pdf" {
				ext = ".pdf"
			}
			os.WriteFile(filepath.Join(args[1], fmt.Sprintf("bloco%d%s", i, ext)), d, 0o600)
			fmt.Printf("  bloco %d: %s (bloco%d%s)\n", i, src["media_type"], i, ext)
		}
	}
	fmt.Printf("  %d pseudônimos | 1ª vez: %s | repetido (memória): %s\n", len(ents), frio.Round(time.Millisecond), quente.Round(time.Microsecond))
	return nil
}

// medir: aplica o mascarador ao que o Claude enviaria (seus prompts e saídas de ferramenta).
func medir(args []string) error {
	_, _, _, _, m, err := carregarTudo()
	if err != nil {
		return err
	}
	for _, arq := range args {
		f, err := os.Open(arq)
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 256<<20)
		var n, mod, ruins int
		var tempos []time.Duration
		tipos := map[string]int{}
		for sc.Scan() {
			var e map[string]any
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				ruins++ // linha inválida no transcript: pula e segue (antes parava aqui)
				continue
			}
			msg, _ := e["message"].(map[string]any)
			for _, t := range textos(msg["content"]) {
				n++
				t0 := time.Now()
				_, ents := m.Mascarar(t)
				tempos = append(tempos, time.Since(t0))
				if len(ents) > 0 {
					mod++
				}
				for _, en := range ents {
					tipos[en.Tipo]++
				}
			}
		}
		f.Close()
		sort.Slice(tempos, func(i, j int) bool { return tempos[i] < tempos[j] })
		q := func(p float64) time.Duration {
			if len(tempos) == 0 {
				return 0
			}
			return tempos[int(float64(len(tempos)-1)*p)]
		}
		var soma time.Duration
		for _, t := range tempos {
			soma += t
		}
		fmt.Printf("### %s: %d textos (%d linhas inválidas puladas) | mascarados: %d | ocorrências por tipo: %v\n   tempo: mediana %s | p95 %s | máx %s | soma %s\n",
			filepath.Base(arq)[:8], n, ruins, mod, tipos, q(.5), q(.95), q(1), soma.Round(time.Millisecond))
	}
	return nil
}

func textos(c any) []string {
	switch x := c.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, b := range x {
			if bl, ok := b.(map[string]any); ok {
				switch bl["type"] {
				case "text":
					if s, ok := bl["text"].(string); ok {
						out = append(out, s)
					}
				case "tool_result":
					out = append(out, textos(bl["content"])...)
				}
			}
		}
		return out
	}
	return nil
}

// colunas mostra como o llm-dlp entende os nomes de campo de um arquivo (o cabeçalho de um
// CSV, as chaves de um JSON): o que será mascarado e o que ele não reconhece. Serve para
// descobrir o que acrescentar em campos_extras no config.json.
func colunas(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: llm-dlp colunas ARQUIVO...")
	}
	_, _, _, _, m, err := carregarTudo()
	if err != nil {
		return err
	}
	nomes := map[string]string{"nome": "nome de pessoa", "nomecompleto": "nome de pessoa (só valor com nome e sobrenome)",
		"nomesolto": "nome de pessoa (se a tabela tiver outra coluna de pessoa)", "usuario": "código/login de usuário",
		"doc": "documento", "sensivel": "dado pessoal sensível", "quase": "quase identificador", "endereco": "endereço",
		"nascimento": "data de nascimento", "telefone": "telefone", "conta": "conta bancária", "cartao": "cartão",
		"email": "e-mail (pelo formato do valor)"}
	for _, arq := range args {
		f, err := os.Open(arq)
		if err != nil {
			return err
		}
		buf := make([]byte, 256<<10)
		n, _ := io.ReadFull(f, buf)
		f.Close()
		cols := m.DescreverCampos(string(buf[:n]))
		fmt.Printf("%s: %d campos\n", arq, len(cols))
		nao := 0
		for _, c := range cols {
			switch {
			case c.Classe == "":
				nao++
				fmt.Printf("  %-34s não reconhecido como dado pessoal (fica como está)\n", c.Nome)
			case nomes[c.Classe] != "":
				fmt.Printf("  %-34s MASCARADO: %s\n", c.Nome, nomes[c.Classe])
			default:
				fmt.Printf("  %-34s MASCARADO: %s\n", c.Nome, strings.ToUpper(c.Classe))
			}
		}
		if nao > 0 {
			fmt.Println("\nSe algum campo não reconhecido guarda dado pessoal, acrescente a palavra dele em campos_extras no config.json, por exemplo:")
			fmt.Println(`  "campos_extras": [{"tipo": "usuario", "palavras": ["chapa"]}, {"tipo": "pessoa", "palavras": ["segurado"]}]`)
			fmt.Println("Valores com formato próprio (e-mail, CPF com pontos, telefone, IP) são mascarados em qualquer campo.")
		}
	}
	return nil
}
