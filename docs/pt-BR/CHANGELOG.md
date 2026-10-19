# Mudanças

[English](../../CHANGELOG.md)

O que mudou em cada versão do llm-dlp. As versões seguem `MAIOR.MENOR.CORREÇÃO`; o que cada
número significa aqui está em [versioning.md](versioning.md). A mais nova vem primeiro.

Em cada versão: **Mascaramento** é o que passou a ser (ou deixou de ser) mascarado;
**Programa** é comando, instalação, configuração, desempenho e documentação; **Código** é o
que só interessa a quem mexe no código.

## [0.13.0] - 2026-10-19

### Mascaramento

- Um registro de conexão escrito por uma pessoa (um inventário, um chamado): um bloco de linhas
  `Rótulo: valor` com um servidor e um número de porta (`Host: SRVSQL`, `Porta: 1433`,
  `DataBase:` com os bancos recuados embaixo). Os valores dos rótulos de conexão são mascarados
  mesmo quando são uma palavra solta; vários valores numa linha (`A / B`) e a lista recuada
  também. Sem a porta, `Server: nginx` fica.
- `~/x` é lido como `/home/<usuário>/x`, e a última parte de um caminho só é arquivo quando termina
  numa extensão: `cd ~/HX.AWS.PRD.Carga` é pasta, `main.tf` e `v1.2.3` continuam arquivos.
- O ID do endpoint de um cluster EKS (`<id>.yl4.<região>.eks.amazonaws.com`), e o IP privado
  dentro de um nome EC2 também quando está grudado em outro nome
  (`pod-x-ip-10-24-34-0.us-east-2.compute.internal-abc12`).
- Os filtros de uma receita de ingestão do DataHub: nas listas `allow`/`deny` de
  `database_pattern`, `schema_pattern`, `table_pattern`..., um literal ancorado (`"^DWVENDAS$"`,
  `'^DW\.dbo\..*'`) é o nome. Uma regex com alternativas (`^(dev|test)_.*`) fica.
- O nome de uma tag (`urn:li:tag:<nome>`), a menos que seja feito de palavras do dicionário
  (`Legacy`, `dados_pessoais`).
- Medido numa sessão real de 15.306 blocos: apareceram 789 achados e nenhum sumiu. Limite: esses
  nomes soltos não são propagados para o resto da conversa; citados em prosa ou numa tabela,
  continuam saindo (539 das 856 vezes em que os nomes daquele inventário apareceram fora dele).

### Código

- `reader_connection_record.go`, `reader_allow_deny.go` e `resource_labels_test.go`; `fileName`
  (`reader_paths.go`) decide o que é arquivo nos três leitores de caminho.

## [0.12.0] - 2026-10-15

Menos palavras comuns mascaradas como nome: os leitores de estrutura só leem um nome onde a
estrutura é a que eles leem. Medido numa sessão real de 19.078 blocos: sumiram 1.356 achados, e
105 nomes reais que saíam passaram a ser mascarados.

### Mascaramento

- Uma lista de rótulos não é uma lista de nomes: em `"colunas": ["Elemento", "O que se mediu",
  "Veredito"]`, uma frase entre os itens faz dela o cabeçalho de um relatório, e só um item com
  forma de identificador continua mascarado. Um campo que dá nome a si mesmo
  (`"Tabelas": "tabelas"`, `"usuarios": "Usuários"`) não é recurso, no código como na
  configuração.
- Uma chamada (`f("a", "b")`) não é uma linha de resultado.
- Uma tabela com separador termina na primeira linha sem ele (`--`, o título da tabela seguinte);
  o cabeçalho da tabela seguinte não é dado dela. Uma célula entre aspas que passa de uma linha
  (RFC 4180) não termina a tabela.
- Num caminho do Windows, a palavra depois de uma pasta com espaço no nome
  (`Power BI\Banco de Dados`) é mascarada como pasta, não como `DOMÍNIO\usuário`.
- `CREATE TABLE` numa frase não é DDL: depois do nome tem de vir o que os dialetos põem ali, e o
  primeiro item dos parênteses tem de ser uma coluna ou uma restrição. O `FROM` de uma função
  (`EXTRACT(EPOCH FROM x)`) não é tabela. O que vem depois de um schema do sistema
  (`information_schema.EVENTS`, `SNOWFLAKE.ACCOUNT_USAGE.X`) é do sistema; o banco antes dele
  continua mascarado. O vocabulário público de SQL ganhou as colunas de `pg_stat_*_tables`,
  `pg_extension`, do `INFORMATION_SCHEMA.TABLES` do MySQL, as funções de tamanho e as partes do
  `EXTRACT`, tiradas das páginas oficiais.
- Um nome com pontos cuja raiz é a raiz de uma variável de template no mesmo texto
  (`{{analise.veredito}}`) é um caminho nos dados do programa, não schema.tabela.
- Dois nomes reais só eram mascarados por acaso (uma tabela emendada na anterior) e agora saem;
  nome de cliente ou de sistema citado em prosa é para `terms`. Os nomes aprendidos por engano
  antes ficam no arquivo de vistos: continuam mascarados enquanto seguem aparecendo (cada vez
  renova a data) e param depois de 90 dias sem aparecer.

### Código

- `structure_shapes_test.go`: cada regra com o outro lado (o nome real ao lado continua
  mascarado); todo caso de "fica" falha na 0.11.1.

## [0.11.1] - 2026-10-14

### Mascaramento

- Uma instrução SQL sem `;` num item de lista não leva mais os itens seguintes: as palavras deles
  (`10. minha carteira`, `11. cpf`) eram mascaradas como colunas. O próximo item (`10. `, `- `,
  `• `) termina a instrução. Em 40 MB de código público, sumiram 416 achados em listas de
  changelog e README, e nenhum apareceu.

## [0.11.0] - 2026-10-14

Passam a ser mascarados os dados de autenticação, que a Resolução CD/ANPD nº 15/2024 (art. 5º,
IV) lista entre os dados que tornam um incidente de segurança um risco relevante. Instalações
antigas recebem as regras novas do Claude Code ao atualizar.

### Mascaramento

- Senha só de dígitos: `senha: 4821`, `SENHA_BANCO=4821`, "a senha é 4821". Antes, um número de
  menos de 6 dígitos ao lado da palavra saía. Uma chave que guarda uma quantidade da senha
  (`password_min_length: 8`, `senha_tentativas: 3`) fica.
- Códigos de autenticação, ao lado do rótulo: PIN (`PIN: 4821`), o código de segurança do
  cartão (`CVV: 123`, `código de segurança`), códigos de uso único e de verificação
  (`OTP: 482913`, `2FA`, `código de verificação`, `código de acesso`, `token: 482913`), códigos
  de recuperação e a resposta de uma pergunta de segurança. CVV e PIN são os "dados de
  autenticação sensíveis" do PCI DSS; o código de uso único tem de 6 a 8 dígitos (RFC 4226 e
  6238). Nome do detector: `secret`.
- Frase de recuperação de carteira (BIP-39): 12 a 24 palavras da lista oficial, em inglês ou em
  português, cujo dígito verificador confere, também sem rótulo e também numerada
  (`1. legal 2. winner…`). Depois de um rótulo (`frase de recuperação:`, `seed phrase:`),
  qualquer valor até o fim da frase.

### Programa

- O `llm-dlp update` e o `llm-dlp reload` acrescentam no `~/.claude/settings.json` as regras
  que mantêm o agente longe do llm-dlp quando uma versão nova as traz. Até aqui só o `install`
  as gravava, então uma instalação antiga não tinha `Bash(*.config/llm-dlp*)` (ler ou mudar a
  configuração por comando) nem `Bash(*llm-dlp emergency*)`. Para a trava, que precisa de
  `sudo`, eles dizem o que falta e oferecem acrescentar; o `install` faz o mesmo, e o
  `llm-dlp status` avisa enquanto faltar alguma regra. Vindo da 0.10.0 ou anterior, rode
  `llm-dlp reload` uma vez depois do update: o update roda o código da versão que estava
  instalada.
- O `llm-dlp install`, rodado de novo, grava num `config.json` que já existe só o que foi
  respondido (domínios internos, nomes protegidos), mantém o resto do arquivo como estava e
  guarda o anterior como `config.json.bak-DATA`. Antes, ele regravava o arquivo inteiro a partir
  do que conhecia, sem cópia.
- As últimas mensagens em português passam para o inglês: os passos do `install` ("Program",
  "Apply?", "Summary"), o `llm-dlp columns`, o aviso de corte do `llm-dlp last`, o uso do
  `simulate`, um erro do OCR e o aviso que o modelo recebe por uma imagem coberta. A resposta da
  checagem de saúde (`llm-dlp status`) diz `requests` e `online_since`.
- O resumo do `simulate` volta a classificar a recusa por imagem ou PDF que não deu para
  verificar (ele procurava o texto em português).
- O `llm-dlp install` sem terminal (rodado por um script ou por um agente) fazia perguntas que
  ninguém respondia e terminava sem aplicar nada: agora diz para rodar num terminal ou com
  `--yes`, que aceita todos os passos menos os que precisam de `sudo` (agora no `llm-dlp help`).
- Documentação: os campos do `ocr` (`strict_pdf`, os caminhos dos programas, `env`), os arquivos
  de `cmd/llm-dlp` e `internal/proxy`, e nomes de leitor ainda em português no `structures.md`.

### Código

- `detectors_auth.go`, com as listas da BIP-39 em `internal/mask/data/` (licença MIT, origem no
  cabeçalho). `config.SetFields` grava alguns campos do `config.json` mantendo o resto.
- O `TestPseudonymGolden` não depende mais do dia em que roda (o arquivo de valores vistos guarda
  a data; a partir de 2026-10-15, o teste da 0.10.0 falhava).

## [0.10.0] - 2026-10-14

Mais dados passam a ser reconhecidos pelo próprio dado, cada um pela regra de quem define o
formato (IANA e RFCs, documentação da AWS e do Kubernetes, manual do contribuinte da NF-e,
manual do Pix do Banco Central, FEBRABAN, UPU, CNJ, IBGE, Ministério da Saúde, TSE). Testado com
endereços inventados em IPv4, IPv6, rotas e nomes de máquina, e com documentos e frases
inventados: o que vazava está listado abaixo.

O llm-dlp passa a ser todo em inglês: nomes de tipo e de detector, rótulos dos pseudônimos,
arquivos de dados, linhas do log e mensagens. O que ele detecta não muda: as palavras em
português que as regras procuram no texto são as mesmas, e o identificador de cada pseudônimo
também.

### Mascaramento

- IPv4 interno também na faixa compartilhada dos provedores `100.64.0.0/10` (RFC 6598, CGNAT;
  usada por VPNs e clusters), no nome de uma máquina (`ip-10-42-7-15.ec2.internal`, o prompt
  `ubuntu@ip-10-42-7-15`, `10-42-7-15.default.pod.cluster.local`) e no DNS reverso
  (`15.7.42.10.in-addr.arpa`, e a zona `/24` `7.42.10.in-addr.arpa`). O IP falso mantém o jeito
  de escrever (hífens, ao contrário), e a mesma rede continua a mesma rede falsa.
- IPv6 interno passa a ser mascarado sem `objects.public_ip`, como nome de servidor: local
  único (`fd12:3456:789a:1::15`, RFC 4193), link-local com identificador de interface
  (`fe80::1c2d:3e4f:5a6b:7c8d`, que pode vir do MAC), IPv4 interno escrito em hexadecimal
  (`::ffff:a2a:70f`) e o nome reverso em `ip6.arpa`. Ficam `fd00::/8` numa explicação, `fe80::1`
  e os endereços fixos da AWS (`fd00:ec2::253`, `fd00:ec2::254`).
- Com `objects.public_ip`: o IP público no nome de uma máquina da AWS
  (`ec2-52-14-88-21.compute-1.amazonaws.com`) e em `ip6.arpa`. A faixa de documentação da
  RFC 9637 (`3fff::/20`) fica.
- Chave de acesso de nota fiscal eletrônica (NF-e, NFC-e, CT-e, MDF-e), 44 dígitos com o CNPJ
  ou CPF do emitente dentro, também em grupos de 4 e como `Id="NFe…"` no XML: só conta com UF,
  mês, CNPJ/CPF do emitente e dígito verificador válidos. Nome do detector: `nfe`.
- Pix copia e cola (BR Code): o código inteiro, que leva a chave Pix, o nome e a cidade de quem
  recebe (antes só a chave era mascarada). Só conta com o CRC válido.
- ID de recurso da AWS solto (`vpc-…`, `subnet-…`, `i-…`, `sg-…`, `eni-…` e outros), com o mesmo
  pseudônimo que já tinha dentro de um ARN.
- Boleto (linha digitável de 47 dígitos), conta de consumo e tributo (48 dígitos) e o código de
  barras de 44, com os dígitos verificadores da FEBRABAN. Nome do detector: `boleto`.
- Código de rastreio (`RR473124829BR`), com o DV da norma S10 da UPU. Nome do detector: `tracking`.
- Matrícula de certidão do registro civil (nascimento, casamento, óbito; 32 dígitos), pela
  estrutura (o DV do CNJ não é publicado). Nome do detector: `certificate`.
- Dado sensível da LGPD (art. 5º, II) no meio do texto, só o valor e só depois de uma expressão
  que o liga a alguém: cor ou raça (`se declara parda`, `mulher negra`), religião (`é
  evangélico`, `membro da Assembleia de Deus`), orientação sexual e identidade de gênero (`é
  bissexual`, `identidade de gênero: mulher trans`), partido político (`filiado ao PSOL`, `vota no
  PT`), sindicato (`sindicalizado no Sindicato dos …`), código da CID-10 depois de `CID` ou
  `diagnóstico` (`CID F32.1, F41.0`) e template de impressão digital (ISO/IEC 19794-2 em base64).
  Valores das listas oficiais (IBGE, Ministério da Saúde, TSE). `a tela é preta`, `a igreja
  católica do bairro`, `o PL 2630` ficam. Nome do detector: `sensitive`.
- Pelo nome do campo, também `identidade de gênero`, `filiação partidária`, `opinião política`,
  `convicção religiosa`, `vida sexual`, `impressão digital`, `raça/cor`, `race`,
  `comorbidades`.
- RG de qualquer estado depois de `RG`, `registro geral` ou `carteira de identidade`
  (`MG-12.345.678`, `1234567 SSP/DF`, 10 dígitos no RS): cada estado define o seu número (Lei
  7.116/1983) e nenhum publica regra de dígito verificador, então conta pelo rótulo. Antes, só o
  formato de São Paulo.
- As maiúsculas e minúsculas do ID no pseudônimo de um nome de objeto de caixa mista
  (`Pedido_Item` → `T_aBcd…`) passam a vir também da chave. Antes vinham de um hash sem a chave,
  o que deixava quem adivinhasse o nome confirmá-lo pela caixa de cada letra. Um teste agora
  confere que todo pseudônimo muda com a chave e que nenhum contém o valor real.
- Duas grafias da mesma pessoa importada (nome completo e primeiro + último nome) dividem o
  pseudônimo e deixam de contar como colisão: o pseudônimo volta à primeira grafia vista. Antes,
  nenhuma voltava e o cliente via o pseudônimo.

### Programa

- Nomes em inglês: detectores e tipos no `config.json` (`segredo` → `secret`, `telefone` →
  `phone`, `tabela` → `table`, `quase` → `quasi`...; a lista está em
  [configuration.md](configuration.md)), rótulos dos pseudônimos (`TELEFONE-6x6fjtkg` →
  `PHONE-6x6fjtkg`, `Pessoa …` → `Person …`), os arquivos de `~/.config/llm-dlp` (`chave` → `key`,
  `pessoas.json` → `people.json`, `vistos.json` → `seen.json`, `enviados.log` → `sent.log`), as
  linhas do log (`substituições` → `replacements`, `RECUSADO` → `REFUSED`...), as mensagens e a
  palavra que o `sudo llm-dlp emergency` pede (`LIBERAR` → `UNLOCK`).
- Os nomes antigos continuam funcionando. Um `config.json` com os nomes de tipo antigos é lido
  como está, e o `llm-dlp reload` o regrava (o comando de instalação também, e o `llm-dlp update`
  da 0.10 em diante; ao voltar para uma versão anterior, ele é convertido de volta). A
  primeira subida copia `chave`, `pessoas.json` e `vistos.json` para os nomes novos e deixa os
  antigos para uma versão anterior. O `llm-dlp audit` lê as linhas do log das duas versões. Com
  os rótulos novos, a primeira mensagem de cada conversa regrava o cache uma vez, como depois de
  qualquer atualização.
- A linha de cada requisição no log diz também quantos valores de cada tipo foram mascarados
  (`types: cpf 2, table 5`) e, quando acontece, os pseudônimos que chegaram ao cliente sem
  voltar a ser o valor real (`NOT UNDONE`) e os que colidiram (`collided`). Só contagens e
  pseudônimos.
- Comando novo `llm-dlp audit [24h|7d]`: resume o log de um período (o que foi mascarado por
  tipo, pseudônimos que voltaram, envios sem máscara, recusas, erros da API).
- O log troca de arquivo aos 10 MB também com o llm-dlp no ar (antes, só quando ele subia): no
  máximo ~20 MB no disco.

### Código

- Identificadores, comentários, nomes de arquivo e mensagens dos testes em inglês. As variáveis
  de ambiente dos testes também: `LLM_DLP_STRESS`, `LLM_DLP_SAMPLE`, `LLM_DLP_GEN_REF`,
  `LLM_DLP_SESSIONS`, `LLM_DLP_VECTORS`, e `LLM_DLP_DEBUG` (`LLM_DLP_DEPURAR` continua valendo).
- O `pseudonym_golden_test.go` grava os pseudônimos de uma chave fixa: falha se uma mudança no
  código mudar algum deles. O domínio da HMAC de cada tipo continua sendo o nome dele até a 0.9
  (`hmacDomain`).
- Os nomes que uma versão usa para falar com outra durante a atualização (`servir`,
  `supervisionar`, `/__llm-dlp/saude`...) continuam em português, porque a atualização a partir
  da 0.9 roda o código da 0.9. A lista está em `swap.go`.

## [0.9.2] - 2026-10-13

Quando o llm-dlp veio da 0.7.0, o supervisor dele ficava sem `supervisor.pid`: o supervisor da
0.7.0 apagava o arquivo ao sair, logo depois de o novo gravá-lo. Sem ele, o `llm-dlp update`
não achava o supervisor e trocava só o proxy, cortando uma resposta em andamento (nenhuma
requisição foi recusada: a porta ficou aberta), o supervisor ficava na 0.8.0, e o
`llm-dlp stop` dizia que não havia supervisor rodando.

### Programa

- Sem o `supervisor.pid`, o llm-dlp acha o supervisor pela trava que ele segura
  (`/proc/locks`) e grava o arquivo de novo. O `update` e o `reload` passam a fazer a troca sem
  pausa e põem o supervisor no binário instalado, e o `stop` o para. Vindo da 0.9.0 ou 0.9.1:
  `llm-dlp update` e depois `llm-dlp reload` uma vez.
- O supervisor grava o `supervisor.pid` de novo sempre que se reexecuta depois de uma troca.
- README e configuração: da 0.8.0, `llm-dlp atualizar` e depois `llm-dlp reload`.

## [0.9.1] - 2026-10-13

Atualizar da 0.8.0 com `llm-dlp atualizar` deixava o `config.json` e os ganchos do Claude Code
com os nomes em português: quem faz a atualização é o código da versão instalada, e a 0.8.0 não
conhece os nomes em inglês. Os nomes antigos continuam funcionando, então nada parou; agora o
`llm-dlp update` e o `llm-dlp reload` fazem a troca mesmo quando a versão já está no ar.

### Programa

- `llm-dlp update`, quando a versão mais nova já está no ar, e `llm-dlp reload` passam o
  `config.json` (guardando `config.json.bak-DATA`) e os ganchos para os nomes da versão
  instalada. Vindo da 0.8.0: `llm-dlp atualizar` e depois `llm-dlp update` uma vez.
- README e configuração: como atualizar a partir da 0.8.0, e a 0.7.0 deixa de ser descrita como
  a última versão sem comando de atualizar.

## [0.9.0] - 2026-10-13

Os comandos, as opções deles e os campos da configuração agora são em inglês (`llm-dlp update`,
`llm-dlp stop`, `internal_domains`...). Os nomes em português continuam funcionando, então não
é preciso mudar nada à mão: atualizar regrava o `config.json` com os nomes novos e troca os
ganchos do Claude Code. As mensagens na tela continuam em português.

### Programa

- Comandos: `install`, `uninstall`, `install-lock`, `uninstall-lock`, `update`, `reload`,
  `stop`, `emergency`, `import-people`, `columns`, `test`, `test-media`, `simulate`, `measure`,
  `last`, `version`, `help`, e os internos `ensure`, `check`, `serve` e `supervise`. Os nomes
  antigos continuam funcionando (os ganchos já instalados chamam `garantir` e `verificar`); o
  `help` lista só os novos.
- Opções: `--yes`, `--version`, `--file`, `--group`, `--name-separator`, `--window` e
  `emergency off` (antes: `--sim`, `--versao`, `--arquivo`, `--grupo`, `--separador-nome`,
  `--janela` e `sair`). As antigas continuam funcionando.
- Campos do `config.json`: `port`, `providers` (`format`), `internal_domains`,
  `allowed_emails`, `allowed_email_domains`, `host_roles`, `extra_patterns` (`label`), `terms`
  (`label`, `values`), `disabled_detectors`, `optional_detectors`, `extra_fields` (`type`,
  `words`), `documents_without_context`, `fail_closed`, `ocr` (`mode`: `mask`, `block` ou
  `allow`; `language`, `max_pages`, `strict_pdf`), `tools_never_unmasked` e `objects`
  (`enabled`, `mask`, `propagate`, `public_ip`). Os nomes em português continuam sendo lidos
  (com os dois, vale o em inglês). O `llm-dlp update` e o comando de instalação regravam o
  arquivo com os nomes novos quando a versão nova já está no ar, guardando o anterior em
  `config.json.bak-DATA`; campos que o llm-dlp não conhece ficam como estão.
- Voltar para uma versão anterior à 0.9.0 (`llm-dlp update --version 0.8.0`, ou o comando de
  instalação com `--version`): o arquivo volta aos nomes em português antes de ela subir, e os
  ganchos voltam para `garantir` e `verificar`. Uma versão anterior ignoraria os campos em
  inglês e deixaria de mascarar o que eles pedem (domínios internos, termos...). Ao voltar, o
  comando de instalação agora deixa a atualização com a versão já instalada, que sabe converter
  o arquivo.
- Sem mudança: os nomes dos detectores (em `disabled_detectors`), os tipos em `extra_fields` e
  em `objects.mask` e `objects.propagate`, os prefixos dos pseudônimos, o log e os endereços
  `/__llm-dlp/`.
- As notas da versão não mandam mais fechar o Claude Code e rodar `llm-dlp stop` depois do
  comando de instalação: desde a 0.8.0 ele atualiza sem tirar do ar.

### Código

- Os testes de `cmd/llm-dlp` rodam com a pasta pessoal num diretório temporário (`TestMain`):
  `install`, `uninstall` e `update` mexem no `~/.claude` e no `~/.local/bin`, e um teste não
  consegue mais tocar nos de verdade.
- `internal/config/old_names.go`: os nomes antigos dos campos, lidos ao carregar e
  convertidos no arquivo nos dois sentidos (`MigrarArquivo`).
- O supervisor continua subindo o proxy como `servir` e se reexecutando como `supervisionar`:
  depois de voltar, uma versão anterior entende esses nomes.

## [0.8.0] - 2026-10-13

Atualizar não tira mais o llm-dlp do ar, e o proxy cair também não: agora quem segura a porta é
o supervisor, que só a empresta ao proxy. O `llm-dlp atualizar` baixa a versão nova, confere,
sobe na mesma porta enquanto a antiga ainda atende, e só então deixa a antiga sair. Não precisa
fechar o Claude Code nem rodar `llm-dlp parar`, e se a versão nova não subir, ele volta sozinho
para a anterior. O comando de instalação faz o mesmo quando o llm-dlp já está instalado. Para
chegar aqui a partir da 0.7.0 ou anterior, rode o comando de instalação uma vez, entre uma
mensagem e outra.

### Programa

- O supervisor abre a porta uma vez e a empresta a cada proxy que sobe, como o systemd faz com
  socket activation. Quando o proxy cai, a porta continua aberta, e as requisições esperam na
  fila até o proxy seguinte (cerca de um segundo). Antes, o proxy segurava a porta: enquanto ele
  estava fora, toda requisição era recusada. Derrubar o proxy com quatro clientes mandando
  mensagens sem parar deu 19.655 "Connection refused" em cerca de um segundo na 0.7.0, e nenhum
  agora. Esse erro chegava à tela porque o Claude Code manda requisições no meio de um turno
  (depois de uma ferramenta) sem passar pelo gancho que sobe o llm-dlp.
- `llm-dlp atualizar [--versao X.Y.Z] [--arquivo ARQ.tar.gz]`: baixa a release (a mais nova ou
  a pedida), confere o SHA-256 com o `checksums.txt` da release, troca o `~/.local/bin/llm-dlp`
  (guardando só a cópia anterior mais recente) e pede a troca ao supervisor: ele sobe a versão
  nova na mesma porta, espera ela avisar que está pronta, e aí o antigo para de aceitar
  conexões, ainda atende o que chegar pelas conexões já abertas (com `Connection: close`),
  termina as respostas em andamento e sai; o supervisor se reexecuta com o binário novo sem
  soltar a porta. Testado com quatro clientes mandando mensagens sem parar e uma resposta de 8
  segundos no meio: nenhuma falhou e nenhuma foi cortada. Se a versão nova não ficar pronta em
  20 s, ela é parada, o binário anterior volta e o proxy antigo nunca saiu do ar.
- O comando de instalação (`curl ... | sh`) roda o `llm-dlp atualizar` quando o llm-dlp já está
  instalado em `~/.local/bin`: sem as perguntas da configuração. O `llm-dlp instalar`, rodado de
  novo com uma versão anterior no ar, troca do mesmo jeito.
- `llm-dlp recarregar` aplica uma mudança no `config.json` sem tirar o llm-dlp do ar (antes:
  `llm-dlp parar`). O `make instalar` o roda depois de instalar.
- O instalador subia o supervisor a partir do arquivo que estava rodando, que o `install.sh`
  apaga no fim: o proxy continuava no ar, mas na primeira vez que precisasse reiniciar, o
  supervisor não achava o programa e ficava com a trava, e nada subia o llm-dlp de novo até um
  `llm-dlp parar`. Agora o supervisor roda sempre o `~/.local/bin/llm-dlp`.
- O `vistos.json` e o `pessoas.json` não perdem mais o que outro processo gravou: cada gravação
  junta o que foi gravado desde a última leitura. Isso vale para as duas versões no ar juntas
  durante uma atualização, e para o `llm-dlp importar-pessoas` com o proxy no ar (antes, a
  gravação seguinte do proxy apagava as pessoas importadas).
- O `llm-dlp status` sugere `llm-dlp atualizar` quando o binário instalado não é o que está no
  ar.

## [0.7.0] - 2026-10-12

Mascaramento medido com canários (dados falsos plantados em 21 formatos de saída, 8 tipos de
arquivo de código e uma conversa de várias rodadas, pelo proxy inteiro): o nome de uma pessoa
num registro JSON, XML, dict Python ou YAML é mascarado, as chaves de um objeto JSON continuam
chaves, um nome que a requisição provou continua mascarado no código e nas saídas seguintes, e
cada grafia de um termo ou código de usuário volta na sua caixa. Os nomes em claro no código
caíram de 107 para 75 de 253 numa conversa nova, e de 73 para 41 com os dados antes na
requisição. Depois de atualizar, feche o Claude Code e rode `llm-dlp parar`.

### Mascaramento

- Um campo chamado só `nome` ou `name` é nome de pessoa quando o mesmo registro tem outro
  campo pessoal (`cpf`, `telefone`...), como já era numa tabela: o `{...}` em volta (JSON, dict
  Python), as linhas vizinhas com o mesmo recuo (XML, YAML, `.env`) ou a linha (log). Um
  registro sem nenhum o deixa como está (`<produto><nome>CDB</nome>...`). Antes, o nome saía em
  claro em JSON, JSON minificado, XML e dict Python.
- As chaves de um objeto JSON (e de YAML, `.env`, `chave=valor`) nunca são tomadas como os
  nomes de um inventário. Com um comando que cita um substantivo no plural
  (`curl .../api/clientes/1`), `"email"` e `"telefone"` eram mascarados como objetos e o modelo
  via o JSON sem as chaves.
- Em dado (uma saída de comando, um arquivo lido), um nome de recurso que a requisição provou
  (tabela, schema, banco, servidor, serviço...) continua mascarado dentro de um comentário e em
  outro arquivo lido: a prova é de onde o valor veio. Antes, `# carrega as propostas do dia`
  num `.py` lido depois de um `\dt` saía em claro. Na prosa nada muda: a mensagem do usuário, a
  resposta do modelo e, fora dos blocos de código, um arquivo de documentação (`.md`, `.mdx`,
  `.txt`, `.rst`, `.adoc`), onde uma palavra comum continua palavra. Colunas e índices ficam
  como antes (uma coluna `status` não mascara `status` em outro arquivo).
- Um marcador de comentário entre aspas é texto, `--` colado numa letra é opção de comando, e
  `#` seguido de dígito é número: `pg_dump --schema=comercial` e `"lote #3: propostas"`
  liberavam nomes como se fossem comentário.
- A aplicação de uma URN do DataHub (`urn:li:application:SGP`) é mascarada, mesmo uma sigla
  curta.
- O pseudônimo de um termo, de um código de usuário e dos outros tipos com rótulo segue a caixa
  do valor, como o de uma pessoa: `SGP` vira `EMPRESA-abcd2345`, `sgp` vira `empresa-abcd2345`,
  `SgP` vira `Empresa-abcd2345` (o mesmo identificador). Antes, toda grafia voltava como a
  primeira vista, e um comando ou arquivo que o modelo escrevia podia voltar com a caixa trocada
  (`echo SgP sgp SGP` virava `echo SgP SgP SgP`). Achado em 21 de 6.841 gravações repetindo uma
  sessão real.

### Código

- O `llm-dlp simular` conta as formas novas de pseudônimo quando procura um pseudônimo que
  ficou numa gravação.

## [0.6.0] - 2026-10-11

Três correções do mascaramento, achadas repetindo uma sessão real do Microsoft Fabric: um nome
já conhecido passa a ser mascarado também dentro de uma palavra maior, o GUID de um campo de id
passa a ser mascarado, e o nome de uma pessoa volta na caixa em que foi escrito (antes, um
arquivo podia ser gravado com o nome em outra caixa). O README põe os clientes em beta numa
tabela só e diz quais rodam no VS Code. Depois de atualizar, feche o Claude Code e rode
`llm-dlp parar`.

### Mascaramento

- Um nome que a conversa já conhece passa a ser mascarado também dentro de uma palavra maior:
  a tabela `VENDA2024`, mascarada no SQL, saía em `LH_BRONZE_VENDA2024`, `T01_VENDA2024.sql`
  ou `DW$VENDA2024_HIST`. Agora a palavra inteira é mascarada. Só vale assim o nome com cara de
  identificador (com `_`, dígito ou mistura de caixa): uma coluna `status` não mascara o
  `user_status` do código.
- Um GUID que é o valor de um campo de id (`workspace_id: X`, `"lakehouseId": "X"`,
  `tenant_id=X`, `"id": "X"`) é mascarado como documento, e dali em diante nos outros lugares
  em que aparece. Ele identifica um recurso do cliente (um workspace do Fabric, um tenant do
  Azure). O id de um aplicativo OAuth (`client_id`, `app_id`) fica: ele não é segredo (RFC 6749,
  2.2).
- O pseudônimo de uma pessoa segue a caixa do nome: `MARIA DA SILVA` vira `PESSOA ABCD2345`,
  `maria da silva` vira `pessoa abcd2345`, `Maria da Silva` continua `Pessoa abcd2345` (a mesma
  pessoa, o mesmo identificador). Antes, toda grafia voltava como a primeira vista na conversa:
  um relatório que o modelo escrevia com o nome em maiúsculas era gravado com outra caixa, e um
  Edit com o nome em maiúsculas no `old_string` podia não casar com o arquivo.
- Os casos foram achados repetindo uma sessão real do Microsoft Fabric: nomes que o próprio
  llm-dlp tinha reconhecido saíam sem máscara em saídas de comando novas, e 7 das 7.790
  gravações de arquivo e comandos voltavam com um nome de pessoa em outra caixa.

### Programa

- O README põe os clientes em beta numa tabela só, com a linha exata de cada cliente e conta,
  e diz quais rodam no VS Code: o Gemini CLI no terminal do editor, a extensão do Codex (o
  mesmo `config.toml` do CLI), o opencode e o Copilot. Uma extensão não passa pela função do
  `~/.bashrc`: rode `llm-dlp garantir` uma vez depois de ligar a máquina. A extensão Gemini
  Code Assist não é atendida.

### Código

- O `llm-dlp simular` repete um arquivo de subagente (`subagents/agent-*.jsonl`). Antes, toda
  mensagem dele era pulada como conversa lateral, e o arquivo dava zero requisições e zero
  problemas. Um arquivo sem nada para repetir agora avisa.
- O `llm-dlp simular` deixa de contar como ida e volta diferente um texto cortado pela API
  falsa (um texto logo abaixo de 4000 bytes que cresce ao ser mascarado).

## [0.5.0] - 2026-10-11

Assinaturas, **em beta**: o Gemini CLI com conta Google e o Codex com assinatura do ChatGPT (ou
chave da OpenAI) passam a ir pelo llm-dlp; ainda não foram conferidos com uma conta de verdade.
Uma string de conexão passa a ter cada parte mascarada com o seu tipo. Para o Claude Code, só
essa correção da máscara muda. Depois de atualizar, feche o Claude Code e rode `llm-dlp parar`.

### Mascaramento

- Uma string de conexão (`esquema://usuario:senha@servidor/banco`) tem cada parte mascarada
  com o seu tipo. Antes, uma senha sem símbolo era lida como e-mail junto com o servidor
  (`senha@servidor` virava um pseudônimo de e-mail), uma senha terminada em `!` ou `.` deixava
  esse caractere em claro, e o servidor tinha um pseudônimo dentro da URL (`host_...`) e outro
  no resto do texto (`....invalid`). Agora o servidor do domínio interno tem o mesmo
  pseudônimo em todo lugar, e a senha é mascarada até o `@`.

### Programa

- **Assinaturas, em beta.** Além da chave de API, o llm-dlp passa a atender o Gemini CLI com a
  conta Google (*Login with Google*: a API do Code Assist, em `/codeassist`) e o Codex com a
  assinatura do ChatGPT (em `/chatgpt`). Leitor novo para o formato Responses da OpenAI, o do
  Codex, também com chave (`/openai/v1/responses`): o texto, as chamadas de ferramenta, o patch
  do `apply_patch` e o comando do shell são desmascarados na volta, também em streaming.
  Provedores embutidos `codeassist` e `chatgpt`. Os métodos da conta do Code Assist (login,
  cota) passam como vieram. Ainda não conferido com uma conta de verdade. O Claude Code já
  era atendido com assinatura (Pro, Max); o GitHub Copilot com a assinatura do GitHub não é,
  porque não deixa trocar o endereço.
- O README explica como ligar cada cliente. A instalação fica dividida em instalar o llm-dlp
  (igual para todos) e ligar o seu cliente: o Claude Code (passos 4 e 5) ou os clientes em
  beta, com uma tabela de onde cada um lê o endereço (`~/.bashrc`, `~/.codex/config.toml`,
  `opencode.json`), as linhas de cada tipo de conta e uma função no `~/.bashrc` que sobe o
  llm-dlp antes de abrir o cliente, e diz quais tipos de conta passam pelo llm-dlp e quais saem sem máscara
  (Vertex AI, Copilot com a assinatura do GitHub). Os passos do Gemini CLI com chave de API e do
  opencode foram seguidos como estão escritos numa pasta pessoal limpa.
- O exemplo do topo do README passa a ir passo a passo: o arquivo que o agente lê, o que a
  API recebe, o que o modelo escreve e o que é gravado no seu disco, com uma tabela de como
  ler cada pseudônimo.
- O `llm-dlp instalar` diz que os passos 4 e 5 são só do Claude Code. Quem responde não deixa
  de receber um ✗ mandando refazer esses passos: o resumo os mostra como não configurados (–)
  e aponta para o README.

## [0.4.0] - 2026-10-11

Outros clientes e APIs, **em beta**: a API do Gemini e o formato Chat Completions da OpenAI,
para o Gemini CLI, o opencode e o GitHub Copilot com a sua chave. O Claude Code segue o único
cliente estável, e nada muda no que é mascarado para ele. Os clientes em beta são apontados à
mão e não têm trava: leia [Outros clientes e APIs (beta)](configuration.md#outros-clientes-e-apis-beta)
antes de usá-los. Depois de atualizar, feche o Claude Code e rode `llm-dlp parar`.

### Programa

- **API do Gemini (beta).** O llm-dlp passa a atender também a API do Gemini (`generateContent`,
  `streamGenerateContent`, `countTokens`), no caminho `/gemini` da mesma porta: aponte o
  Gemini CLI, o opencode ou uma SDK do Google para `http://127.0.0.1:8787/gemini`. Vale o
  mesmo mascaramento do Claude Code (texto, chamadas e resultados de ferramenta, instruções
  do sistema, imagens e PDFs, memória da conversa, histórico congelado), e a resposta é
  desmascarada também em streaming. Campo novo na configuração: `provedores`; outras APIs
  podem ser acrescentadas ali (sem leitor próprio, todo texto é mascarado). Conferido com o
  Gemini CLI e o opencode rodando, e o `llm-dlp ultima` mostra também a conversa do Gemini.
  Ver [configuration.md](configuration.md#outros-clientes-e-apis-beta).
- **Formato da OpenAI, Chat Completions (beta).** A requisição cujo caminho termina em
  `/chat/completions` passa a ser lida como as outras (instruções, ferramentas, texto,
  chamadas e resultados de ferramenta, imagens), e a resposta é desmascarada, também em
  streaming. Com isso, o GitHub Copilot com a sua chave e as APIs da OpenAI, da xAI e a do
  Gemini compatível com a OpenAI passam pelo llm-dlp: provedores embutidos `openai` e `xai`,
  ao lado do `gemini`. Conferido com o Copilot CLI rodando.
- A documentação marca os outros clientes como beta (o Claude Code segue o único estável) e
  diz o que vale para cada um: só o Claude Code é apontado,
  iniciado, conferido e travado pela instalação; os outros clientes são apontados à mão e não
  têm trava.
- Quando a API não responde, o log e o erro não mostram mais o endereço da requisição, que
  pode levar a chave da API (`?key=...`).

### Código

- O `llm-dlp simular` passa a conferir também a volta do que o modelo escreve: a API falsa
  devolve a última chamada de ferramenta com os pseudônimos, e a repetição conta as chamadas
  que voltaram diferentes da original (o que iria para um arquivo gerado ou para o terminal).

## [0.3.1] - 2026-10-11

Correções de segurança no próprio programa. Nada muda no que é mascarado. Depois de
atualizar, feche o Claude Code e rode `llm-dlp parar`.

### Programa

- **A porta local está protegida de páginas da web e de outros usuários.** Uma página
  aberta no navegador conseguia chegar à porta por DNS rebinding e ler a última conversa
  (mascarada), e qualquer outro usuário da máquina também; um programa que ocupasse a porta
  com o llm-dlp fora do ar se passava por ele e receberia a conversa sem máscara e a
  credencial da API. Agora a requisição que não usa o endereço local, ou que vem de uma
  página da web, é recusada; a última conversa só sai para quem lê a chave
  (`llm-dlp ultima`); e a cada mensagem o llm-dlp tem de provar que é ele, respondendo a
  um desafio com a chave. Um caminho com quebra de linha não forja mais uma linha no log.
  **Depois de atualizar, feche o Claude Code e rode `llm-dlp parar`:** o processo antigo
  não sabe dar a prova, e as mensagens ficam barradas até ele ser trocado.
- **Aviso quando a configuração reduz a proteção.** O `config.json` é do usuário, então um
  comando rodado pelo agente consegue mudá-lo: desligar um detector, ou apontar o `upstream`
  para outro servidor, que receberia as mensagens e a credencial da API. Enquanto for o
  caso, o Claude Code mostra um aviso a cada mensagem, e o `llm-dlp status` também. O
  `llm-dlp instalar` também passa a proibir o agente de citar a pasta do llm-dlp num
  comando (rode de novo para receber a regra).
- Imagem que declara um tamanho enorme, ou PDF cujo texto se expande sem fim, não estoura
  mais a memória nem derruba o llm-dlp: é recusada pelo cabeçalho (mais de 16000 pontos de
  lado ou 64 milhões no total), e a saída dos programas externos tem teto.
- Compilado com o Go 1.27.2, que corrige 9 vulnerabilidades conhecidas da biblioteca
  padrão (HTTP e TLS) presentes nos binários até a 0.3.0; dependência `xz` atualizada.
- **Notas da release no GitHub:** as releases 0.2.1 e 0.3.0 foram publicadas com as notas
  vazias (uma configuração do GoReleaser pulava o passo que as lê). As notas voltam a ser a
  seção da versão neste arquivo (a versão em inglês), agora com cada parágrafo numa linha
  só, links que funcionam na página da release e o comando de instalação no fim. Release
  sem notas faz o workflow falhar.

## [0.3.0] - 2026-10-11

A conversa não fica mais presa: nem em 429 depois de uma atualização, nem numa imagem que
não dá para verificar.

### Mascaramento

- **O texto do próprio Claude Code não é mais estragado.** A palavra comum que a conversa
  aprendeu como nome (uma tabela `billing`) era trocada dentro do system e das definições
  das ferramentas sempre que eles eram mascarados de novo (depois de uma atualização, por
  exemplo). Com a palavra trocada dentro do `x-anthropic-billing-header`, a API deixava de
  reconhecer o Claude Code e respondia 429 a toda mensagem daquela conversa, que ficava
  presa. Agora a palavra comum fica como palavra nesses textos (como na prosa das
  mensagens), e o cabeçalho e a frase de identidade do Claude Code passam intactos, com
  prova pela forma exata. A conversa que estava presa volta a funcionar depois de atualizar.

### Programa

- **Imagem ou PDF que não dá para verificar não trava mais a conversa.** Sem o OCR instalado
  (ou com arquivo ilegível, ou com `ocr.modo = bloquear`) a requisição inteira era recusada
  com 502, em toda mensagem dali em diante. Agora o arquivo é retirado da requisição
  (continua sem sair nunca) e o modelo recebe um aviso no lugar, com o motivo e o comando
  que instala o OCR. O `llm-dlp status` avisa quando falta o OCR.
- O log mostra o tipo e a mensagem de um erro da API (`| API: rate_limit_error: ...`).
- `llm-dlp --help` (e `ajuda`, `-h`) mostra a ajuda e sai com 0; comando desconhecido diz
  qual foi. A ajuda mostra a versão, como atualizar, o `parar` e onde ficam a configuração
  e o log.
- Menos arquivos na raiz do repositório: `CONTRIBUTING.md` e `SECURITY.md` foram para
  `.github/` (o GitHub continua mostrando os dois), e o README em português para
  `docs/pt-BR/README.md`.

## [0.2.1] - 2026-10-10

Instalação com um comando e releases empacotadas como os projetos grandes fazem. Nada muda no
que é mascarado.

### Programa

- **Instalação em uma linha:** o `install.sh` (o comando está no README) baixa o binário da
  versão mais recente para a máquina (amd64 ou arm64), confere a soma SHA-256 com a publicada
  e roda o `llm-dlp instalar`. Opções: `--version X.Y.Z`, `--no-setup`, `--bin-dir DIR`.
- **Pacotes da release:** cada release passa a ter um `.tar.gz` por arquitetura (o binário, a
  LICENSE, o README e o CHANGELOG) e um `checksums.txt`, montados pelo GoReleaser. As releases
  até a 0.2.0 publicavam o binário solto e um arquivo `SHA256SUMS`.
- **Documentação em inglês e em português:** `README.md` e `docs/` estão em inglês;
  `README.pt-BR.md` e `docs/pt-BR/`, em português.
- `CONTRIBUTING.md` e `SECURITY.md` (como contribuir e como relatar uma vulnerabilidade).

### Código

- O histórico de commits foi reescrito em inglês, seguindo o
  [Conventional Commits](https://www.conventionalcommits.org). O conteúdo de cada commit é o
  mesmo; os hashes dos commits anteriores a esta versão mudaram.
- `make dist` ensaia a release com o GoReleaser; o CI também valida o `install.sh`
  (shellcheck) e a configuração do GoReleaser.

## [0.2.0] - 2026-10-10

Nome público só fica em claro com prova. Toda regra que deixa um nome passar exige agora uma
garantia de estrutura, de falha fechada: na dúvida, mascara.

### Mascaramento

- **Escrita não latina:** nome de banco, schema, tabela e usuário em qualquer escrita
  (cirílico, japonês, acentos) é mascarado. Usuário com ponto (`maria.souza`) também.
- **Traceback** (Python, Java, JavaScript, Go): a linha que aponta para o projeto tem a função,
  o módulo, o código e o valor citado no erro mascarados. A linha de dependência ou da
  biblioteca padrão (`pandas`, `net/http`) fica legível.
- **Imagem de contêiner:** só fica em claro com duas chaves, organização fornecedora **e** nome
  de software público. `acme/redis` e a imagem construída no projeto são mascaradas.
- **Software público na conversa:** o nome padrão de um software (`airflow`, `datahub`,
  `superset`) deixa de ser espalhado pela conversa quando ela mostra que é o software
  (instalado, importado, rodado como imagem pública ou executado). Palavra de dicionário, nome
  definido no projeto e termo cadastrado nunca recebem essa prova.
- **Origem pelo caminho:** os leitores de caminho param no pacote público
  (`site-packages/sqlalchemy/...` fica inteiro) e continuam lendo as pastas do projeto debaixo
  de usuário público (`/home/ubuntu/<projeto>`). Arquivo de dependência lido não ensina nomes à
  conversa. Estar numa pasta de dependências não basta: o pacote, o módulo ou o grupo tem de
  ser público, porque o pacote interno do cliente fica nas mesmas pastas.
- **docker-compose:** serviço reconhecido por qualquer chave da especificação, inclusive `<<`.
- **Listagem de linha de comando:** o comando à vista (`$ airflow dags list`) diz que o
  cabeçalho é da ferramenta; num tipo fora do vocabulário, só a coluna de identidade
  (`dag_id`) é tipada. Tabela de `kubectl get` e `docker ps` é lida pelo cabeçalho.
- **Hosts de serviço gerenciado de nuvem** (RDS, Redshift, S3, Azure, GCP): o rótulo do cliente
  é mascarado e o domínio do provedor fica.

### Programa

- Versão e mudanças organizadas: `llm-dlp versao`, este arquivo, binários prontos em cada
  release e `make release` (ver [versioning.md](versioning.md)).
- Estimativa de uso de CPU, memória e disco em
  [how-it-works.md](how-it-works.md#quanto-usa-da-máquina).

### Código

- Listas geradas em `internal/mask/data/`; arquivos do pacote `mask` agrupados por prefixo
  (`detectores_`, `campos_`, `leitor_`, `chamada_`, `decisor_`, `memoria_`, `vocab_`). O mapa e
  a tabela do que fica guardado estão em [development.md](development.md).
- Listas novas, geradas: `public_software.txt` e `vendors.txt` (repositórios do GitHub
  com 3000 estrelas ou mais), `official_images.txt` (Imagens Oficiais do Docker) e
  `dictionary_words.txt` (50 mil palavras mais frequentes de português e inglês).

### Medido

Contra a 0.1.0, em nomes rotulados à mão de 16 repositórios públicos:

| | 0.1.0 | 0.2.0 |
|---|---|---|
| Nome do dono que vazou (5 repositórios, validação) | 6 de 22 | 1 de 22 |
| Nome do dono que vazou (11 repositórios novos, validação) | 2 de 9 | 1 de 9 |
| Nome público mascarado à toa (5 repositórios, validação) | 138 de 207 | 124 de 207 |
| Nome público mascarado à toa (11 repositórios novos, validação) | 40 de 122 | 46 de 122 |
| Palavras trocadas em prosa / código / configuração | 2,29% / 2,06% / 5,47% | 2,16% / 1,70% / 4,91% |
| Tempo para 21 MB de texto novo (2 núcleos) | 75 s | 77 s |
| Memória ao subir | 21 MB | 25 MB |

O excesso que subiu nos repositórios novos são rótulos de catálogo de tradução (`"User":
"Gebruiker"`): uma regra que os liberava também liberava dado de planilha exportada, e foi
recusada.

## [0.1.0] - 2026-10-08

Primeira versão: tudo o que existia até o commit `815a6ac`.

### Mascaramento

- **Dado pessoal e segredo pelo formato:** e-mail, CPF, CNPJ, telefone, cartão, documentos com
  dígito verificador, tokens e chaves (gitleaks), senhas comuns.
- **Dado pessoal pelo nome do campo:** `campo: valor`, JSON, SQL, XML, CSV, planilha, tabela de
  terminal.
- **Pessoas cadastradas** (nomes, e-mails, códigos), guardadas só como hash.
- **Nomes de recursos internos pela estrutura:** SQL e DDL, tabelas em qualquer desenho,
  esquemas, strings de conexão, YAML/JSON de Kubernetes, Helm, compose, Terraform, Ansible e
  CI, chave-valor, endereços, caminhos, pacotes, nuvem, código e linha de comando.
- **Normalização de transporte:** o conteúdo é lido como chega (número de linha, `grep`, diff,
  ANSI, JSON escapado).
- **Memória da conversa e rastreamento:** o nome decidido onde entra vale em toda a conversa;
  o que o modelo escreveu antes de qualquer dado é público; palavra comum fica como palavra na
  prosa.
- **Imagens e PDFs:** OCR e tarja preta.

### Programa

- Proxy local para o Claude Code, com falha fechada, volta exata e texto já enviado congelado
  (o cache da API continua valendo).
- `llm-dlp instalar` (programa, configuração, OCR, Claude Code, trava), `status`, `parar`,
  `emergencia`, `desinstalar`.
