package mask

import (
	"os"
	"strings"
	"testing"
)

// Senhas e rótulos montados por partes: o texto do teste não deve ensinar nada a um
// llm-dlp que esteja no caminho de quem edita o código.
var fracas = []string{"change" + "me", "ad" + "min", "gira" + "ssol"}

// ensina: a senha numa URL de conexão (onde até a só de letras é detectada)
// cpfTeste: CPF fictício válido
var cpfTeste = "529.982" + ".247-25"

func ensina(s string) string { return "mysql://app:" + s + "@db1:3306/base" }

func maskerEmDisco(t *testing.T, dir string) (*Masker, *Vistos) {
	t.Helper()
	vs, err := CarregarVistos(dir + "/vistos.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UsarEnviados(dir+"/enviados.log", "teste"); err != nil {
		t.Fatal(err)
	}
	return m, vs
}

// (a) Senha fraca, só de letras ou com cara de exemplo, dita pelo usuário ou lida de um
// arquivo local, é lembrada: aparece depois sem rótulo e é mascarada.
func TestSenhaFracaELembrada(t *testing.T) {
	for _, origem := range []string{"o usuário digitou: " + ensina("%s"), "# .env lido do disco\nDATABASE_URL=" + ensina("%s") + "\n"} {
		m := novoTeste(t)
		for _, s := range fracas {
			m.Mascarar(strings.Replace(origem, "%s", s, 1))
			if out, _ := m.Mascarar("tentei " + s + " e falhou"); strings.Contains(out, s) {
				t.Errorf("senha fraca não foi lembrada (origem %q): %q", origem[:8], out)
			}
		}
	}
}

// (b) A mesma palavra, como senha de exemplo numa URL de um resultado de WebFetch: é mascarada
// ali, mas não é lembrada nem gravada em vistos.json.
func TestConteudoDaWebNaoEnsina(t *testing.T) {
	dir := t.TempDir()
	m, vs := maskerEmDisco(t, dir)
	ex := fracas[0]
	pagina := "Exemplo da documentação: mysql://app:" + ex + "@db.example.com:3306/base"
	out, _ := m.NovoLote().Mascarar(pagina, true)
	if strings.Contains(out, ex) {
		t.Fatalf("na própria página, a senha de exemplo deveria ser mascarada: %q", out)
	}
	solta := "a palavra " + ex + " aparece em outro texto"
	if out, _ := m.Mascarar(solta); out != solta {
		t.Errorf("valor vindo da web foi lembrado: %q", out)
	}
	if _, ok := vs.Tipo(m.p.ID("visto", "s:"+ex)); ok {
		t.Errorf("valor vindo da web foi gravado em vistos.json")
	}
	vs.SalvarSeSujo()
	m2, _ := maskerEmDisco(t, dir)
	if out, _ := m2.Mascarar(solta); out != solta {
		t.Errorf("após reinício, valor vindo da web é mascarado: %q", out)
	}
	// o mesmo texto vindo de fonte local ensina normalmente
	m.Mascarar(pagina)
	if out, _ := m.Mascarar(solta); out == solta {
		t.Errorf("o mesmo texto, de fonte local, deveria ensinar")
	}
}

// (c) Texto que já saiu sai igual depois, mesmo que um valor dele seja aprendido mais
// tarde, e também depois de um reinício (memo vazio). Texto novo usa o valor aprendido.
func TestTextoEnviadoFicaCongelado(t *testing.T) {
	dir := t.TempDir()
	m, vs := maskerEmDisco(t, dir)
	s := fracas[2]
	antigo := "tentei " + s + " no login e não entrou; CPF " + cpfTeste
	l := m.NovoLote()
	enviado, _ := l.Mascarar(antigo, false)
	if !strings.Contains(enviado, s) {
		t.Skip("premissa mudou: a palavra já é mascarada sem rótulo")
	}
	l.Congelar()

	m.Mascarar(ensina(s)) // aprende depois
	if out, _ := m.NovoLote().Mascarar(antigo, false); out != enviado {
		t.Fatalf("texto já enviado mudou depois de aprender um valor:\nantes:  %q\ndepois: %q", enviado, out)
	}
	if out, _ := m.Mascarar("de novo: " + s); strings.Contains(out, s) {
		t.Fatalf("texto novo não usou o valor aprendido: %q", out)
	}

	m.Persistir()
	vs.SalvarSeSujo()
	m2, _ := maskerEmDisco(t, dir) // reinício
	out, ents := m2.NovoLote().Mascarar(antigo, false)
	if out != enviado {
		t.Fatalf("após reinício, o texto enviado mudou:\nantes:  %q\ndepois: %q", enviado, out)
	}
	if len(ents) == 0 || ents[0].Real != cpfTeste {
		t.Errorf("após reinício, faltou a entrada para desmascarar a resposta: %+v", ents)
	}
	if out, _ := m2.Mascarar("outra vez: " + s); strings.Contains(out, s) {
		t.Errorf("após reinício, texto novo não usou o valor aprendido: %q", out)
	}
	if b, _ := os.ReadFile(dir + "/enviados.log"); strings.Contains(string(b), s) || strings.Contains(string(b), cpfTeste) {
		t.Errorf("valor real gravado em enviados.log")
	}
}

// Configuração ou versão diferente: os registros antigos não valem (o texto é mascarado
// com as regras atuais).
func TestEnviadosDeOutraConfiguracaoNaoValem(t *testing.T) {
	dir := t.TempDir()
	m, _ := maskerEmDisco(t, dir)
	s := fracas[2]
	antigo := "tentei " + s + " no login"
	l := m.NovoLote()
	l.Mascarar(antigo, false)
	l.Congelar()
	m.Mascarar(ensina(s))
	m.Persistir()
	m.vistos.SalvarSeSujo()

	vs, _ := CarregarVistos(dir + "/vistos.json")
	m2, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	m2.UsarEnviados(dir+"/enviados.log", "outra configuração")
	if out, _ := m2.Mascarar(antigo); strings.Contains(out, s) {
		t.Errorf("registro de outra configuração foi usado: %q", out)
	}
}

// Linha corrompida ou trecho fora do texto: não remonta, mascara de novo.
func TestEnviadosCorrompido(t *testing.T) {
	dir := t.TempDir()
	m, _ := maskerEmDisco(t, dir)
	texto := "CPF " + cpfTeste
	m.enviados.Gravar(m.idTexto(texto), []trecho{{4, 99, "cpf", "CPF-x"}})
	if out, _ := m.Mascarar(texto); strings.Contains(out, texto[4:]) {
		t.Errorf("trecho inválido foi usado: %q", out)
	}
	m.Persistir()
	f, _ := os.OpenFile(dir+"/enviados.log", os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{quebrado\n")
	f.Close()
	if _, err := CarregarEnviados(dir+"/enviados.log", m.enviados.impressao); err != nil {
		t.Fatal(err)
	}
}
