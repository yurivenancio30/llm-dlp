# O que é detectado

[← voltar ao README](../README.md)

São quatro jeitos de reconhecer um dado. Qualquer um basta.

## 1. Pelo formato (sempre, em qualquer lugar)

| Tipo | Exemplo |
|---|---|
| Tokens e chaves de API | `ghp_k3Jd9s…`, também dentro de base64 (regras do [gitleaks](https://github.com/gitleaks/gitleaks)) |
| Senhas | `DB_PASSWORD=…`, `senha: …`, "a senha é …", `mysql://usuario:senha@host`, `mysql -pSENHA` |
| Credencial em cabeçalho HTTP | `Authorization: Basic …`, `Authorization: Bearer …`, `Cookie: sessionid=…` |
| E-mail | `joao.silva@empresa.com.br` (menos os que você liberar) |
| CPF e CNPJ com pontuação | `529.982.247-25`, `11.222.333/0001-81`, `12.ABC.345/01DE-35` (CNPJ novo, com letras) |
| Cartão | `4111 1111 1111 1111` |
| Telefone | `(11) 98765-4321`, `+55 11 98765-4321` |
| Endereço | `Rua das Flores, 123` |
| IP de rede interna | `10.42.7.15`, `192.168.0.10` |
| Hostname interno | `mysql.empresa.intra` (os que contêm um dos seus `dominios_internos`) |
| Conta internacional (IBAN) | `GB82 WEST 1234 5698 7654 32` |
| Processo judicial | `0001234-56.2023.8.13.0024` |
| Endereço MAC | `aa:bb:cc:dd:ee:ff` |

## 2. Com a palavra por perto

Sozinhos, estes são só números, e confundiriam com IDs e datas.

| Tipo | Exemplo |
|---|---|
| CPF/CNPJ sem pontuação | `cpf 52998224725` |
| RG, CNH, PIS | `RG: 12.345.678-9`, `CNH 12345678900` |
| CEP | `CEP 01310-100` |
| Agência e conta | `agência 1234-5 conta 123456-7` |
| Chave PIX aleatória | `pix 123e4567-e89b-…` |
| Data de nascimento | `nascimento: 12/03/1985` |
| Título de eleitor, cartão SUS, RENAVAM | `título de eleitor 1234 5678 9012` |
| Passaporte, placa, chassi, IMEI | `passaporte FZ123456`, `placa BRA2E19` |

CPF, CNPJ, PIS, CNH, cartão, título de eleitor, cartão SUS, RENAVAM, IBAN e IMEI só contam
se o dígito verificador for válido.

## 3. Pelo nome do campo

Muito dado pessoal não tem formato (um nome, um código de usuário, um CPF sem pontuação numa
coluna), mas vem com o nome do campo ao lado. O nome do campo decide o que fazer com o valor.

| Campo com nome de... | Exemplo de nome | O valor vira |
|---|---|---|
| Nome de pessoa | `NOM_CLIENTE`, `nome_mae`, `full_name`, `Data Owner`, `Cliente` | `Pessoa …` |
| Código, login, matrícula | `COD_USU_OWNER`, `matricula`, `login`, `user_id`, `id_cliente` | `USUARIO-…` |
| Documento | `CPF`, `NUM_CNPJ`, `rg`, `cnh`, `titulo_eleitor`, `passaporte`, `placa` | `CPF-…`, `DOC-…` |
| Contato e endereço | `telefone`, `celular`, `endereco`, `logradouro`, `cep` | `TELEFONE-…`, `ENDERECO-…` |
| Nascimento | `DT_NASCIMENTO`, `birth_date` | `NASCIMENTO-…` |
| Conta e cartão | `account_number`, `conta_corrente`, `credit_card_number`, `iban` | `CONTA-…`, `CARTAO-…` |
| Dado pessoal sensível (LGPD) | `religiao`, `raca_cor`, `orientacao_sexual`, `tipo_sanguineo`, `deficiencia`, `sindicato`, `doenca` | `SENSIVEL-…` |

**Onde o nome do campo é reconhecido:**

| Formato | Exemplo |
|---|---|
| CSV, TSV, `;` | `NOM_CLIENTE;CPF` na primeira linha |
| Tabela markdown ou de terminal (mysql, psql) | `\| nm_titular \| nr_cpf \|` |
| JSON, YAML, `chave=valor` | `"nome_cliente": "…"`, `matricula: …`, `user_id=…` |
| SQL `INSERT` | `INSERT INTO t (nome_cliente, cpf) VALUES (…)` |
| XML | `<nomeCliente>…</nomeCliente>` |
| Planilha Excel | Ver abaixo |

**Planilha Excel.** O arquivo `.xlsx` nunca vai para a API como arquivo: o que vai é o texto
que uma ferramenta tirou dele. O llm-dlp reconhece as formas usuais desse texto: a tabela
impressa pelo pandas (`print(df)`, com ou sem índice), `to_csv`, `to_markdown`, `to_dict`,
JSON, e as linhas do openpyxl (`('NOME', 'CPF')`).

**Como ele decide se um nome de campo dá match:**

1. Quebra o nome em palavras: `COD_USU_DATA_OWNER` vira `cod`, `usu`, `data`, `owner`;
   `nomeCliente` vira `nome`, `cliente`.
2. **Todas** as palavras têm que ser conhecidas. Uma palavra desconhecida e o campo não dá
   match. É isso que impede de mascarar `NOM_SISTEMA`, `file_name` ou `login_timeout`.
3. A combinação decide o tipo:

| Combinação | Vira | Exemplo |
|---|---|---|
| Uma palavra que diz o dado | Aquele dado | `NUM_CPF_CLIENTE`, `matricula`, `telefone_celular` |
| "nome" + de quem é | Nome de pessoa | `NOM_DATA_OWNER`, `nm_titular`, `customer_name` |
| "código" ou "id" + de quem é | Código de usuário | `COD_USU_OWNER`, `id_cliente`, `user_id` |
| Só de quem é | Nome de pessoa, se o valor tiver nome e sobrenome | `Cliente`, `Responsável`, `Owner` |

Há também uma checagem no valor: numa coluna de nome, tem que ser letras e espaços; numa de
CPF, tem que ter dígitos. Isso evita mascarar um `NULL` ou um cabeçalho repetido.

O vocabulário padrão junta o que as ferramentas de classificação de dados usam (piicatcher,
OpenMetadata, datahub-classify, em inglês) com os termos e prefixos usados no Brasil (`NM_`,
`NO_`, `NU_`, `CO_`, `CD_`). Cada empresa abrevia do seu jeito, então nenhuma lista é
completa: veja [Ensinar o llm-dlp](deteccao.md#ensinar-o-llm-dlp).

## 4. Porque já é conhecido

| Como ficou conhecido | O que passa a valer |
|---|---|
| Você importou a pessoa (`importar-pessoas`) | O nome, o e-mail e o código dela são reconhecidos em qualquer lugar e em qualquer grafia ("João Silva", "JOAO SILVA") |
| O valor já foi mascarado uma vez pelos jeitos 2 ou 3 | É reconhecido depois sozinho, sem a palavra ou o campo ao lado |
| Você listou em `termos` ou `padroes_extras` | Sempre mascarado |

Código de usuário curto ou sem dígito (`jsilva`, `ana`) só é mascarado junto do nome do
campo: procurá-lo no texto inteiro mascararia a palavra em todo lugar. Contas de serviço
(`root`, `postgres`, `admin`) não são mascaradas.

## 5. Nomes de recursos internos (pela estrutura)

Nome de servidor, banco, schema, tabela, coluna, procedure, índice, usuário, namespace,
serviço, bucket ou fila é reconhecido pela **posição na estrutura** do conteúdo (a gramática do
SQL, a chave de uma configuração, a parte de uma URI...), nunca por parecer um nome. As regras
de cada formato estão descritas em [estruturas.md](estruturas.md). Já ligadas:

| Regra | Onde |
|---|---|
| SQL e DDL | Instrução com a forma da gramática (`SELECT … FROM x`, `INSERT INTO x`, `CREATE TABLE x`, `EXEC x`…), em qualquer dialeto, solta ou dentro de uma string de código. O nome depois de `FROM`/`JOIN`/`INTO`/`UPDATE`/`TABLE`/`VIEW`/`PROCEDURE`/`DATABASE`/`SCHEMA`/`INDEX` é objeto, com as partes de `servidor.banco.schema.objeto`; os outros nomes são colunas. Palavras-chave, tipos e funções nativas ficam |
| Mensagem de erro | Palavra do tipo seguida do nome entre aspas ou colchetes (`relation "x"`, `object name 'x'`, `Table 'db.x'`), ou `Table/Dataset projeto:dataset` |
| Conexão | Strings de conexão (`Server=…;Database=…;User Id=…`, ODBC, JDBC, DSN do libpq), URIs de banco (`postgresql://usuario@host/banco`, `jdbc:…`, `mongodb://`), `tnsnames.ora`, URNs do DataHub, `ref()`/`source()` do dbt e `conn_id` do Airflow |
| Tabela | Saída de cliente de banco e CSV/TSV: valores das colunas de catálogo (`table_name`, `table_schema`, `column_name`, `TABNAME`, `owner`, `Tables_in_…`…) e nomes de coluna do cabeçalho que têm cara de identificador |

Uma instrução em minúsculas só vale com pelo menos duas cláusulas (`select … from … where`),
para não confundir com prosa. As regras de DevOps e de desenvolvimento entram nos próximos
lotes.

| Situação | O que acontece |
|---|---|
| O nome aparece numa posição estrutural | Mascarado ali, com um pseudônimo do tipo (`T_…` tabela, `HOST_…` servidor, `C_…` coluna) |
| A posição é inequívoca (ou o nome foi visto em duas regras diferentes) e ele tem cara de identificador (`_`, dígito, ponto, hífen entre partes ou mistura de caixa) | É lembrado e mascarado também em qualquer outro texto, em qualquer caixa, inclusive depois de reiniciar |
| Palavra simples (`cliente`), coluna, índice, vocabulário do próprio formato, conteúdo da web, posição duvidosa | Mascarado só onde apareceu; não é lembrado |
| Não visto há mais de 90 dias | Deixa de ser procurado fora da estrutura |

**Limite:** um nome que só aparece em frases, sem nunca ter passado por uma estrutura que o
llm-dlp reconheça ("o problema é na tabela de pedidos do sistema X"), **não é pego**.

## Na dúvida, mascara

Perto de `senha`, `password` ou `secret`, qualquer valor com dígito ou símbolo é tratado como
senha, mesmo que seja só um nome (`secretRef: db-credentials`). Numa tabela de uma linha só,
em que não dá para separar as colunas, ele mascara a mais. Isso não quebra nada: na volta,
você e os comandos veem o valor real.

## Ensinar o llm-dlp

### Pessoas

Nome e código de pessoa não têm formato. Para que sejam reconhecidos em qualquer texto,
importe uma lista que você já tenha:

```bash
llm-dlp importar-pessoas owners.csv --grupo COD_USUARIO:NOME:EMAIL --separador-nome /
```

- `--grupo` diz quais colunas do CSV têm o código, o nome e o e-mail de cada pessoa. Pode
  repetir a opção para mais de um conjunto de colunas, e deixar uma parte vazia
  (`COD_STEWARD:NOM_STEWARD:`).
- `--separador-nome` é para quando a célula traz o nome seguido de outra coisa
  (`Fulano / Área X`): vale só a parte antes do separador.
- Tudo é guardado **só como hash**. Nome, código e e-mail da mesma pessoa ganham o mesmo
  identificador.
- Código curto ou sem dígito (`TBD`, `N/A`) é ignorado, para não mascarar palavras comuns.

Não é obrigatório: um nome que aparece num campo reconhecido (`NOM_CLIENTE`) entra no
registro sozinho.

### Nomes de campo da sua empresa

Para ver como o llm-dlp entende os campos de um arquivo, e quais ele não reconhece:

```bash
llm-dlp colunas clientes.csv
```

Para ensinar os que faltam, no `config.json`:

```json
"campos_extras": [
  {"tipo": "usuario", "palavras": ["chapa", "re"]},
  {"tipo": "pessoa",  "palavras": ["cooperado", "segurado"]}
]
```

| Tipo | Significado |
|---|---|
| `cpf`, `cnpj`, `rg`, `cnh`, `pis`, `doc`, `nascimento`, `telefone`, `cep`, `endereco`, `usuario`, `conta`, `cartao`, `sensivel`, `nome` | A palavra diz que dado o campo guarda |
| `pessoa` | A palavra diz de quem é o campo (`cooperado` faz `NOME_COOPERADO` valer) |
| `neutra` | A palavra não muda o que o campo guarda (`bco`, `sis`) |

### Códigos e termos próprios

```json
"padroes_extras": [{"rotulo": "matricula", "regex": "\\bMAT\\d{6}\\b"}],
"termos": [{"rotulo": "projeto", "valores": ["Projeto Fenix"]}]
```

`padroes_extras` é para o que tem formato (uma regex). `termos` é para palavras exatas, como
o nome de um projeto ou de um sistema que não pode sair.
