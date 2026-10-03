# llm-dlp

Proxy local que **mascara dado sensível entre você e a LLM**. Hoje funciona com o Claude Code.

Você e o seu disco veem tudo real. A API do modelo só vê pseudônimos.

```
você ─► Claude Code ─► [ llm-dlp ] ─► API da Anthropic
 real       real        mascara ►      só vê pseudônimos
 real   ◄── real    ◄── desmascara ◄── responde com pseudônimos
```

Exemplo do que acontece com uma mensagem:

```
você escreve:   o cliente de cpf 529.982.247-25 (maria.lopes@banco.com.br) não acessa mysql.banco.intra
a API recebe:   o cliente de cpf CPF-irvikcpk (p.k3f9x2ab@d7q2m.invalid) não acessa mysql-x7k2.invalid
a API responde: verifique se p.k3f9x2ab@d7q2m.invalid tem permissão em mysql-x7k2.invalid
você lê:        verifique se maria.lopes@banco.com.br tem permissão em mysql.banco.intra
```

## Índice

1. [O que ele faz](#o-que-ele-faz)
2. [Instalação](#instalação)
3. [No dia a dia](#no-dia-a-dia)
4. [Como funciona](#como-funciona)
5. [O que é detectado](#o-que-é-detectado)
6. [Ensinar o llm-dlp](#ensinar-o-llm-dlp)
7. [Configuração](#configuração)
8. [Comandos](#comandos)
9. [Segurança](#segurança)
10. [Desempenho](#desempenho)
11. [Limites conhecidos](#limites-conhecidos)
12. [Para desenvolvedores](#para-desenvolvedores)

## O que ele faz

- **Mascara tudo que vai para a API:** o que você digita, arquivos lidos, saída de comandos,
  dados de MCP, subagentes, anexos e até o título da sessão.
- **Desmascara tudo que volta:** você lê os valores reais; comandos rodam e arquivos são
  gravados com os valores reais.
- **Mantém a referência:** o mesmo valor vira sempre o mesmo pseudônimo, então o modelo
  continua entendendo que duas menções são da mesma pessoa, do mesmo servidor, da mesma conta.
- **Lê imagens e PDFs** (OCR) e cobre de preto o que for sensível.
- **Falha fechada:** se o llm-dlp cair ou não souber tratar algo, a mensagem **não sai**. Não
  existe vazamento silencioso.
- **Não pesa:** uma mensagem custa 5–30 ms, quase não muda o número de tokens e não atrapalha
  o cache da API.
- **Não exige nada no dia a dia:** sobe sozinho com a sessão e volta sozinho se cair.

## Instalação

Funciona em qualquer Linux, inclusive WSL no Windows. O programa é um arquivo só, sem
dependências. No macOS compila, mas ainda não foi testado por completo.

**1. Obter o programa** (por enquanto, a partir do código; precisa do Go):

```bash
git clone git@github.com:yurivenancio30/llm-dlp.git && cd llm-dlp && make build
```

**2. Instalar.** Um comando, que explica e pede confirmação a cada passo:

```bash
./bin/llm-dlp instalar
```

| O que ele faz | Por quê |
|---|---|
| Copia o programa para `~/.local/bin` | Para ser chamado de qualquer pasta |
| Cria `~/.config/llm-dlp` com a configuração e a chave secreta | A chave gera os pseudônimos. Ele pergunta os seus domínios internos (ex.: `banco`) |
| Confere o OCR (tesseract) | Se faltar, mostra o comando `sudo` certo para a sua distribuição. Sem OCR, imagens e PDFs ficam bloqueados |
| Liga o Claude Code ao llm-dlp | Mostra o que vai mudar no `~/.claude/settings.json` e só aplica se você confirmar (com backup) |

**3. Trava contra vazamento silencioso** (recomendada):

```bash
sudo llm-dlp instalar-trava
```

Sem a trava, se a configuração do passo 2 for apagada, o Claude Code volta a falar direto
com a API, sem máscara e sem avisar. Com ela, o Claude Code **se recusa a funcionar** fora
do llm-dlp. Precisa de `sudo` porque fica numa pasta do sistema, que o agente não consegue
alterar. Requer Claude Code 2.1.285 ou mais novo.

Depois, feche e abra o Claude Code.

## No dia a dia

Não há nada para rodar. O llm-dlp sobe ao abrir uma sessão e volta sozinho se cair.

| Situação | O que acontece / o que fazer |
|---|---|
| Quero ver se está tudo certo | `llm-dlp status` |
| O llm-dlp caiu | Volta sozinho em ~1 s. Enquanto estiver fora, nenhuma mensagem sai |
| Ele não volta, e preciso do Claude para consertar | `sudo llm-dlp emergencia 30m` libera o Claude **sem máscara** por tempo limitado. Use uma sessão nova, sem dados de cliente. Volta ao normal no fim do prazo, ou com `sudo llm-dlp emergencia sair`. Cada mensagem mostra um aviso |
| Imagem ou PDF bloqueado | Falta o OCR: a mensagem de erro traz o comando de instalação |
| Mudei o `config.json` | `llm-dlp parar` (ele volta na próxima mensagem, com a configuração nova) |
| Quero ver o que foi feito em cada mensagem | `~/.config/llm-dlp/llm-dlp.log` (só contagens e tempos, nunca valores) |
| Quero desfazer tudo | `llm-dlp desinstalar` (e `sudo llm-dlp desinstalar-trava`, se instalou a trava) |

## Como funciona

### O caminho de uma mensagem

1. O Claude Code é configurado para falar com `http://127.0.0.1:8787` (o llm-dlp) em vez de
   falar direto com a API. Funciona com assinatura ou com chave de API.
2. **Ida:** o llm-dlp recebe a requisição, procura dado sensível em cada texto e troca por
   pseudônimos. Guarda, só em memória e só para aquela requisição, a tabela
   "pseudônimo → valor real".
3. Envia a requisição mascarada para a API.
4. **Volta:** a resposta chega em pedaços (streaming). O llm-dlp troca os pseudônimos de
   volta usando a tabela, inclusive quando um pseudônimo chega cortado entre dois pedaços.
5. O Claude Code recebe tudo real: mostra para você, roda comandos e grava arquivos com os
   valores verdadeiros.

Como o Claude Code reenvia a conversa inteira a cada mensagem, o llm-dlp memoriza o
resultado de cada texto. Só o que é novo é examinado.

### Pseudônimos

| Valor real | O que a API vê |
|---|---|
| `joao.silva@empresa.com.br` | `p.mxvotcyt@didl3.invalid` |
| `JOAO CARLOS SILVA` | `Pessoa mxvotcyt` (mesmo identificador do e-mail, se for a mesma pessoa) |
| `B123456` (código do João) | `USUARIO-mxvotcyt` |
| `10.42.7.15` | `250.153.31.15` (mesma sub-rede vira a mesma sub-rede falsa) |
| `mysql-dh.empresa.intra` | `mysql-x7k2.invalid` |
| `529.982.247-25` | `CPF-irvikcpk` |
| token ou senha | `SEGREDO-…` |

- São derivados de uma **chave secreta de 256 bits** (HMAC-SHA256). Sem a chave, não dá para
  saber quem é quem, nem testando listas de candidatos.
- O mesmo valor vira sempre o mesmo pseudônimo, entre mensagens e entre sessões.
- **Perder a chave não perde dado**, porque os seus arquivos são reais. Uma nova é gerada e
  os pseudônimos mudam dali em diante. Mesmo assim, guarde uma cópia de
  `~/.config/llm-dlp/chave`.

### O que é lembrado

Alguns dados só são reconhecidos com uma pista ao lado (a palavra "RG", o nome da coluna).
Quando um desses valores é mascarado, o llm-dlp passa a lembrar dele e o reconhece depois em
qualquer lugar: na saída de um comando, num arquivo lido mais tarde, sem a pista por perto.

| Onde | O que guarda |
|---|---|
| Memória | O valor real, enquanto o llm-dlp estiver no ar |
| Disco (`vistos.json`, `pessoas.json`) | **Só o hash**, feito com a chave. Serve para reconhecer depois de reiniciar |

Nenhum valor real e nenhum texto mascarado é gravado em disco pelo llm-dlp.

### Falha fechada

| Situação | O que acontece |
|---|---|
| O llm-dlp está fora do ar | O Claude Code não consegue enviar a mensagem |
| A requisição não é JSON, ou tem algo que ele não sabe tratar | Recusada com erro; nada é enviado |
| Imagem ou PDF que não deu para ler | Recusada |
| Alguém apaga a configuração (com a trava instalada) | O Claude Code se recusa a funcionar |

### Imagens e PDFs

O texto é lido por OCR e os trechos sensíveis são cobertos de preto antes de sair. Como o
OCR erra (lê `@` como `Q`, por exemplo), em imagem o llm-dlp cobre a mais: tudo com cara de
e-mail, domínio, IP ou número longo. PDF com texto vira texto mascarado; PDF escaneado vira
imagens cobertas.

### Ferramentas que vão para a internet

Quando o Claude usa uma ferramenta, o pseudônimo é trocado pelo valor real antes de ela
rodar. É assim que comandos e arquivos funcionam com os dados verdadeiros:

```
o Claude escreve:  grep "p.mxvotcyt@didl3.invalid" arquivo.txt
roda de verdade:   grep "joao.silva@empresa.com.br" arquivo.txt      (na sua máquina)
```

`WebFetch` (abrir uma página) e `WebSearch` (pesquisar) são exceção: o que o Claude escreve
nelas vai para um site de fora. Se a troca fosse feita, o dado real sairia junto, e uma
página maliciosa poderia pedir isso de propósito. Por isso, **só nessas duas ferramentas a
troca não é desfeita**: o site recebe o pseudônimo, que não serve para nada.

O efeito colateral: pesquisar na web algo que foi mascarado não acha nada. Para mudar, edite
`ferramentas_sem_desmascarar` no `config.json`.

## O que é detectado

São quatro jeitos de reconhecer um dado. Qualquer um basta.

### 1. Pelo formato (sempre, em qualquer lugar)

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

### 2. Com a palavra por perto

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

### 3. Pelo nome do campo

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
completa: veja [Ensinar o llm-dlp](#ensinar-o-llm-dlp).

### 4. Porque já é conhecido

| Como ficou conhecido | O que passa a valer |
|---|---|
| Você importou a pessoa (`importar-pessoas`) | O nome, o e-mail e o código dela são reconhecidos em qualquer lugar e em qualquer grafia ("João Silva", "JOAO SILVA") |
| O valor já foi mascarado uma vez pelos jeitos 2 ou 3 | É reconhecido depois sozinho, sem a palavra ou o campo ao lado |
| Você listou em `termos` ou `padroes_extras` | Sempre mascarado |

Código de usuário curto ou sem dígito (`jsilva`, `ana`) só é mascarado junto do nome do
campo: procurá-lo no texto inteiro mascararia a palavra em todo lugar. Contas de serviço
(`root`, `postgres`, `admin`) não são mascaradas.

### Na dúvida, mascara

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

## Configuração

Arquivo `~/.config/llm-dlp/config.json`. Depois de mudar, rode `llm-dlp parar`.

| Campo | Para quê | Padrão |
|---|---|---|
| `dominios_internos` | Trechos que identificam os seus hostnames internos (`["banco"]` pega `mysql.banco.intra`) | vazio |
| `emails_liberados` | E-mails que passam sem máscara | vazio |
| `dominios_email_liberados` | Domínios de e-mail que passam sem máscara | `example.com`, `anthropic.com`… |
| `papeis_host` | Primeira parte do hostname que pode ficar visível (`mysql` em `mysql-x7k2.invalid`) | `mysql`, `postgres`, `redis`… |
| `campos_extras` | Palavras a mais para os nomes de campo | vazio |
| `padroes_extras` | Regex próprias | vazio |
| `termos` | Palavras exatas a mascarar | vazio |
| `detectores_desligados` | Detectores a desligar (lista abaixo) | vazio |
| `detectores_opcionais` | Detectores a ligar. Hoje só `quase`: sexo, idade, estado civil, profissão, nacionalidade, renda, latitude/longitude | vazio |
| `ferramentas_sem_desmascarar` | Ferramentas que recebem só pseudônimos | `WebFetch`, `WebSearch` |
| `falhar_fechado` | Recusa o que não sabe mascarar | `true` |
| `ocr` | `modo` (`mascarar`, `bloquear` ou `permitir`), `idioma`, `max_paginas`, caminhos do tesseract e do poppler | `mascarar`, `por`, 30 |
| `porta` | Porta local do proxy | 8787 |
| `upstream` | Endereço da API | `https://api.anthropic.com` |

Nomes aceitos em `detectores_desligados`: `segredo`, `email`, `ip`, `host`, `cpf`, `cnpj`,
`pis`, `cnh`, `rg`, `telefone`, `cep`, `cartao`, `conta`, `pix`, `endereco`, `nascimento`,
`nome`, `campo` (detecção pelo nome do campo), `usuario`, `doc`, `iban`, `processo`, `mac`.

## Comandos

| Comando | O que faz |
|---|---|
| `llm-dlp instalar` | Instala e configura tudo, explicando cada passo |
| `sudo llm-dlp instalar-trava` | O Claude Code só funciona passando pelo llm-dlp |
| `llm-dlp desinstalar` / `sudo llm-dlp desinstalar-trava` | Desfaz |
| `llm-dlp status` | Mostra se está no ar e em que modo |
| `llm-dlp parar` | Para o proxy (ele volta na próxima mensagem) |
| `sudo llm-dlp emergencia [30m\|sair]` | Libera o Claude sem máscara por tempo limitado |
| `llm-dlp importar-pessoas ARQ.csv --grupo COD:NOME:EMAIL` | Ensina pessoas |
| `llm-dlp colunas ARQ` | Mostra como os campos de um arquivo são entendidos |
| `llm-dlp testar < arquivo` | Mostra a versão mascarada de um texto |
| `llm-dlp testar-midia ARQ DIR` | Processa uma imagem ou PDF e grava o resultado em DIR |
| `llm-dlp medir ARQ.jsonl` | Tempo e cobertura sobre uma sessão antiga do Claude Code (só contagens) |
| `llm-dlp versao` | Versão |

`garantir`, `verificar`, `servir` e `supervisionar` são internos: quem chama são os ganchos
do Claude Code e o supervisor.

## Segurança

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

- Dado que ele não detecta (ver [Limites conhecidos](#limites-conhecidos)).
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

## Desempenho

Medido em sessões reais e em testes de carga.

| Item | Valor |
|---|---|
| Mensagem numa conversa em andamento | 5–30 ms |
| Texto novo | ~0,4 ms por trecho (95% abaixo de 8 ms) |
| Primeira mensagem depois de reiniciar, 1 MB de conversa | 0,1–0,45 s com 32 núcleos; 0,15–0,75 s com 4 |
| Um texto só de 1 MB | 0,1–0,3 s com 32 núcleos; 0,2–0,7 s com 4; 0,6–2,5 s com 1 |
| Imagem | 0,7–2 s na primeira vez; ~1 ms quando reenviada |
| Tokens | ~1,4 token a mais por valor mascarado; nada é acrescentado ao contexto. Num teste real com um arquivo em que quase todo campo é sensível (245 valores), a requisição ficou 0,6% maior |
| Cache da API | Igual ao uso sem o llm-dlp |
| Memória | 40–100 MB no uso normal; no pior caso medido, estabilizou em ~170 MB |
| CPU parado | Zero |

- **Quantidade não pesa:** o tempo não cresce com o número de valores aprendidos nem de
  pseudônimos na conversa (medido até 120 mil valores e 20 mil e-mails distintos).
- **Tudo que é memorizado tem teto** e descarta o mais antigo.
- **O cache é refeito uma vez** quando um valor é aprendido: as ocorrências anteriores dele
  passam a ser mascaradas.
- **Texto grande** é examinado em pedaços, em paralelo, usando os núcleos que a máquina
  tiver. O corte é sempre num espaço em branco, para nunca dividir uma senha ou token. Por
  isso um bloco gigante sem nenhum espaço (2 MB de base64 ou de JSON minificado) não é
  dividido e leva de 1 a 5 s.

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
- Por enquanto, só a API da Anthropic (ver [Acoplar outra API de LLM](#acoplar-outra-api-de-llm)).

## Para desenvolvedores

```bash
make build   # binário estático em bin/llm-dlp
make test

# testes de carga (minutos): memória com 1,2 GB de texto, entradas patológicas, 16 conversas
# em paralelo, respostas abandonadas, API fora do ar, requisição de 32 MB
LLM_DLP_ESTRESSE=1 go test ./... -run Estresse -v -timeout 60m
```

### Mapa do código

```
cmd/llm-dlp/          o programa de linha de comando
  main.go               ponto de entrada e lista de comandos
  servico.go            servir, supervisionar, garantir, verificar, status, parar
  ferramentas.go        importar-pessoas, testar, testar-midia, medir, colunas
  instalar.go           instalar, desinstalar e a trava
  emergencia.go         modo emergência (exige sudo)

internal/config/      leitura do config.json

internal/proxy/       o servidor que fica entre o Claude Code e a API
  proxy.go              recebe a requisição, encaminha, devolve a resposta
  requisicao.go         ida: mascara o corpo da requisição
  resposta.go           volta: desmascara a resposta (streaming ou inteira)
  midia.go              imagens e PDFs (OCR e tarja preta)

internal/mask/        detecção e troca por pseudônimos
  masker.go             o tipo Masker e a sua construção
  mascarar.go           Mascarar: porta de entrada, com memória de resultados
  detectar.go           Detectar: junta os detectores; texto grande vai em pedaços
  detectores_formato.go   e-mail, IP, hostname, CPF, CNPJ, telefone, cartão, tokens
  detectores_senhas.go    senhas comuns
  detectores_extras.go    cabeçalho HTTP, IBAN, processo, documentos com palavra por perto
  campos_vocabulario.go   nome do campo: que nomes guardam que dado
  campos.go               nome do campo: "campo: valor", JSON, SQL INSERT, XML
  campos_tabelas.go       nome do campo: CSV, markdown, terminal, planilha
  validadores.go        dígitos verificadores (CPF, CNPJ, título, IBAN...)
  conhecidos.go         valores já mascarados são reconhecidos depois (em memória)
  vistos.go             os mesmos valores em disco, só como hash
  pessoas.go            registro de pessoas (nomes, e-mails, códigos), só como hash
  chave.go              a chave secreta e os identificadores derivados dela
  pseudonimos.go        troca dos achados por pseudônimos
  desmascarar.go        troca de volta, inclusive em resposta que chega em pedaços

internal/ocr/         leitura de texto em imagem e PDF (tesseract, poppler)
```

Os testes ficam ao lado do código que testam, com o mesmo nome e `_test.go` no fim
(`campos.go` e `campos_test.go`): é a convenção do Go, e é o que permite testar funções
internas. À parte: `ajuda_test.go` (funções de apoio), `benchmark_test.go` (medições de
tempo) e `estresse_test.go` (testes de carga).

### Como um texto é mascarado

```
proxy.ServeHTTP                      recebe a requisição
 ├ mascararCorpo (requisicao.go)     percorre o JSON e chama Mascarar em cada texto
 │  └ mask.Mascarar (mascarar.go)    já está na memória? devolve. senão:
 │     ├ Detectar (detectar.go)      roda os detectores e junta os achados
 │     │  ├ detectores_*.go          formato, senhas, extras
 │     │  ├ campos*.go               nome do campo
 │     │  └ conhecidos.go            valores já vistos
 │     └ aplicar (pseudonimos.go)    troca cada achado pelo pseudônimo
 ├ envia para a API
 └ desmascararSSE (resposta.go)      troca os pseudônimos de volta, pedaço a pedaço
```

### Acoplar outra API de LLM

Hoje o llm-dlp entende o formato da API da Anthropic (`/v1/messages`). O que já funciona e o
que falta para outro formato (OpenAI, Gemini):

| Parte | Hoje, com outra API | O que fazer |
|---|---|---|
| Cliente | Qualquer cliente que aceite trocar o endereço da API pode apontar para o llm-dlp | Configurar a variável do cliente (como o `ANTHROPIC_BASE_URL` do Claude Code) e o `upstream` no `config.json` |
| Ida (requisição) | Um caminho que não seja `/v1/messages` cai no modo genérico: **todo texto do JSON é mascarado**. É seguro, mas mascara até campos que não precisava | Em `requisicao.go`, escrever uma função como `requisicaoAnthropic` para o formato novo: quais campos levam texto do usuário, quais nunca podem mudar, onde vêm as imagens. Escolher a função pelo caminho em `mascararCorpo` |
| Imagens | Só o formato da Anthropic é tratado. No modo genérico, imagem em outro formato não passa pelo OCR | Na função nova, mandar os blocos de imagem para `Midia.Processar` |
| Volta sem streaming | Os campos `text`, `content` e `input` do JSON são desmascarados | Acrescentar os campos do formato novo em `desmascararJSONResposta` (`resposta.go`) |
| Volta com streaming | **Não é desmascarada**: você veria os pseudônimos. Nada vaza | Em `resposta.go`, tratar os eventos do formato novo como `desmascararSSE` faz: achar o campo que traz o texto e passá-lo por `mask.Fluxo`, que cuida do pseudônimo cortado entre dois pedaços |
| Mais de uma API ao mesmo tempo | Não: há um `upstream` só | Escolher o destino pelo caminho da requisição em `proxy.go` |

O pacote `mask` não depende do formato de nenhuma API: recebe texto e devolve texto. Todo o
trabalho de uma API nova fica em `internal/proxy`.

O que testar ao acoplar: nada real chega à API, a resposta volta exatamente real, e as
mensagens antigas chegam idênticas de uma requisição para a outra (o cache depende disso).
O `estresse_test.go` do proxy faz essas três conferências e serve de modelo.

### Acrescentar um detector

1. Escreva a função num dos `detectores_*.go` (ou num arquivo novo). Ela recebe o texto e
   chama `add(início, fim, "tipo")` para cada trecho achado.
2. Chame-a em `detectarBase` (`detectores_formato.go`), atrás de um teste barato que evite
   rodar em texto que não tem o "ingrediente" (uma palavra-chave, um caractere).
3. O pseudônimo sai como `TIPO-identificador`. Para outro formato, acrescente um caso em
   `Pseudonimo` (`pseudonimos.go`).
4. Se o valor deve ser lembrado depois de mascarado uma vez, acrescente o tipo em
   `contextuais` (`conhecidos.go`).
5. Testes: um caso que pega e um que não pega em `detectores_test.go`, e o caso em
   `casosDeCorte` (`detectar_test.go`), que confere o dado em todas as posições de corte de
   um texto grande.
6. Rode `go test ./...` e os benchmarks (`go test ./internal/mask -bench Frio`) para ver se
   não pesou.

## Licença

MIT. Ver [LICENSE](LICENSE).
