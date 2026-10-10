# Ideias para depois

[← voltar ao README](README.md) · [English](../ideas.md)

Uma linha por ideia: o que foi visto e ainda não foi feito. Nada aqui é promessa.

## Mascarar mais (hoje passa)

- Pasta de projeto que é palavra simples (`/home/ubuntu/faturahx/...`): só a pasta com cara de identificador (hífen, dígito, `_`) é mascarada.
- `import polaris` num arquivo lido: palavra simples decidida em outro lugar não é levada para dentro de arquivo.
- Chave em português que não é palavra de tipo (`"Banco": "vendashx_prod"`): `banco` também é banco de dinheiro, por isso não entrou no vocabulário.
- Nome de CTE com cara de identificador (`WITH clientes_inadimplentes AS (`): hoje todo nome de CTE é nome local da consulta, como apelido de tabela.
- Diretório de trabalho da sessão (vem na requisição): usar como origem, para saber que todo caminho debaixo dele é do projeto.

## Mascarar menos (hoje sobra)

- `pacote@versão` (`lodash@4.17.21`, `testify@v1.8.4`) sai como e-mail. Antes de liberar, conferir que o nome do pacote do cliente continua coberto por outro leitor.
- Dependência instalada com nome de palavra de dicionário (`pandas`, `requests`): as pastas do caminho seguem a regra comum. Uma lista dos pacotes mais baixados do PyPI e do npm daria a segunda chave sem depender do dicionário.
- Rótulo traduzido de chave de tipo num catálogo em outra escrita (`"User": "Użytkownik"`).
- DataHub: padrões `allow`/`deny` de `table_pattern`/`schema_pattern` (regex) nas receitas de ingestão; mascarar só os trechos literais que são nome inteiro, sem quebrar a regex.
