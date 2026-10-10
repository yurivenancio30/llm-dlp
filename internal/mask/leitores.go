package mask

import (
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Registro de todos os leitores de estrutura: os fixos e os que dependem da configuração.
// Para acrescentar um formato, escreva o leitor no arquivo da família e registre-o aqui.

// leitoresDe: todos os leitores para esta configuração (os fixos, depois os da configuração).
func leitoresDe(cfg config.Config) []Leitor {
	return append(leitoresPadrao(), leitoresConfig(cfg)...)
}

// LeitoresPadrao: os leitores fixos (para ferramentas de medição).
func LeitoresPadrao() []Leitor { return leitoresPadrao() }

// leitoresPadrao: os leitores de estrutura que vêm ligados.
func leitoresPadrao() []Leitor {
	return []Leitor{
		{Nome: "sql", Achar: acharSQL, Publico: publicoSQL},
		{Nome: "erro", Achar: acharErroObjeto, Publico: publicoSQL},
		{Nome: "conexão", Achar: acharConexoes, Publico: publicoConexao},
		{Nome: "tabela", Achar: acharTabelasObj, Publico: publicoSQL},
		{Nome: "esquema", Achar: acharEsquema, Publico: publicoSQL},
		{Nome: "nome-contexto", Achar: acharNomePorContexto, Publico: publicoDev},
		{Nome: "devops-yaml", Achar: acharDevopsEstruturado, Publico: publicoDevops},
		{Nome: "terraform", Achar: acharTerraform, Publico: publicoDevops},
		{Nome: "ansible-ini", Achar: acharAnsibleINI, Publico: publicoDevops},
		{Nome: "bicep", Achar: acharBicep, Publico: publicoDevops},
		{Nome: "jenkins", Achar: acharJenkins, Publico: publicoDevops},
		{Nome: "k8s-dns", Achar: acharDNSK8s, Publico: publicoDevops},
		{Nome: "imagem", Achar: acharImagens, Publico: publicoDevops},
		{Nome: "saída-cli", Achar: acharTabelaCLI, Publico: publicoDevops},
		{Nome: "ip-infra", Achar: acharIPInfra},
		{Nome: "chave-valor", Achar: acharChaveValor, Publico: publicoDev},
		{Nome: "endereço", Achar: acharEnderecos, Publico: publicoDev},
		{Nome: "git", Achar: acharGitSCP, Publico: publicoDev},
		{Nome: "pacote", Achar: acharPacotes, Publico: publicoDev},
		{Nome: "caminho", Achar: acharCaminhos, Publico: publicoDev},
		{Nome: "usuário de rede", Achar: acharUsuariosRede, Publico: publicoDev},
		{Nome: "nuvem", Achar: acharNuvem, Publico: publicoDev},
		{Nome: "código-nome", Achar: acharCodigoNome, Publico: publicoDev},
		{Nome: "código-chamada", Achar: acharCodigoChamada, Publico: publicoDev},
		{Nome: "linha-de-comando", Achar: acharCLI, Publico: publicoDev},
		{Nome: "caminho-sistema", Achar: acharCaminhosFora, Publico: publicoDev},
		{Nome: "url-nuvem", Achar: acharURLsNuvem, Publico: publicoDev},
		{Nome: "dsn", Achar: acharDSN, Publico: publicoConexao},
		{Nome: "env-lista", Achar: acharEnvLista, Publico: publicoDev},
		{Nome: "placeholder", Achar: acharPlaceholder, Publico: publicoDev},
	}
}

// leitoresConfig: os leitores ligados conforme a configuração (IP público, termos).
func leitoresConfig(cfg config.Config) []Leitor {
	var ls []Leitor
	if cfg.Objetos.IPPublico {
		ls = append(ls, Leitor{Nome: "ip-público", Achar: acharIPPublico})
	}
	var ts []string
	for _, t := range cfg.Termos {
		for _, v := range t.Valores {
			n := normTermo(v)
			if len(n) >= 3 && !strings.ContainsAny(v, " \t") {
				ts = append(ts, n)
			}
		}
	}
	if len(ts) > 0 {
		ls = append(ls, Leitor{Nome: "termo-embutido", Achar: func(s string, add func(ObjAchado)) { acharTermos(s, ts, add) }})
	}
	return ls
}
