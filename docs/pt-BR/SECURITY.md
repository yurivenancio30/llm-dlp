# Política de segurança

[English](../../.github/SECURITY.md)

## Versões com suporte

As correções entram na versão mais recente. Se puder, atualize antes de relatar
(`llm-dlp versao` mostra a que você tem).

## Relatar uma vulnerabilidade

**Não abra uma issue pública.** Relate em privado pelo GitHub:
[Report a vulnerability](https://github.com/yurivenancio30/llm-dlp/security/advisories/new)
(aba "Security" do repositório, "Advisories").

No relato:

- a versão do llm-dlp (`llm-dlp versao`) e como você usa;
- um texto que reproduz o problema, **com valores inventados**. Nunca mande dado real: nada de
  nome, documento, credencial ou conteúdo de cliente de verdade. Um valor com a mesma forma
  basta;
- o que você esperava e o que aconteceu (o que chegou à API, o que voltou).

`llm-dlp testar < arquivo` mostra a versão mascarada de um texto e costuma bastar para
reproduzir um problema de detecção.

## O que conta como vulnerabilidade

- Um valor que uma regra documentada deveria mascarar chegando em claro à API.
- Uma resposta desmascarada com o valor errado, ou um valor real enviado a uma ferramenta que
  só deveria receber pseudônimos (`WebFetch`, `WebSearch`, conectores do claude.ai).
- Um jeito de contornar a falha fechada ou a trava sem `sudo`.
- Um valor real ou um texto mascarado gravado em disco pelo llm-dlp.
- Um jeito de reverter um pseudônimo sem a chave.

## O que já é conhecido

Os limites descritos em [seguranca.md](seguranca.md#limites-conhecidos) (por exemplo, um nome
que só aparece em texto corrido, ou um programa rodado pelo agente que leva o dado para fora)
são comportamento documentado. Ideias para reduzi-los são bem-vindas como issues comuns, com
dados inventados.
