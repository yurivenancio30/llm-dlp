package main

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Nomes de objeto aprendidos (vistos.json): contar e esquecer. Só contagens, nunca valores.

func dataDias(s string) (int, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, fmt.Errorf("data %q: use AAAA-MM-DD", s)
	}
	return int(t.Unix() / 86400), nil
}

func diaTexto(d int) string { return time.Unix(int64(d)*86400, 0).UTC().Format("2006-01-02") }

func aprendidos() error {
	vistos, err := mask.CarregarVistos(config.Caminho("vistos.json"))
	if err != nil {
		return err
	}
	porTipo, porMes := map[string]int{}, map[string]int{}
	total, antigos := 0, 0
	hoje := int(time.Now().Unix() / 86400)
	vistos.Objetos(func(ent string, aprendido, visto int) {
		total++
		porTipo[ent]++
		porMes[diaTexto(aprendido)[:7]]++
		if hoje-visto > 90 {
			antigos++
		}
	})
	fmt.Printf("nomes de objeto aprendidos: %d (não vistos há mais de 90 dias, já sem propagar: %d)\n", total, antigos)
	ks := make([]string, 0, len(porTipo))
	for k := range porTipo {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("  %-10s %d\n", k, porTipo[k])
	}
	ms := make([]string, 0, len(porMes))
	for k := range porMes {
		ms = append(ms, k)
	}
	sort.Strings(ms)
	if len(ms) > 0 {
		fmt.Println("aprendidos por mês:")
		for _, k := range ms {
			fmt.Printf("  %s  %d\n", k, porMes[k])
		}
	}
	return nil
}

// esquecer apaga nomes aprendidos. Só com o proxy parado: o proxy tem os nomes também na
// memória e regravaria o arquivo; e assim o modelo, que só roda com o proxy no ar, não
// consegue apagar nada.
func esquecer(args []string) error {
	fs := flag.NewFlagSet("esquecer", flag.ContinueOnError)
	tipo := fs.String("tipo", "", "só deste tipo (tabela, coluna, servidor...)")
	desde := fs.String("desde", "", "só os aprendidos a partir desta data (AAAA-MM-DD)")
	ate := fs.String("ate", "", "só os aprendidos até esta data (AAAA-MM-DD)")
	tudo := fs.Bool("tudo", false, "todos os nomes de objeto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	nome := fs.Arg(0)
	if nome == "" && *tipo == "" && *desde == "" && *ate == "" && !*tudo {
		return errors.New("diga o que esquecer: NOME, --tipo T, --desde D, --ate D ou --tudo")
	}
	cfg, err := config.Carregar()
	if err != nil {
		return err
	}
	if saudavel(cfg) {
		return errors.New("o proxy está no ar: feche o Claude Code, rode llm-dlp parar e tente de novo")
	}
	d0, d1 := 0, 1<<30
	if *desde != "" {
		if d0, err = dataDias(*desde); err != nil {
			return err
		}
	}
	if *ate != "" {
		if d1, err = dataDias(*ate); err != nil {
			return err
		}
	}
	_, _, _, vistos, m, err := carregarTudo()
	if err != nil {
		return err
	}
	id := ""
	if nome != "" {
		id = m.IdObjeto(nome)
	}
	n := vistos.Esquecer(id, func(ent string, aprendido, _ int) bool {
		return (*tipo == "" || ent == *tipo) && aprendido >= d0 && aprendido <= d1
	})
	if err := vistos.Salvar(); err != nil {
		return err
	}
	fmt.Printf("%d nome(s) esquecido(s).\n", n)
	return nil
}
