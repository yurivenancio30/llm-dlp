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

// Support commands: import-people, test, test-media, measure, columns.

func importPeople(args []string) error {
	fs := flag.NewFlagSet("import-people", flag.ExitOnError)
	var groups multi
	fs.Var(&groups, "group", "COD:NOME:EMAIL (colunas da mesma pessoa; qualquer uma pode ficar vazia)")
	fs.Var(&groups, "grupo", "nome antigo de --group")
	sep := fs.String("name-separator", "", `o nome vem como "PESSOA/ÁREA": só a parte antes deste separador é o nome`)
	fs.StringVar(sep, "separador-nome", "", "nome antigo de --name-separator")
	if len(args) == 0 {
		return errors.New("informe o arquivo CSV")
	}
	file := args[0]
	fs.Parse(args[1:])
	if len(groups) == 0 {
		return errors.New("informe ao menos um --group")
	}
	_, key, people, _, _, err := loadAll()
	if err != nil {
		return err
	}
	p := mask.NewPseudo(key)
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	hdr, err := r.Read()
	if err != nil {
		return err
	}
	col := map[string]int{}
	for i, h := range hdr {
		col[strings.TrimPrefix(strings.TrimSpace(h), "\uFEFF")] = i
	}
	takes := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	before, lines := people.Total(), 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		lines++
		for _, g := range groups {
			c := strings.SplitN(g+"::", ":", 4)
			name := takes(row, c[1])
			if *sep != "" {
				name = strings.TrimSpace(strings.SplitN(name, *sep, 2)[0])
			}
			people.Import(p, name, takes(row, c[2]), takes(row, c[0]))
		}
	}
	if err := people.Save(); err != nil {
		return err
	}
	// what already went out to the API stays frozen (see memory_sent.go); with new names, the
	// history is masked again, with them
	os.Remove(config.Path("enviados.log"))
	fmt.Printf("%d linhas lidas; variantes de nome conhecidas: %d (antes: %d). Nada em texto puro foi gravado.\n",
		lines, people.Total(), before)
	// the proxy reads the registry on start: restart it so the new names apply right away
	if cfg, err := config.Load(); err == nil && healthy(cfg) {
		if err := stop(); err == nil {
			if err := ensure(); err != nil {
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

func test() error {
	_, _, _, _, m, err := loadAll()
	if err != nil {
		return err
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	out, ents := m.Mask(string(b))
	fmt.Print(out)
	types := map[string]int{}
	for _, e := range ents {
		types[e.Type]++
	}
	fmt.Fprintf(os.Stderr, "\n[llm-dlp] %d substituições: %v\n", len(ents), types)
	return nil
}

// testMedia processes an image/PDF as the proxy would and writes what would go out into DIR.
func testMedia(args []string) error {
	if len(args) < 2 {
		return errors.New("uso: llm-dlp test-media ARQUIVO DIR_SAIDA")
	}
	cfg, _, _, _, m, err := loadAll()
	if err != nil {
		return err
	}
	bin, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	typ := http.DetectContentType(bin)
	if strings.HasSuffix(strings.ToLower(args[0]), ".pdf") {
		typ = "application/pdf"
	}
	block := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": typ,
		"data": base64.StdEncoding.EncodeToString(bin)}}
	if typ == "application/pdf" {
		block["type"] = "document"
	}
	md := proxy.NewMedia(cfg, m)
	md.Warning = func(msg string) { fmt.Fprintln(os.Stderr, "  aviso:", msg) }
	t0 := time.Now()
	blocks, ents, err := md.Process(block)
	cold := time.Since(t0)
	if err != nil {
		return err
	}
	t1 := time.Now()
	md.Process(block)
	hot := time.Since(t1)
	os.MkdirAll(args[1], 0o700)
	for i, b := range blocks {
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
	fmt.Printf("  %d pseudônimos | 1ª vez: %s | repetido (memória): %s\n", len(ents), cold.Round(time.Millisecond), hot.Round(time.Microsecond))
	return nil
}

// measure: applies the masker to what Claude would send (your prompts and tool outputs).
func measure(args []string) error {
	_, _, _, _, m, err := loadAll()
	if err != nil {
		return err
	}
	for _, file := range args {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 256<<20)
		var n, mod, bad int
		var timings []time.Duration
		types := map[string]int{}
		for sc.Scan() {
			var e map[string]any
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				bad++ // invalid line in the transcript: skip and go on (before, it stopped here)
				continue
			}
			msg, _ := e["message"].(map[string]any)
			for _, t := range texts(msg["content"]) {
				n++
				t0 := time.Now()
				_, ents := m.Mask(t)
				timings = append(timings, time.Since(t0))
				if len(ents) > 0 {
					mod++
				}
				for _, en := range ents {
					types[en.Type]++
				}
			}
		}
		f.Close()
		sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
		q := func(p float64) time.Duration {
			if len(timings) == 0 {
				return 0
			}
			return timings[int(float64(len(timings)-1)*p)]
		}
		var sum time.Duration
		for _, t := range timings {
			sum += t
		}
		fmt.Printf("### %s: %d textos (%d linhas inválidas puladas) | mascarados: %d | ocorrências por tipo: %v\n   tempo: mediana %s | p95 %s | máx %s | soma %s\n",
			filepath.Base(file)[:8], n, bad, mod, types, q(.5), q(.95), q(1), sum.Round(time.Millisecond))
	}
	return nil
}

func texts(c any) []string {
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
					out = append(out, texts(bl["content"])...)
				}
			}
		}
		return out
	}
	return nil
}

// columns shows how llm-dlp understands the field names of a file (the header of a CSV, the keys of
// a JSON): what will be masked and what it does not recognize. It is for finding out what to add to
// extra_fields in config.json.
func columns(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: llm-dlp columns ARQUIVO...")
	}
	_, _, _, _, m, err := loadAll()
	if err != nil {
		return err
	}
	names := map[string]string{"name": "nome de pessoa", "name_full": "nome de pessoa (só valor com nome e sobrenome)",
		"name_loose": "nome de pessoa (se a tabela tiver outra coluna de pessoa)", "user": "código/login de usuário",
		"doc": "documento", "sensitive": "dado pessoal sensível", "quasi": "quase identificador", "address": "endereço",
		"birthdate": "data de nascimento", "phone": "phone", "account": "conta bancária", "card": "cartão",
		"email": "e-mail (pelo formato do valor)"}
	for _, file := range args {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		buf := make([]byte, 256<<10)
		n, _ := io.ReadFull(f, buf)
		f.Close()
		cols := m.DescribeFields(string(buf[:n]))
		fmt.Printf("%s: %d campos\n", file, len(cols))
		negated := 0
		for _, c := range cols {
			switch {
			case c.Class == "":
				negated++
				fmt.Printf("  %-34s não reconhecido como dado pessoal (fica como está)\n", c.Name)
			case names[c.Class] != "":
				fmt.Printf("  %-34s MASCARADO: %s\n", c.Name, names[c.Class])
			default:
				fmt.Printf("  %-34s MASCARADO: %s\n", c.Name, strings.ToUpper(c.Class))
			}
		}
		if negated > 0 {
			fmt.Println("\nSe algum campo não reconhecido guarda dado pessoal, acrescente a palavra dele em extra_fields no config.json, por exemplo:")
			fmt.Println(`  "extra_fields": [{"type": "usuario", "words": ["chapa"]}, {"type": "pessoa", "words": ["segurado"]}]`)
			fmt.Println("Valores com formato próprio (e-mail, CPF com pontos, telefone, IP) são mascarados em qualquer campo.")
		}
	}
	return nil
}
