# Segurança e limites

[← voltar ao README](../README.md)

**O que protege:**

- A API da LLM não recebe os valores detectados, só pseudônimos.
- Os pseudônimos não podem ser revertidos sem a chave.
- Nada sensível é gravado em disco pelo llm-dlp (só hashes).
- Se algo falhar, a mensagem não sai.

**Por que a trava e a emergência exigem `sudo`:** tudo que enfraquece a proteção precisa do
administrador. Assim, nada que rode com o seu usuário, inclusive o agente induzido por uma
página maliciosa, consegue desligar a máscara. Se o seu `sudo` não pede senha, essa barreira
não existe.

**O que não protege:**

- Dado que ele não detecta (ver [Limites conhecidos](seguranca.md#limites-conhecidos)).
- `curl` ou outro programa chamado pelo agente, que leve o dado real para fora. A proteção
  de saída vale para `WebFetch` e `WebSearch`.
- Um programa malicioso rodando com o seu próprio usuário, que pode se passar pelo proxy.
- O que não passa por ele: outra instalação do Claude Code (a nativa do Windows, por
  exemplo), outros clientes.

**Arquivos em `~/.config/llm-dlp`:**

| Arquivo | Conteúdo |
|---|---|
| `chave` | A chave secreta. Não compartilhe; guarde uma cópia |
| `config.json` | A configuração |
| `pessoas.json` | Hashes de nomes, e-mails e códigos de pessoas |
| `vistos.json` | Hashes de valores já mascarados |
| `llm-dlp.log` | Uma linha por requisição: contagens e tempos, nunca valores |

## Limites conhecidos

**O que passa sem máscara:**

- Nome de pessoa que não é conhecido e aparece sem nome de campo (texto corrido, ou um
  pedaço de CSV lido sem a linha de cabeçalho).
- Campo com nome fora do vocabulário, até você acrescentar em `campos_extras`.
- **Nomes de bancos, schemas, tabelas, colunas e sistemas.** São metadados, não dados
  pessoais. Para mascarar alguns, use `termos`.
- Dado pessoal sensível (saúde, religião, etnia) em texto corrido. Só é detectado num campo
  com esse nome.
- Valores financeiros (saldo, renda, limite). O que os liga a uma pessoa (nome, CPF, conta)
  é mascarado.
- Senha só de letras (`senha: abacaxi`) ou só de números com até 5 dígitos, a não ser
  dentro de URL.
- Texto muito pequeno ou estilizado numa imagem, que o OCR não lê.

**Outros limites:**

- Com centenas de sub-redes diferentes na mesma conversa, duas podem ganhar a mesma sub-rede
  falsa. Os IPs delas continuam mascarados na ida, mas não são trocados de volta (você vê o
  IP falso), para não apontar um comando para o host errado.
- Depois de reiniciar, um valor lembrado só pelo hash é reconhecido quando aparece "solto"
  ou separado por `= & : / ? @ | # +`. Colado direto em letras, só depois de reaparecer uma
  vez solto.
- Por enquanto, só a API da Anthropic (ver [Acoplar outra API de LLM](desenvolvimento.md#acoplar-outra-api-de-llm)).
