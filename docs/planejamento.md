# mserver — Planejamento

Documento de resumo das decisões de arquitetura e do roadmap do projeto.
Decisões abertas estão listadas ao final e devem ser registradas aqui (ou
em ADRs em `docs/adr/`) assim que forem tomadas.

## 1. Visão geral

O **mserver** é um servidor de mídia escrito em Go que pretende substituir, em
um único projeto:

| Ferramenta | Papel no mserver                                      |
| ---------- | ----------------------------------------------------- |
| Jellyfin   | Biblioteca, streaming e transcode (prioridade máxima) |
| Sonarr     | Aquisição de séries                                   |
| Radarr     | Aquisição de filmes                                   |
| Prowlarr   | Gerenciamento de indexers                             |
| Bazarr     | Legendas                                              |
| Lingarr    | Tradução de legendas                                  |

O software é projetado para rodar com máxima eficiência em hardwares modestos
(como Raspberry Pi e NAS de entrada) e escalar horizontalmente em clusters:
pode ser executado como **monolito autossuficiente** (todos os módulos em um
único processo, usando SQLite e memória) ou como **microserviços distribuídos**
(cada módulo, ou grupo de módulos, em instâncias diferentes com Postgres, NATS
e Valkey), a partir do **mesmo binário**, alterando apenas variáveis de ambiente
ou flags CLI.

## 2. Princípios

1. **Monolito modular com fronteiras rígidas e transição transparente para microserviços.**
   Cada módulo é isolado logicamente e pode ser extraído para outra instância
   sem alterar os demais.
2. **Recursos isolados por módulo.** Cada módulo tem seu próprio banco (arquivo
   SQLite ou schema Postgres), migrations e estado. Não existe `JOIN` entre
   módulos; referências entre módulos são feitas apenas por ID.
3. **Autossuficiente no modo monolito.** Por padrão tudo roda localmente, sem
   dependências de infraestrutura externa (SQLite, cache/KV em memória, pubsub
   em memória, chamadas entre módulos em memória e disco local).
4. **Infraestrutura atrás de interfaces enxutas.** A infraestrutura (`kv`,
   `pubsub`, `database`) permite alternar entre o modo autossuficiente
   (`sqlite`, `memory`) e o modo distribuído (`postgres`, `nats`, `valkey`) via
   variáveis de ambiente. Pastas de backends ainda não implementados **não**
   devem ser criadas antecipadamente.
5. **Compatibilidade com Jellyfin antes de UI própria, com isolamento total no plugin.**
   Os clientes oficiais do Jellyfin são a interface de reprodução; a
   administração é feita via API (curl/Bruno) até existir uma interface web.
   **Todas as regras de negócio, conceitos, DTOs, convenções e adaptações
   referentes ao Jellyfin devem estar 100% isoladas no plugin
   `plugins/api/jellyfin`.** Nenhum módulo do núcleo pode conhecer conceitos ou
   regras do Jellyfin, e **nenhum conceito não discutido deve existir fora do
   plugin do Jellyfin** — tudo o que for preciso adicionar fora de
   `plugins/api/jellyfin` deve ser discutido e aprovado antes.
6. **Processamento multimídia exclusivamente pela suíte FFmpeg.** Probe,
   transcode, geração de imagens, conversão de legendas e qualquer outro
   processamento de mídia usam os binários `ffprobe`/`ffmpeg` disponíveis
   no ambiente, executados como processos externos.
7. **Arquitetura orientada a eventos (ECST) com `Resync` universal (sem Outbox).**
   Os módulos propagam estado de forma assíncrona via _Event-Carried State
   Transfer_ (ECST): cada módulo mantém projeções locais dos dados de que
   precisa em seu próprio banco, evitando chamadas em cascata (_fan-out_
   síncrono) no caminho crítico de leitura. Os eventos são publicados
   diretamente no `pubsub` após o commit (com retry em memória no monolito e
   persistência/deduplicação nativa no NATS JetStream nos microserviços),
   eliminando o custo de escrita dupla de uma tabela `outbox`. Todo módulo
   produtor de estado persistido expõe uma operação de `Resync` para republicar
   seu estado atual, garantindo consistência eventual, _cold start_ e autocura
   (o `playback`, cujo estado vive só em memória, é a exceção).
8. **Contratos Protobuf + ConnectRPC (In-Process Transport no monolito, HTTP/2 `h2c` nos microserviços).**
   Toda comunicação síncrona de controle entre módulos é definida em arquivos
   `.proto` e implementada com **ConnectRPC** (`connectrpc.com/connect/v2`). No
   monolito, os módulos comunicam-se via **`In-process transport`** do Connect
   (`connectinprocess`), em memória, sem serialização e sem rede. Nos
   microserviços, usam o cliente
   ConnectRPC no protocolo padrão sobre HTTP/2 (`h2c`). O código dos módulos
   consumidores é 100% idêntico nos dois modos (ver seção 4).

## 3. Decisões de arquitetura

| Tema                       | Decisão                                                                                                                                                                                                                                                                        |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Linguagem                  | Go 1.27.3                                                                                                                                                                                                                                                                      |
| Modo de execução           | Mesmo binário; módulos habilitados via `MSERVER_MODULES` (padrão: `all`) e backends selecionados por variáveis de ambiente                                                                                                                                                     |
| Contratos entre módulos    | **Protobuf (`.proto`)** em `proto/<módulo>/v1/`, compilados via `buf` (`protoc-gen-go` + `protoc-gen-connect-go`) para `gen/<módulo>/v1`                                                                                                                                       |
| Comunicação síncrona (RPC) | **ConnectRPC**: `In-process transport` (`connectinprocess`) quando o módulo de destino é local no monolito; **HTTP/2 (`h2c`) no protocolo padrão do Connect** (Protobuf binário) quando remoto. Aceita também JSON via `POST` para administração (`curl`/Bruno)                |
| Plano de dados (bytes)     | **HTTP puro** servido pelo módulo `storage` em `GET /locations/{LocationID}/files/{FileID}` (com `Range` / `sendfile` zero-copy no MVP; `PUT` no pós-MVP); bytes brutos de mídia nunca trafegam por Protobuf/ConnectRPC nem pelo `pubsub`                                      |
| Sincronização de estado    | **ECST (Event-Carried State Transfer)** via publicação direta no `pubsub` (sem tabela `outbox`) + operação `Resync` nos módulos produtores com estado persistido (não no `playback`, só em memória) para _cold start_, reconciliação e autocura                                |
| Descoberta de serviços     | Se `MSERVER_<MÓDULO>_URL` estiver vazio e o módulo rodar no processo, usa o `connectinprocess`; se definido, usa o endereço remoto informado via `h2c`                                                                                                                         |
| Banco de dados             | Implementação única em SQL manual direto em `store/` (com queries base em `store/queries.go`) usando `*sql.DB` puro e placeholders `$1, $2`. Suporta **SQLite** (1 arquivo `<módulo>.db` por módulo no monolito) e **Postgres** (1 `SCHEMA` por módulo), mudando só migrations |
| Cache / KV                 | Estado efêmero compartilhável (ex.: sessões do `playback`) usa `internal/kv` (`kv.Store`): `kv/memory` no monolito e `kv/valkey` nos microserviços, mapeado 1:1 para comandos atômicos nativos (`GET`, `SET NX PX`, `SET XX PX`, `DEL`, `SCAN`)                                |
| PubSub / Eventos           | Interface em `internal/pubsub` (`Publisher`/`Subscriber` com `consumer` group e `Message.ID` UUIDv7): `pubsub/memory` (com retry em memória) no monolito e `pubsub/nats` (**NATS JetStream** com deduplicação por `Nats-Msg-Id`) nos microserviços                             |
| Storage                    | Módulo próprio implementando um VFS; pode rodar isolado diretamente no NAS enquanto os demais módulos escalam em outras máquinas; driver inicial: disco local (`os.Root`)                                                                                                      |
| Identificação de arquivos  | `FileID` opaco e ordenável (**UUIDv7**), gerado pelo `storage` na indexação; `ffprobe` roda como analisador do `storage` no MVP e hashes (`sha256`, `ed2k`) entram no pós-MVP (seção 7)                                                                                        |
| API do projeto             | As próprias rotas ConnectRPC dos módulos montadas na raiz do servidor (que aceitam tanto Protobuf/gRPC quanto HTTP `POST` com JSON para administração via `curl`/Bruno e futura UI web)                                                                                        |
| Processamento multimídia   | `ffprobe` (analisador no `storage` no MVP) e `ffmpeg` (no `transcoder` no pós-MVP, com controle de processo via `SIGSTOP`/`SIGCONT`/`SIGTERM`) disponíveis no ambiente, via `exec`                                                                                             |
| FFmpeg ausente             | Módulos que dependem dele (`storage` com `ffprobe` no MVP; `transcoder` com `ffmpeg` no pós-MVP) não inicializam; o processo falha no startup com erro claro                                                                                                                   |
| Clientes de download       | **Não** haverá cliente embutido; apenas integração com clientes externos (Transmission, qBittorrent, SABnzbd) via plugins                                                                                                                                                      |
| Plugins                    | Inicialmente apenas internos (compilados no binário); suporte a plugins externos no futuro                                                                                                                                                                                     |
| Metadados                  | **Fora do MVP.** O MVP implementa apenas o fallback baseado em nomes de arquivos (seção 8)                                                                                                                                                                                     |
| Compatibilidade Jellyfin   | Alvo: API do **Jellyfin 12.4.0** (`docs/jellyfin-openapi-stable.json`); cliente alvo do MVP: **`jellyfin-web`** (`jellyfin-android` e `jellyfin-tizen` no pós-MVP)                                                                                                             |
| Licença                    | Apache 2.0                                                                                                                                                                                                                                                                     |

## 4. Comunicação entre módulos

### 4.1 Estratégia (Pilar 1: ConnectRPC + Protobuf + In-Process Transport)

- Usa-se o **connect-go v2** (`connectrpc.com/connect/v2`), cujo núcleo não
  depende de `net/http`: o servidor é um `*connect.Server`, o transporte HTTP
  vem de `connecthttp` e as chamadas em memória vêm de `connectinprocess`. As
  mensagens e métodos gerados usam a assinatura simples
  `func(ctx, *Request) (*Response, error)`, sem `connect.Request[T]`.
- Cada módulo define seu contrato síncrono em um arquivo Protobuf
  (`proto/<módulo>/v1/<módulo>.proto`). O `buf generate` (`make generate`)
  gera em `gen/<módulo>/v1` as structs de mensagem e o pacote
  `<módulo>v1connect`, contendo as interfaces `<Service>Handler` (servidor) e
  `<Service>Client` (cliente), `Register<Service>Handler` e
  `New<Service>Client`. O código gerado é versionado, para que o build não
  dependa do `buf`.
- O pacote `handler/` de cada módulo implementa `<Service>Handler`. O
  `module.go` o registra no `*connect.Server` recebido do `cmd/server` via
  `<mod>v1connect.Register<Service>Handler(server, h)`.
- O `cmd/server` cria um único `*connect.Server` (com os interceptors comuns) e
  o expõe por HTTP com `connecthttp.Mount(mux, server)`, que instala uma rota
  `/<pacote.v1.Service>/<Método>` por procedimento. Os mesmos handlers
  atendem os clientes remotos (HTTP) e as chamadas em memória.
- **Modo Monolito (`In-process transport`):** quando o módulo de
  destino está habilitado no mesmo processo e `MSERVER_<MÓDULO>_URL` não está
  definido, o `cmd/server` cria um transporte `connectinprocess` sobre o
  `*connect.Server` e instancia o `<Service>Client` sobre esse transporte:

  ```go
  server := connect.NewServer()

  // 1. O módulo registra seu handler no server
  pingv1connect.RegisterPingServiceHandler(server, &pingServer{})

  // 2. Inicializa o transporte inprocess sobre o server
  transport := connectinprocess.New(server)

  // 3. Cria o cliente usando este transporte
  client := pingv1connect.NewPingServiceClient(connect.NewClient(transport))
  ```

  Assim, todas as chamadas `client.<Método>()` ocorrem em memória, **sem
  serialização Protobuf/JSON e sem passar pela pilha TCP/HTTP** (as mensagens
  são copiadas pelo `ProtoCopy` padrão do Connect), preservando 100% da
  semântica e dos interceptors do ConnectRPC.

- **Modo Microserviços (Remoto via HTTP/2 `h2c` no protocolo padrão Connect):**
  quando `MSERVER_<MÓDULO>_URL` está definido, o `cmd/server` cria o transporte
  `connecthttp.NewTransport(h2cClient, url)` e instancia o cliente gerado
  (`New<Service>Client(connect.NewClient(transport))`). O `h2cClient` é um
  `*http.Client` cujo `http.Transport` tem `Protocols` com
  `SetUnencryptedHTTP2(true)` (HTTP/2 Cleartext). Usa-se o **protocolo padrão
  do Connect** (Protobuf binário por padrão), sem `gRPC`/`gRPC-Web`.
- **Exposição HTTP e Montagem:** o servidor sobe um listener HTTP/1.1 + HTTP/2
  Cleartext (`h2c`) e monta:

  | Montagem                             | Conteúdo                                                        | Exemplo                                      |
  | ------------------------------------ | --------------------------------------------------------------- | -------------------------------------------- |
  | `/<pacote.v1.Service>/<Método>`      | Serviços ConnectRPC dos módulos (protocolo Connect: Proto/JSON) | `POST /identity.v1.IdentityService/Setup`    |
  | `/locations/{LocationID}/files/{ID}` | Plano de dados HTTP puro do `storage` (`Range` / `PUT`)         | `GET /locations/{LocationID}/files/{FileID}` |
  | `/plugins/<plugin>/`                 | Plugins de API (surfaces)                                       | `/plugins/jellyfin/Users/Me`                 |

- **Autenticação das rotas de módulos:** por simplicidade, as rotas internas dos
  módulos não têm autenticação no momento; o servidor roda atrás de um reverse
  proxy/API gateway que expõe ao exterior apenas `/plugins/<plugin>/`.

### 4.2 Regras dos contratos Protobuf e chamadas entre módulos

1. **Fonte única da verdade no `.proto`:** nenhum módulo escreve pacotes
   `router/` ou `client/` manuais para chamadas entre módulos. Todas as
   mensagens de requisição/resposta são definidas em `proto/<módulo>/v1/`.
2. **Erros tipados com `connect.Code`:** o `handler/` do módulo converte os
   erros sentinela do `service/` para códigos padrão (`connect.CodeInvalidArgument`,
   `connect.CodeUnauthenticated`, `connect.CodePermissionDenied`,
   `connect.CodeNotFound`, `connect.CodeAlreadyExists`,
   `connect.CodeFailedPrecondition`, `connect.CodeUnavailable`,
   `connect.CodeInternal`). Os consumidores verificam erros exclusivamente via
   `connect.CodeOf(err)`.
3. **Coleções grandes paginadas** (limite/offset ou cursor), nunca listas
   ilimitadas.
4. **Bytes e paths físicos fora dos contratos Protobuf/JSON:** arquivos são
   referenciados pelo `FileID` (seção 7) e transferidos exclusivamente via HTTP
   puro do `storage` (`GET` com `Range` e `PUT`), aproveitando `sendfile(2)` do
   kernel Linux. Caminhos só aparecem nos contratos como **dados relativos a uma
   localização**.
5. **Eventos versionados e idempotentes:** eventos usam o envelope CloudEvents
   1.0 (`pubsub.Envelope[T]`) com ID (`UUIDv7`), timestamp e tipo versionado;
   seus payloads ficam no pacote central `internal/events`, em arquivos por contexto (ex.:
   `FileIndexedV1 = "storage.file.indexed.v1"` e `FileEventV1`).
6. **Sem estado global compartilhado** entre módulos; cada módulo acessa apenas
   seu próprio banco e recursos.
7. **Wiring centralizado:** apenas `cmd/server` instancia os módulos habilitados
   e monta a struct `clients.Clients` (decidindo entre `connectinprocess` e
   cliente remoto `h2c` por módulo).

### 4.3 PubSub, eventos e ausência de Outbox (Pilar 3)

Para maximizar a performance e reduzir pela metade o I/O de escrita em disco/SD
Card no SQLite (especialmente em Raspberry Pi e NAS), o projeto **não utiliza
tabela `outbox` relacional**:

- **Publicação direta após o commit:** o `service` de cada módulo persiste a
  alteração no seu `store` e, imediatamente após o commit, publica os eventos
  no `pubsub.Publisher`.
- **Isso elimina o acoplamento de transação no contexto:** não é necessário
  compartilhar transações entre o `store` do módulo e um pacote de outbox
  externo.
- **Interface `internal/pubsub`:**
  - `Message`: contém `{ID string, Topic string, Payload []byte}`, onde `ID` é o
    `UUIDv7` do evento.
  - `Publisher`: `Publish(ctx context.Context, msg Message) error`.
  - `Subscriber`: `Subscribe(ctx context.Context, consumer, topic string, h Handler) error`,
    onde `consumer` identifica o módulo consumidor (ex.: `"library"`,
    `"subtitles"`).
- **Backends de `pubsub`:**
  1. **`pubsub/memory` (Monolito autossuficiente):** entrega os eventos em
     memória para os handlers inscritos. Se um handler falhar temporariamente,
     realiza novas tentativas em background com _exponential backoff_ curto
     (`100ms`, `500ms`, `2s`, `5s`).
  2. **`pubsub/nats` (Microserviços distribuídos):** utiliza **NATS JetStream**
     para eventos duráveis de estado e **NATS Core** para eventos efêmeros:
     - Publica enviando `msg.ID` (`UUIDv7`) no header `Nats-Msg-Id`, ativando a
       deduplicação nativa do NATS JetStream.
     - Cada `consumer` (`"library"`, `"subtitles"`, `"watchstate"`) cria um
       _Durable Consumer / Queue Group_ isolado no JetStream: se houver 3
       réplicas da `library` e 2 do `subtitles`, o NATS entrega o evento para
       apenas 1 réplica da `library` e 1 réplica do `subtitles`, e se uma
       falhar (`Nak`), reentrega apenas para o grupo que falhou.
- **Envelope CloudEvents 1.0 (`pubsub.Envelope[T]`):**

  | Campo         | Conteúdo                                                           |
  | ------------- | ------------------------------------------------------------------ |
  | `id`          | UUIDv7, usado para ordenação e deduplicação (`Nats-Msg-Id`)        |
  | `type`        | Nome versionado, ex.: `storage.file.indexed.v1`; é também o tópico |
  | `source`      | Módulo de origem, ex.: `storage`                                   |
  | `specversion` | `1.0`                                                              |
  | `time`        | Momento do evento (UTC)                                            |
  | `subject`     | Recurso principal, ex.: o `FileID`                                 |
  | `data`        | Payload específico, definido em `internal/events` (por contexto)   |

### 4.4 ECST (Event-Carried State Transfer) e `Resync`

Para atingir baixa latência de leitura (estilo Netflix) mesmo com bancos
isolados por módulo, a comunicação entre módulos prioriza **eventos carregando
estado (ECST)** em vez de chamadas síncronas em cascata:

- **Projeções locais (padrão obrigatório):** cada módulo escuta os eventos dos
  módulos dos quais depende e mantém uma projeção local enxuta no seu próprio
  banco, evitando consultas síncronas entre módulos no caminho crítico de
  leitura.
- **Escrita de arquivos sempre síncrona (Push no `storage`):** apenas o
  `storage` manipula dados físicos. Qualquer módulo que produza um arquivo
  (`transcoder`, `subtitles`, `metadata`, `downloader`) grava-o de forma
  síncrona no `storage` (via HTTP `PUT`), e o `storage` emite os eventos
  correspondentes (`FileIndexed`, `FileAnalyzed`).
- **RPC síncrono restrito ao estritamente necessário:** chamadas síncronas
  via ConnectRPC entre módulos ocorrem apenas quando há interação imediata
  bloqueante (ex.: `Surfaces` consultando `identity`/`library`/`watchstate`,
  `library` acionando `storage.Resync`, ou `playback` chamando `StartTranscode`
  no `transcoder`).
- **Operação `Resync` universal e Reconciliação:** todo módulo produtor de
  estado persistido (o `playback`, só em memória, é a exceção) implementa operações de `Resync` por contexto em seu contrato
  ConnectRPC. Ao ser invocada, ela lê os registros atuais do banco do módulo e
  republica os eventos de estado no `pubsub`. Como todo consumidor é idempotente
  (`UPSERT` por ID), o `Resync` resolve:
  1. **Cold start:** quando uma nova biblioteca é cadastrada ou um módulo
     opcional é habilitado depois que o sistema já possui dados.
  2. **Recuperação pós-queda abrupta e reconciliação periódica:** caso o
     processo sofra queda de energia no microssegundo entre o `COMMIT` no banco
     e o `Publish` no `pubsub`, o `Resync` (acionado sob demanda, após
     desligamento abrupto ou periodicamente via `MSERVER_RESYNC_INTERVAL=24h`,
     podendo ser incremental por `updated_at`) reconstrói e reconcilia o estado
     de ponta a ponta sem perda.

### 4.5 Banco de Dados Unificado: SQLite e Postgres (Pilar 2)

Cada módulo possui **uma única implementação de persistência em SQL manual**
diretamente em `internal/modules/<módulo>/store/` (sem subpasta `sqlstore` ou
`sqlite`), operando sobre `*sql.DB` da biblioteca padrão (sem ORMs e sem
wrappers sobre `*sql.DB`), compatível simultaneamente com **SQLite**
(Monolito / NAS) e **Postgres** (Microserviços):

1. **Queries base em `store/queries.go`:** todas as queries SQL base do módulo
   ficam centralizadas como constantes em `internal/modules/<módulo>/store/queries.go`
   (apenas a concatenação condicional de filtros/paginação em queries dinâmicas
   fica como exceção nos métodos, partindo da constante base em `queries.go`).
2. **Placeholders sempre no padrão `$1, $2, $3...`:** suportado nativamente
   tanto pelo SQLite quanto pelo Postgres (`pgx`). O uso de `?` é proibido.
3. **Queries dinâmicas com helpers `arg` e `inClause`:** filtros opcionais
   constroem a cláusula `WHERE` adicionando o argumento ao slice e gerando o
   placeholder `$N` correspondente em uma única operação (`arg(&args, val)` e
   `inClause(&args, slice)`).
4. **Paginação SQL padrão:** a cláusula `LIMIT $N` só é concatenada na query
   quando `Limit > 0` (nunca usar `LIMIT -1`, que é inválido no Postgres) e
   `OFFSET $N` só quando `Offset > 0`. Como o SQLite não aceita `OFFSET` sem
   `LIMIT`, um `Offset` sozinho usa `LIMIT` com o maior `int64`.
5. **Upserts padronizados:** uso de `INSERT INTO ... ON CONFLICT (...) DO UPDATE SET col = excluded.col`,
   cuja sintaxe é idêntica no SQLite e no Postgres.
6. **Tipos portáveis nas structs de scan:**
   - **IDs:** `uuid.UUID` (`TEXT` no SQLite, `UUID` nativo no Postgres).
   - **Timestamps:** convertidos para `int64` (`time.Time.UnixNano()` →
     `INTEGER` no SQLite, `BIGINT` no Postgres).
   - **Booleanos:** `bool` (`INTEGER` 0/1 no SQLite, `BOOLEAN` no Postgres).
   - **Erros de unicidade:** verificados via `database.IsUniqueViolation(err)`
     (cobrindo `"UNIQUE constraint failed"` do SQLite e SQLSTATE `23505` do
     Postgres).
7. **Migrations isoladas por dialeto:** cada módulo embute via `go:embed` duas
   pastas de migrations dentro de `store/migrations/`, exportadas como
   `store.Migrations`:
   - `migrations/sqlite/*.sql`
   - `migrations/postgres/*.sql`

   O `module.go` abre o banco com
   `database.Open(ctx, cfg, "<módulo>", store.Migrations)`, que aplica as
   migrations do dialeto configurado, e cria o store com `store.New(db)`.

8. **Transações internas ao `store`:** transações (`withTx`) são um detalhe
   de cada método do `store` que precisa de atomicidade (ex.: `ApplyScan`) e
   nunca viajam pelo `context.Context`. Como o pool do SQLite tem uma única
   conexão, dentro da transação todas as queries usam o `*sql.Tx` e as linhas
   são fechadas antes da próxima query.
9. **Isolamento físico e lógico:**
   - Quando `MSERVER_DB_DRIVER=sqlite` (padrão): abre `<MSERVER_DATA_DIR>/<módulo>.db`
     com `WAL`, `foreign_keys=1`, `synchronous=NORMAL` e `MaxOpenConns(1)`.
   - Quando `MSERVER_DB_DRIVER=postgres`: conecta em `MSERVER_POSTGRES_URL`,
     executa `CREATE SCHEMA IF NOT EXISTS <módulo>` e configura a conexão do
     módulo com `search_path=<módulo>`, mantendo isolamento estrito entre as
     tabelas e `schema_migrations` de cada módulo dentro do mesmo cluster
     Postgres.

> O detalhamento completo do contexto de cada módulo e o catálogo de todos
> os eventos e seus payloads estão em
> [`docs/modulos-e-eventos.md`](modulos-e-eventos.md).

## 5. Plugins vs. backends

Critério: **quem escolhe e quantos ficam ativos ao mesmo tempo?**

| Critério           | Backend                                                                   | Plugin                                        |
| ------------------ | ------------------------------------------------------------------------- | --------------------------------------------- |
| Quem escolhe       | Operador, na instalação/deploy                                            | Usuário, em tempo de execução                 |
| Quantos ativos     | Normalmente um                                                            | Vários simultaneamente, com prioridade/ordem  |
| Configuração       | Arquivo de config / variáveis de ambiente                                 | API (credenciais, prioridade, por biblioteca) |
| Conjunto de opções | Fechado e estável                                                         | Aberto (terceiros podem querer criar novos)   |
| Semântica          | Idêntica entre implementações                                             | Cada um traz dados/comportamentos diferentes  |
| Onde fica          | `internal/database`, `internal/kv`, `internal/pubsub` ou dentro do módulo | `plugins/<módulo>/<nome>`                     |

Regra prática: **se um terceiro iria querer escrever, é plugin; se só o
operador escolhe, é backend.**

### 5.1 Classificação

- **Backends:** bancos de dados (`sqlite`, `postgres`), KV (`memory`, `valkey`),
  mensageria (`memory`, `nats`), drivers do VFS (disco local; S3 no futuro —
  ficam dentro do módulo `storage`).
- **Plugins:** analisadores de arquivos do `storage` (`ffprobe`, SHA-256,
  ED2K), provedores de metadados e segmentos (TVDB, TMDB, AniDB, AniSkip,
  IntroDB — pós-MVP), sincronizadores de estado no `watchstate` (Trakt,
  AniList — pós-MVP), indexers (Torznab, Newznab, Cardigann), clientes de
  download (qBittorrent, Transmission, SABnzbd), provedores de legenda,
  motores de tradução, notificações e a API de compatibilidade Jellyfin.

### 5.2 Tipos de plugin

1. **Provider** — fornece dados/capacidades a um módulo (analisadores de
   arquivos, metadados, indexer, download client, legendas, tradução).
2. **Surface** — expõe o sistema para fora, consumindo vários módulos
   (ex.: `api/jellyfin`).
3. **Hook** — reage a eventos (notificações, webhooks).

> A **API do projeto** não é um plugin: ela é formada pelos próprios serviços
> ConnectRPC dos módulos, sempre presentes (ver seção 6.1).

### 5.3 Diretrizes

- Plugins podem ser **bundled** e habilitados por padrão.
- Plugins internos usam **apenas o contrato público** do módulo que
  estendem. Se precisarem de algo interno, o contrato está incompleto.
- Contratos de plugin seguem as mesmas regras da seção 4.2, permitindo
  plugins externos no futuro.
- Registro explícito, sem depender de `init()`.

## 6. Módulos

| Módulo         | Substitui                      | Responsabilidade                                                                                                                                     | Fase    |
| -------------- | ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `storage`      | —                              | VFS, localizações, indexação e `FileID`, analisadores (`ffprobe` no MVP; hashes no pós-MVP), API HTTP de dados (`Range`), eventos, `Resync`          | MVP     |
| `identity`     | —                              | Contas (`Account`: email, username, senha), perfis (`Profile`: handle, nome), dispositivos, sessões e tokens                                         | MVP     |
| `library`      | Jellyfin (catálogo)            | Catálogo materializado global (100% cacheável): bibliotecas, identificação por path via eventos (filmes e séries), informações técnicas e legendas   | MVP     |
| `playback`     | Jellyfin (playback)            | Sessões de reprodução ativas (no `kv.Store`), decisão de reprodução (Direct Play no MVP) e eventos de playback (salas compartilhadas no pós-MVP)     | MVP     |
| `subtitles`    | Bazarr                         | MVP: match bidirecional de legendas locais `.srt` via projeção própria alimentada por eventos. Pós-MVP: outros formatos e provedores externos        | MVP     |
| `watchstate`   | Jellyfin (`UserData`)/Tautulli | Estado e histórico por **perfil** (`ProfileID`): progresso, assistidos, favoritos, próximos episódios e _watch history_                              | Pós-MVP |
| `transcoder`   | Jellyfin (transcode)           | Workers `ffmpeg`: HLS em tempo real (controlado por `SIGSTOP`/`SIGCONT`) e jobs em background (conversão de legendas, thumbnails, capítulos, intros) | Pós-MVP |
| `metadata`     | Jellyfin (providers)           | Provedores externos de metadados, artes e segmentos de abertura/encerramento (plugins: TVDB, TMDB, AniDB, AniSkip, IntroDB, ...)                     | Pós-MVP |
| `indexer`      | Prowlarr                       | Indexers e busca assíncrona de releases (plugins: Torznab, Newznab, Cardigann)                                                                       | Pós-MVP |
| `downloader`   | (parte dos \*arr)              | Integração com clientes de download externos (plugins) e escrita/hardlink dos arquivos concluídos no `storage`                                       | Pós-MVP |
| `acquisition`  | Sonarr + Radarr                | Monitoramento do catálogo, perfis de qualidade, motor de decisão e coordenação assíncrona com `indexer` e `downloader`                               | Pós-MVP |
| `translation`  | Lingarr                        | Tradução assíncrona de legendas (plugins)                                                                                                            | Pós-MVP |
| `notification` | —                              | Notificações e webhooks (plugins) reagindo aos eventos de todos os módulos                                                                           | Pós-MVP |

Observações:

- **Contas (`Account`) vs. Perfis (`Profile`) e Migração de Perfil (`identity` + `watchstate`):**
  - O `identity` separa **Conta (`AccountID`)** de **Perfil (`ProfileID`)** desde o dia 1, no modelo da Netflix:
    - **Conta (`Account`):** guarda credenciais e dados administrativos (`username`, `email`, senha, flag `is_admin` e, no futuro, vínculo SSO OIDC).
    - **Perfil (`Profile`):** pertence a uma conta e guarda os dados de apresentação e consumo (nome, identificador/handle curto e, no futuro, imagem/avatar). Uma conta pode ter vários perfis.
  - Todas as sessões no `playback` (e, no pós-MVP, todo o histórico e estado no `watchstate`) são vinculadas exclusivamente ao **`ProfileID`**, nunca ao `AccountID`.
  - **Migração de perfil entre contas em `O(1)` (Pós-MVP):** como o `watchstate` indexa tudo por `ProfileID`, transferir um perfil de uma conta para outra exige apenas atualizar o `account_id` do perfil no `identity` (emitindo `ProfileTransferred`), levando todo o histórico e favoritos automaticamente sem alterar nenhuma linha no banco do `watchstate`.
- **Separação entre Catálogo (`library`), Sessão de Reprodução (`playback`) e Estado do Perfil (`watchstate` — Pós-MVP):**
  - A `library` não conhece contas, perfis nem conceitos do Jellyfin: seu banco e seu cache são 100% globais e alimentados de forma reativa pelos eventos do `storage` e `subtitles` (e, no pós-MVP, `transcoder` e `metadata`).
  - O `playback` gerencia a decisão de reprodução e o estado efêmero das sessões ativas no `kv.Store` (sem precisar de banco SQL próprio), emitindo eventos de sessão (`PlaybackStarted`, `PlaybackProgressed`, `PlaybackPaused`, `PlaybackResumed`, `PlaybackStopped`) diretamente no `pubsub`.
  - No MVP, como o módulo `watchstate` fica para o segundo momento, o plugin `plugins/api/jellyfin` devolve `UserData` com valores padrão zerados e listas vazias para `Resume` e `NextUp`. Quando o `watchstate` entrar no pós-MVP, ele passará a consumir os eventos da `library` e do `playback` sem alterar esses módulos.
- **Salas de reprodução compartilhada (Pós-MVP):**
  - Entrarão no módulo `playback` no pós-MVP suportando código de acesso. Qualquer adaptação para o protocolo `SyncPlay` do Jellyfin fica isolada em `plugins/api/jellyfin`.
- **Controle eficiente do `transcoder` (`SIGSTOP`/`SIGCONT` — Pós-MVP):**
  - Entrará no pós-MVP para transcode de áudio HLS (`-c:v copy`), conversão de legendas (`.srt`/`.ass` → `.vtt`) e extração de imagens/segmentos em background.
- **Capítulos (`Chapters`) e Pular Abertura/Créditos (`MediaSegments` — Pós-MVP):**
  - Ficam fora do MVP e entrarão no segundo momento (extraídos via `ffprobe`, provedores externos no `metadata` e detecção automática no `transcoder`).
- Sonarr e Radarr serão unificados em `acquisition`, com "tipos de mídia",
  pois compartilham parser de releases, perfis de qualidade e motor de
  decisão, comunicando-se por eventos com `indexer` e `downloader`.
- O wrapper de baixo nível do `ffprobe` (e futuramente `ffmpeg`) fica em
  `internal/ffmpeg` e é usado pelo analisador `ffprobe` do `storage`.
- **Dependências obrigatórias:** cada módulo valida, no `New`, as
  dependências externas de que precisa (no MVP, o `storage` valida `ffprobe`).
  Se a validação falhar, o módulo não inicializa e o startup do processo é
  abortado com uma mensagem de erro clara.

### 6.1 Surfaces: API do projeto e API Jellyfin

- **Regra inegociável de isolamento:**
  - **Todas as regras de negócio, modelos de dados, rotas e adaptações referentes ao Jellyfin devem estar estritamente isoladas dentro do plugin `plugins/api/jellyfin`.**
  - **Nenhum conceito que não tenha sido discutido deve existir fora de `plugins/api/jellyfin`.** Tudo o que for preciso adicionar fora do plugin do Jellyfin (nos módulos de domínio, em seus contratos `.proto` ou na infraestrutura) **deve ser discutido antes**.
- A **API do projeto** é o conjunto dos serviços ConnectRPC dos módulos (ex.:
  `identity.v1.IdentityService`, `storage.v1.StorageService`,
  `library.v1.LibraryService`), que aceitam tanto chamadas gRPC/Protobuf quanto
  requisições HTTP `POST` com JSON (via `curl`/Bruno), ainda sem autenticação.
  Novos métodos devem ser discutidos antes da implementação.
- O **plugin `api/jellyfin`** (`plugins/api/jellyfin`) concentra **100% das
  rotas e regras de compatibilidade com a API do Jellyfin 12.4.0** (descoberta,
  login, parse de `<username ou email>#<profile>`, `QuickConnect`, tradução de
  catálogo para `BaseItemDto`/`MediaSources`/`MediaStreams`/`DeliveryUrl`,
  avaliação de `DeviceProfile` em `PlaybackInfo`, conversão de unidades para
  `Ticks`, sessões de reprodução e streaming). Nenhuma rota ou regra do
  Jellyfin existe nos módulos.
- **Streaming de vídeo e legendas no plugin Jellyfin:** o módulo `storage` expõe
  sua rota HTTP pura de dados (`GET /locations/{LocationID}/files/{FileID}`, com
  suporte a `Range` via `http.ServeContent` / `sendfile`). Nas rotas de
  streaming do Jellyfin (`GET /Videos/{itemId}/stream*` e
  `GET /Videos/.../Subtitles/.../Stream.{format}`), o plugin `api/jellyfin` atua
  como **proxy reverso em streaming** para o `storage` (ou entrega diretamente
  no monolito), repassando `Range`, `Content-Range`, `Content-Length` e status
  `200`/`206`.
- No modo distribuído, surfaces são processos stateless que podem escalar em
  qualquer instância, falando com os módulos via ConnectRPC/gRPC e `kv/valkey`.

```mermaid
flowchart TD
    REST[API do projeto - ConnectRPC JSON/gRPC] --> ID[identity]
    REST --> STO[storage]
    REST --> LIB[library]
    JF[plugin api/jellyfin] -->|ConnectRPC In-Process ou HTTP/2| ID
    JF -->|ConnectRPC In-Process ou HTTP/2| LIB
    JF -->|ConnectRPC In-Process ou HTTP/2| PB[playback]
    JF -->|HTTP Range GET| STO
    PB -->|ConnectRPC| LIB
    STO -.->|FileIndexed / FileAnalyzed / File*| LIB
    STO -.->|FileIndexed / File*| SUB[subtitles]
    LIB -.->|Movie* / Episode*| SUB
    SUB -.->|SubtitleAdded / Removed| LIB
```

## 7. Módulo de storage (VFS)

Todo acesso a dados de mídia passa pelo módulo `storage`. No modo de
microserviços distribuídos, o `storage` pode rodar sozinho diretamente no NAS
(com SQLite local para os metadados físicos dos arquivos e publicando eventos no
NATS), enquanto todos os outros módulos escalam horizontalmente em outras
máquinas.

**Apenas o `storage` lida com paths físicos.** Depois que um arquivo é
indexado, o restante do projeto o referencia pelo **`FileID`**. Quando
outros módulos precisam de nomes de pastas/arquivos, recebem **caminhos
relativos à localização** (ex.: à pasta da biblioteca), nunca caminhos
absolutos.

### 7.1 Conceitos

- **Localização (`Location`):** único conceito de "onde ficam os
  arquivos". É um diretório acessado por um **driver** (ex.: driver
  `local` em `/mnt/hdd/series`), identificado por um `LocationID`. Não
  existe um agrupamento acima dela (não há "storages nomeados").
  **Diretórios são responsabilidade exclusiva do `storage`:** são
  cadastrados nele (via API do projeto, informando driver e opções do
  driver, como o diretório), e os demais módulos apenas
  referenciam o `LocationID`. O storage indexa e observa apenas
  localizações conhecidas (cadastradas ou de sistema, ver abaixo).
  **Localizações não podem se sobrepor:** cadastrar um diretório que
  está dentro de outra localização, ou que contém outra, é recusado.
  Assim, todo arquivo pertence a exatamente uma localização.
- **Classe da localização (planejado, entra no pós-MVP com o `transcoder`):**
  no MVP só existem localizações cadastradas pelo operador (`media`). Classes e
  localizações de sistema serão implementadas quando houver um módulo
  que grave artefatos. A classe define como o storage trata os arquivos
  dela. **Todo arquivo, de qualquer classe, é indexado e tem `FileID`.**
  O que muda entre as classes é o fluxo aplicado depois da indexação.

| Classe       | Criada por                            | Analisadores (seção 7.3) | TTL | Uso                                                   |
| ------------ | ------------------------------------- | ------------------------ | --- | ----------------------------------------------------- |
| `media`      | Operador, via API do projeto          | Sim (`ffprobe` no MVP)   | Não | Diretórios de bibliotecas (mídia e legendas externas) |
| `cache`      | Storage, automaticamente (de sistema) | Não                      | Sim | Artefatos temporários (ex.: legendas convertidas)     |
| `transcodes` | Storage, automaticamente (de sistema) | Não                      | Sim | Segmentos e playlists HLS gerados pelo `ffmpeg`       |
| `images`     | Storage, automaticamente (de sistema) | Não                      | Sim | Imagens, frames de capítulos e _trickplay_ (pós-MVP)  |

- **Localizações de sistema** (todas as classes exceto `media`):
  - São criadas pelo storage no `New`. O driver e o destino de cada uma
    são definidos pelo operador via variáveis de ambiente no formato
    `MSERVER_STORAGE_DRIVER_<DRIVER>_<LOCALIZAÇÃO>_<OPÇÃO>` (ex.:
    `MSERVER_STORAGE_DRIVER_LOCAL_CACHE_DIR=/mnt/ssd/cache`). Sem
    configuração, usam o driver local em `<MSERVER_DATA_DIR>/<classe>`.
    Configurar mais de um driver para a mesma localização é erro no
    `New`.
  - Não podem ser usadas como diretório de biblioteca.
  - Os arquivos são gravados pelos módulos via HTTP `PUT` no `storage`, que
    devolve o `FileID`.
  - **TTL:** como em uma CDN, cada arquivo pode ter um tempo de vida,
    definido na escrita (com um padrão configurável por classe, ex.:
    `MSERVER_STORAGE_CACHE_TTL`).
- **`FileID`:** identificador opaco e ordenável por tempo (**UUIDv7**),
  gerado na primeira indexação.
- **Estabilidade do `FileID`:**
  - **Mudança de conteúdo** no mesmo caminho (tamanho/mtime/inode
    diferentes): mantém o `FileID` e emite `FileChanged`.
  - **Movido/renomeado** dentro da mesma localização: um caminho novo com
    o `inode` de um arquivo que sumiu é o mesmo arquivo; mantém o
    `FileID` e emite `FileMoved`. Sem suporte a inode no driver, o
    arquivo é tratado como removido + adicionado (novo `FileID`).
  - **Movido entre localizações ou sistemas de arquivos (pós-MVP):**
    reconhecido depois da análise, pelo SHA-256 de um arquivo que acabou
    de sumir.
- **Hardlinks e cópias** são caminhos físicos diferentes e, portanto,
  `FileID`s diferentes; o SHA-256 permite identificá-los como conteúdo
  idêntico.

### 7.2 Indexação

A indexação acontece em duas etapas, para que um arquivo apareça no
sistema sem esperar a leitura ou inspeção completa do conteúdo:

1. **Registro (rápido):** varredura da localização; grava `FileID`,
   localização, caminho relativo, tamanho, mtime e `inode` (quando
   suportado). Emite `FileIndexed` no `pubsub`.
2. **Análise:** executa os analisadores habilitados (seção 7.3) de forma
   assíncrona — no MVP, o analisador `ffprobe` para arquivos de mídia; no
   pós-MVP, também os analisadores de hash — e emite `FileAnalyzed`.

Regras:

- **Quando varrer:** todas as localizações são varridas ao iniciar o
  módulo e periodicamente (`MSERVER_STORAGE_SCAN_INTERVAL`). O `storage`
  também expõe `Resync(ctx, LocationID)` para republicar os eventos
  `FileIndexed` e `FileAnalyzed` de todos os arquivos já conhecidos de uma
  localização (ex.: chamado quando a `library` cadastra uma nova
  biblioteca sobre uma localização já indexada). Observação em tempo real
  (`fsnotify`) fica para depois do MVP.
- Arquivos em escrita (cópia ou download em andamento) só serão
  analisados quando o `mtime` for mais antigo que uma janela de
  estabilidade; os mais recentes ficam para a próxima passada.
- **Escrita síncrona por outros módulos (Push):** toda criação de arquivo
  por qualquer módulo (`transcoder`, `subtitles`, `metadata`,
  `downloader`) é feita de forma síncrona no `storage` (via HTTP
  `PUT`). O `storage` nunca puxa arquivos de outros módulos.
- Se a raiz de uma localização, ou qualquer diretório dela, ficar
  inacessível (ex.: disco desmontado), a varredura **falha sem alterar o
  índice** e não gera `FileRemoved` em massa.

### 7.3 Analisadores de arquivos (plugins)

- Informações técnicas de mídia e hashes são
  calculados por **analisadores**, plugins do tipo provider do `storage`
  (`plugins/storage/<nome>`).
- **O próprio módulo inicializa seus plugins** (no `New`, a partir das
  suas variáveis de ambiente). Só os plugins de API (`plugins/api/*`) são
  criados fora dos módulos, pelo `cmd/server`.
- **MVP:** analisador `ffprobe` (`plugins/storage/ffprobe`), que inspeciona
  arquivos de mídia e extrai container, duração, bitrate e faixas de vídeo,
  áudio e legendas embutidas (`MediaStreams`).
- **Pós-MVP:** extração de capítulos embutidos (`Chapters`) no `ffprobe` e
  analisadores de hash (`sha256`, `ed2k` e, futuramente, hash do OpenSubtitles,
  BLAKE3).
- **Leitura única para hashes:** para analisadores baseados em stream de
  bytes, o storage lê o arquivo **uma única vez** e alimenta todos em
  paralelo, em vez de ler um arquivo de 50 GB uma vez por algoritmo.
- Cada resultado é gravado como `(FileID, analisador, versão, valor)` e
  publicado via `FileAnalyzed`. Mudar a versão de um analisador dispara a
  reanálise dos arquivos.
- Os analisadores rodam apenas sobre arquivos de localizações da classe
  `media`.

### 7.4 Metadados de um arquivo

| Campo                             | Origem                         |
| --------------------------------- | ------------------------------ |
| `FileID` (UUIDv7)                 | Storage, na primeira indexação |
| `LocationID`                      | Localização cadastrada         |
| Caminho relativo                  | Relativo à localização         |
| Tamanho, mtime                    | Driver                         |
| Tipo de mídia (`mime_type`)       | Storage, pela extensão         |
| `inode`                           | Driver (quando suportado)      |
| Container, duração, streams, caps | Analisador `ffprobe` (MVP)     |
| SHA-256, ED2K, ...                | Analisadores de hash (pós-MVP) |
| Indexado em                       | Storage                        |

### 7.5 Acesso aos dados (Plano de Dados HTTP Puro)

- **Leitura de nomes ≠ acesso:** a `library` e o `subtitles` usam o
  caminho relativo recebido nos eventos `FileIndexed`/`FileMoved` para
  aplicar regex e fazer o match, mas nenhum módulo usa caminhos para
  acessar, mover ou apagar arquivos: toda operação recebe `FileID`.
- **API HTTP de dados (`storage`):**
  - Expõe `GET /locations/{LocationID}/files/{FileID}` (e `HEAD`) em **HTTP
    puro** (fora do ConnectRPC/Protobuf) com suporte a `Range` (`206 Partial
Content`, `Accept-Ranges: bytes`, `Content-Length`, `Content-Type`), usando
    `http.ServeContent` para permitir _zero-copy_ (`sendfile`) pelo kernel
    Linux.
  - Escrita via `PUT` (saída do `ffmpeg`, legendas convertidas, imagens) e
    autenticação por URLs assinadas de curta duração entram no pós-MVP com o
    `transcoder`.
- **Resolução:** o método `ResolveFile` do `storage` devolve a URL HTTP
  `http://<host>/locations/{LocationID}/files/{FileID}`.

### 7.6 Drivers e capacidades

- **Drivers:** disco local (`driver/local`) no início; S3 e outros no
  futuro. Cada localização fica confinada ao seu diretório (`os.Root` no
  driver local), sem permitir path traversal.
- **Hardlinks:** devem ser suportados pelo contrato do VFS para o futuro
  módulo `acquisition`.
- **Move entre sistemas de arquivos:** quando `rename` não for possível
  (ex.: `EXDEV`), o driver faz cópia + remoção.

### 7.7 Eventos

| Evento (tópico)            | Quando                                                          |
| -------------------------- | --------------------------------------------------------------- |
| `storage.file.indexed.v1`  | Arquivo novo indexado (ou republicado via `Resync`)             |
| `storage.file.changed.v1`  | Tamanho, mtime ou inode mudou no mesmo caminho                  |
| `storage.file.moved.v1`    | Arquivo renomeado dentro da localização (mesmo inode)           |
| `storage.file.removed.v1`  | Arquivo sumiu do disco ou a localização foi removida            |
| `storage.file.analyzed.v1` | Analisador concluiu inspeção do arquivo (`ffprobe`, hashes, ..) |

- Todos usam o envelope CloudEvents (`pubsub.Envelope[T]`) com `source` =
  `storage` e `subject` = `FileID`.
- Publicados diretamente no `pubsub.Publisher` após o commit da alteração no
  banco do `storage`.
- Consumidos pela `library` e pelo `subtitles`.

## 8. Library: bibliotecas e identificação pelo path

No MVP não existe módulo de metadados. Todos os itens são identificados
apenas pelo path dos arquivos, aplicando expressões regulares sobre os
nomes de arquivos e pastas (como fazem o Sonarr e o Jellyfin). Esse
comportamento permanece no futuro como fallback quando nenhum provedor de
metadados encontrar o item.

**Regra geral: o comportamento deve ser igual ao do Jellyfin.**

- As regex do Jellyfin e do Sonarr são usadas como **referência** para
  criarmos as nossas em Go (RE2).
- Os parsers são cobertos por testes baseados em tabela, com nomes reais
  de arquivos e pastas e o resultado esperado.

### 8.1 Cadastro de bibliotecas

- **Responsabilidades separadas:**
  - **Diretórios** pertencem ao `storage` (cadastro de localizações).
  - **Bibliotecas** pertencem à `library`.
- Um método cadastra a biblioteca, informando nome, **tipo** e **um ou mais
  diretórios** (`LocationID`s já cadastrados no `storage`).
- **Construção reativa por eventos:** a `library` **não** varre o disco nem
  consulta arquivos de forma síncrona. Ela reage aos eventos do `storage`
  (`FileIndexed`, `FileMoved`, `FileChanged`, `FileRemoved`,
  `FileAnalyzed`), que trazem `FileID` + caminho relativo ao diretório de
  origem.
- O cadastro de uma nova biblioteca aciona `storage.ResyncLocation` via
  ConnectRPC para que o `storage` republique os eventos dos arquivos já
  indexados naquela localização.

### 8.2 Filmes

- Cada arquivo de vídeo é **um filme**.
- Regex sobre o nome do arquivo e da pasta para extrair **título** e
  **ano** (ex.: `Matrix (1999)/Matrix.1999.1080p.BluRay.mkv` → título
  `Matrix`, ano `1999`).
- **Fallback:** se a regex não identificar nada, o título é o nome do
  arquivo sem extensão.

### 8.3 Séries

**Séries e temporadas são conceitos lógicos que existem apenas dentro da
`library`.** Elas não têm relação com arquivos ou pastas:

- **Episódio:** item ligado a um arquivo (`FileID`).
- **Temporada:** entidade que apenas agrupa episódios (com número da
  temporada).
- **Série:** entidade que apenas agrupa temporadas (com título e ano).

Estrutura de pastas reconhecida pela identificação:

```
<diretório>/
└── <Série>/
    ├── Season 01/
    │   └── <arquivos>
    └── <arquivos>        # sem pasta de temporada
```

- **Série:** o título (e o ano, se houver) é extraído via regex do nome
  da pasta de primeiro nível do diretório.
- **Temporada** (em ordem de prioridade):
  1. Temporada extraída do **nome do arquivo** (ex.: `S02E05` → temporada 2).
  2. Temporada extraída da **pasta de temporada** (`Season 01`, `S01`).
  3. Sem nenhuma das anteriores, a série tem **uma única temporada**.
- Temporadas sem episódios e séries sem temporadas são removidas.
- **Especiais:** pastas como `Season 00`/`Specials` (e `S00Exx` no nome do
  arquivo) formam a temporada 0.
- **Episódio:** suporta episódios simples (`S01E02`, `1x02`, `E02`),
  múltiplos (`S01E01E02`, `S01E01-E02`), numeração absoluta de anime
  (`Serie - 105.mkv`) e episódios sem número identificado, com ordenação
  natural (`ep2` antes de `ep10`).

### 8.4 IDs estáveis

- **Filmes e episódios:** o ID (UUID) é **determinístico e derivado do
  `FileID`** (UUIDv5 do `FileID`).
- **Séries e temporadas:** têm IDs próprios (UUIDv7) gerados pela `library`
  na criação.

### 8.5 Informações técnicas, legendas externas, capítulos e segmentos

- **Informações técnicas (MVP):** container, duração, bitrate e faixas
  de vídeo, áudio e legendas embutidas chegam à `library` pelo evento
  `storage.file.analyzed.v1` gerado pelo analisador `ffprobe` do `storage`.
- **Legendas externas (MVP — `.srt`):** chegam à `library` pelos eventos
  `subtitles.subtitle.added.v1` e `subtitles.subtitle.removed.v1`, e ficam
  junto do filme ou episódio (`external_subtitles`), sem alterar os idiomas
  de legenda das faixas embutidas usados nos filtros.
- **Capítulos e segmentos de mídia (Pós-MVP):** entrarão no pós-MVP via
  `ffprobe`, `metadata` e `transcoder`.

### 8.6 Imagens, imagens de capítulos e _trickplay_

- **Fora do MVP.** Os clientes exibem seus placeholders padrão.

## 9. Compatibilidade com Jellyfin

- **Versão alvo:** Jellyfin 12.4.0 (`docs/jellyfin-openapi-stable.json`).
- **Cliente alvo do MVP:** **`jellyfin-web`** (`jellyfin-android` e
  `jellyfin-tizen` entram no pós-MVP).
- **Isolamento total em `plugins/api/jellyfin`:**
  - **Todas as rotas, DTOs, unidades (`Ticks`) e regras de negócio referentes ao
    Jellyfin vivem exclusivamente dentro de `plugins/api/jellyfin`.**
  - **Nenhum conceito que não foi discutido deve existir fora do plugin do
    Jellyfin. Tudo o que for preciso adicionar fora de `plugins/api/jellyfin`
    deve ser discutido antes.**

### 9.1 Regras de negócio isoladas em `plugins/api/jellyfin`

1. **IDs:** usar **UUID** como identificador nos módulos, evitando tabelas de
   mapeamento na surface.
2. **Mapeamento `Profile` ↔ `User` do Jellyfin e Autenticação (exclusivo do plugin):**
   - O plugin `plugins/api/jellyfin` mapeia cada **`User` exposto na API
     Jellyfin 1:1 para um `Profile` (`ProfileID`)** do `identity`.
   - O parse de `<username ou email>#<profile>` em `POST /Users/AuthenticateByName`
     é feito exclusivamente pelo plugin `plugins/api/jellyfin`.
3. **Tradução do Catálogo na Surface (exclusiva do plugin):** é o plugin
   `plugins/api/jellyfin` que traduz bibliotecas, filmes, séries, temporadas,
   episódios, faixas técnicas e legendas para `BaseItemDto`, `CollectionFolder`,
   `MediaSources`, `MediaStreams`, `DeliveryMethod`, `DeliveryUrl` e converte
   durações para `Ticks`.

### 9.2 Fluxo de reprodução no MVP e evolução para o `transcoder`

1. O cliente chama `POST /Items/{itemId}/PlaybackInfo` enviando seu
   `DeviceProfile`.
2. O plugin `plugins/api/jellyfin` avalia o `DeviceProfile` e responde com
   Direct Play (`GET /Videos/{itemId}/stream*`), atuando como proxy reverso em
   streaming para `GET /locations/{LocationID}/files/{FileID}` no `storage`.
3. **Sessões de reprodução (`/Sessions/Playing/*`):** o plugin converte os
   payloads e `PositionTicks` do Jellyfin para chamadas ConnectRPC ao módulo
   `playback`, mantendo o vínculo `PlaySessionId` ↔ `PlaybackSessionID` no seu
   `kv.Store`.
4. **Legendas externas `.srt` (MVP):** servidas via proxy em streaming para o
   arquivo `.srt` no `storage`.

### 9.3 Endpoints do MVP validados em `docs/jellyfin-openapi-stable.json`

| Entrega | Área             | Endpoints no `plugins/api/jellyfin`                                                                                                                                                                                            |
| ------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1       | Sistema/Branding | `GET /System/Info/Public`, `GET/POST /System/Ping`, `GET /System/Info`, `GET /Branding/Configuration`, `GET /Branding/Css`, `GET /Branding/Css.css`, `GET /QuickConnect/Enabled` (`false`)                                     |
| 1       | Autenticação     | `GET /Users/Public` (`[]`), `POST /Users/AuthenticateByName` (`<user/email>#<profile>`), `GET /Users/Me`, `GET /Users/{userId}`, `POST /Sessions/Capabilities/Full`, `POST /Sessions/Logout`, `GET/POST /DisplayPreferences/*` |
| 2       | Filmes           | `GET /UserViews`, `GET /Items`, `GET /Items/Latest`, `GET /Items/{itemId}`, `GET /Items/{itemId}/Intros` (vazio), `GET /UserItems/Resume` (vazio)                                                                              |
| 3       | Séries           | `GET /Shows/{seriesId}/Seasons`, `GET /Shows/{seriesId}/Episodes`, `GET /Shows/NextUp` (vazio) + suporte a `Series`/`Season`/`Episode` em `/UserViews`, `/Items`, `/Items/Latest`, `/Items/{itemId}`                           |
| 4       | Reprodução       | `POST/GET /Items/{itemId}/PlaybackInfo`, `GET/HEAD /Videos/{itemId}/stream`, `GET/HEAD /Videos/{itemId}/stream.{container}`, `POST /Sessions/Playing`, `POST /Sessions/Playing/Progress`, `Ping`, `Stopped`, `GET /GetUtcTime` |
| 5       | Legendas `.srt`  | `GET /Videos/{routeItemId}/{routeMediaSourceId}/Subtitles/{routeIndex}/Stream.{routeFormat}` e `GET /Videos/{routeItemId}/{routeMediaSourceId}/Subtitles/{routeIndex}/{routeStartPositionTicks}/Stream.{routeFormat}`          |

## 10. Estrutura de pastas

### 10.1 Árvore do repositório (MVP)

```
mserver/
├── buf.yaml
├── buf.gen.yaml
├── Makefile                             # make generate / make lint (buf via go run, versão fixa)
├── proto/                               # contratos Protobuf de todos os módulos
│   ├── identity/v1/identity.proto
│   ├── storage/v1/storage.proto
│   ├── library/v1/library.proto
│   ├── playback/v1/playback.proto
│   └── subtitles/v1/subtitles.proto
├── gen/                                 # código gerado pelo buf (protobuf + connectrpc); versionado, não editar
├── cmd/
│   └── server/                          # ponto de entrada: bootstrap híbrido (monolito / microserviços)
├── internal/
│   ├── events/                          # todos os eventos (tópicos, Source<Módulo> e payloads V1), um arquivo por contexto
│   ├── clients/                         # struct Clients com as interfaces <Service>Client geradas
│   ├── database/                        # abertura de banco (sqlite / postgres), runner de migrations, IsUniqueViolation
│   ├── kv/                              # interface kv.Store (Get/Create/Update/Delete/List com TTL)
│   │   └── memory/                      # implementação em memória (valkey nos microserviços)
│   ├── logger/                          # logger estruturado (slog) com propagação no contexto
│   ├── pubsub/                          # interfaces Publisher/Subscriber (com consumer group) e Envelope CloudEvents 1.0
│   │   └── memory/                      # barramento em memória com retry (nats nos microserviços)
│   ├── ffmpeg/                          # wrapper de ffprobe (e ffmpeg no pós-MVP)
│   └── modules/
│       ├── storage/                     # VFS, localizações, FileID, analisadores, HTTP Range GET
│       │   └── driver/
│       │       └── local/               # driver de disco local (os.Root)
│       ├── identity/                    # contas, perfis, setup inicial, dispositivos, sessões e tokens
│       ├── library/                     # catálogo materializado global (filmes, séries, streams, legendas)
│       ├── playback/                    # sessões ativas (no kv.Store), timeout por inatividade e eventos
│       └── subtitles/                   # match bidirecional de legendas locais .srt
├── plugins/
│   ├── storage/
│   │   └── ffprobe/                     # analisador ffprobe (streams técnicos)
│   └── api/
│       └── jellyfin/                    # surface plugin de compatibilidade Jellyfin 12.4.0
├── api-collection/                      # coleção de requisições da API (Bruno)
├── AGENTS.md                            # instruções de arquitetura e padrões de código para agentes de IA
└── docs/
    ├── planejamento.md
    ├── roadmap.md
    ├── modulos-e-eventos.md
    └── jellyfin-openapi-stable.json
```

### 10.2 Estrutura interna de um módulo

```
internal/modules/<módulo>/
├── service/               # regra de negócio: struct Service, erros de domínio e Resync
├── handler/               # implementa <Service>Handler (ConnectRPC), mapeando erros
├── store/                 # struct Store, models, erros, queries.go e SQL único ($1, $2)
│   ├── queries.go         # constantes com todas as queries SQL base do módulo
│   └── migrations/        # migrations embutidas (go:embed, store.Migrations) separadas por dialeto
│       ├── sqlite/
│       └── postgres/
└── module.go              # New/Close: abre o banco, cria store, service e handler, registra o handler no *connect.Server e inicia plugins
```

- **`service/`**: contém toda a regra de negócio numa struct concreta
  `Service` (não uma interface). Não importa `handler/` nem pacotes gerados do ConnectRPC. Pode
  importar o `store/` do próprio módulo, `internal/events` e as interfaces de
  cliente em `gen/...`.
- **`handler/`**: sem regra de negócio. Implementa a interface gerada pelo
  ConnectRPC (`<Service>Handler`), chama o `service` e converte os erros do
  `service` para `connect.NewError(connect.Code..., err.Error()).WithCause(err)`.
- **`internal/events/`** (fora do módulo): a API de eventos de todos os módulos,
  separada por contexto em arquivos (ex.: `storage_files.go`). Só constantes de
  tópicos versionados, `Source<Módulo>` (ex.: `SourceStorage`) e structs de
  payload com sufixo de versão (`V1`).
- **`store/`**: implementação única em SQL manual diretamente na pasta `store/`,
  com todas as queries SQL base centralizadas como constantes em `store/queries.go`,
  usando placeholders `$1, $2`, `arg(&args, v)`, `inClause(&args, slice)` e
  `database.IsUniqueViolation(err)`. Funciona tanto sobre SQLite quanto sobre
  Postgres mudando apenas o subdiretório de `store/migrations/` (`sqlite/` vs.
  `postgres/`). As transações são internas aos métodos do `store`.
- **`module.go`**: abre o banco do módulo (se o módulo usar banco) com
  `database.Open(..., store.Migrations)`, instancia `store`, `service`,
  `handler` e os plugins do módulo, e registra o `handler` no `*connect.Server`
  recebido do `cmd/server` (`Register<Service>Handler`). O `storage` expõe
  ainda um `Mux` com as rotas HTTP puras do plano de dados.

### 10.3 Regras de fronteira

- Um módulo só pode interagir com outro módulo através da interface
  `<Service>Client` gerada em `gen/<outro>/v1/<outro>v1connect` (presente
  em `clients.Clients`) e dos tipos de eventos em `internal/events`.
  **Nunca** importar `service/`, `store/` ou `handler/` de outro módulo.
- Surfaces (`plugins/api/*`) usam apenas `clients.Clients` e `kv.Store`.
- Fora do `storage`, nenhum código acessa o sistema de arquivos de mídia
  diretamente (`os.Open`, `os.Root`, etc.).
- Pacotes de infraestrutura (`internal/database`, `internal/kv`,
  `internal/pubsub`, `internal/logger`, `internal/ffmpeg`) não importam módulos
  nem plugins.
- Apenas `cmd/server` importa os `module.go` e constrói `clients.Clients`.

### 10.4 Ciclo de vida e Bootstrap Híbrido (`cmd/server`)

O `cmd/server` lê as variáveis de ambiente para decidir quais módulos subir
localmente (`MSERVER_MODULES`) e quais backends utilizar:

1. Inicializa os backends configurados:
   - Banco de dados (`MSERVER_DB_DRIVER=sqlite|postgres`)
   - PubSub (`MSERVER_PUBSUB_DRIVER=memory|nats`)
   - KV (`MSERVER_KV_DRIVER=memory|valkey`)
2. Para cada módulo do sistema:
   - Se `MSERVER_<MÓDULO>_URL` estiver definido, configura o campo em
     `clients.Clients` com um cliente remoto ConnectRPC sobre HTTP/2 (`h2c`) no
     protocolo padrão do Connect
     (`New<Service>Client(connect.NewClient(connecthttp.NewTransport(h2cClient, url)))`).
   - Se o módulo estiver habilitado localmente, inicializa o módulo (que registra
     seu handler no `*connect.Server` do processo) e instancia o
     `<Service>Client` sobre um transporte `connectinprocess`
     (`connectinprocess.New(server)`).
3. Monta o `*connect.Server` no `http.ServeMux` com `connecthttp.Mount` (junto
   das rotas HTTP do `storage` e dos plugins) e sobe o servidor HTTP com `h2c`
   (`http.Protocols` com HTTP/1 e `SetUnencryptedHTTP2(true)`, na mesma porta
   `MSERVER_HTTP_ADDR`, sem depender de `x/net`).
4. Os plugins de API (`jellyfin`, habilitado em `MSERVER_MODULES` como os
   módulos) sobem depois dos módulos, com um `kv.Store` próprio (`kv/memory`),
   e são montados no `http.ServeMux` sob `/plugins/<nome>/`. O plugin recebe os
   clients dos módulos (`identity` é obrigatório; `storage`, `library` e
   `playback` são opcionais e, se ausentes, as rotas que dependem deles
   respondem vazio ou `503`). O stream do Jellyfin obtém a URL do arquivo por
   `storage.ResolveFile` e repassa a resposta HTTP (com `Range`) ao player.
   A URL devolvida é `MSERVER_STORAGE_PUBLIC_URL` + `/locations/{LocationID}/files/{FileID}`;
   essa variável deve ser o endereço do servidor que monta as rotas do `storage`
   **como quem lê os arquivos o alcança** (ex.: `http://nas-storage:8096` quando o
   `storage` roda em outra máquina). Se vazia, usa o endereço de escuta do
   processo no loopback (`http://127.0.0.1:8096`), o que só funciona no monolito.

### 10.5 Configuração por variáveis de ambiente

```sh
# Execução e Módulos
MSERVER_HTTP_ADDR=:8096                      # endereço HTTP/1.1 + HTTP/2 (h2c)
MSERVER_MODULES=all                          # "all" (monolito) ou lista separada por vírgula (ex.: "storage" no NAS)
MSERVER_LOG_LEVEL=info                       # debug, info, warn, error
MSERVER_LOG_FORMAT=text                      # text, json

# Backends de Infraestrutura (Padrões autossuficientes para Monolito)
MSERVER_DB_DRIVER=sqlite                     # "sqlite" (padrão) ou "postgres"
MSERVER_DATA_DIR=/var/lib/mserver            # usado no sqlite: <data>/<módulo>.db
MSERVER_POSTGRES_URL=                        # usado no postgres: postgres://user:pass@host:5432/mserver

MSERVER_PUBSUB_DRIVER=memory                 # "memory" (padrão) ou "nats"
MSERVER_NATS_URL=                            # usado no nats: nats://host:4222

MSERVER_KV_DRIVER=memory                     # "memory" (padrão) ou "valkey"
MSERVER_VALKEY_URL=                          # usado no valkey: valkey://host:6379

# Endereços de Módulos Remotos (Vazio = chamada In-Process em memória no Monolito)
MSERVER_IDENTITY_URL=                        # ex.: http://identity:8096
MSERVER_STORAGE_URL=                         # ex.: http://nas-storage:8096
MSERVER_LIBRARY_URL=                         # ex.: http://library:8096
MSERVER_PLAYBACK_URL=                        # ex.: http://playback:8096
MSERVER_SUBTITLES_URL=                       # ex.: http://subtitles:8096

# Configurações de Módulos
MSERVER_STORAGE_PUBLIC_URL=                  # endereço do plano de dados que o ResolveFile devolve (padrão: o endereço de escuta, no loopback)
MSERVER_FFPROBE_PATH=/usr/bin/ffprobe        # executável do ffprobe (padrão: "ffprobe" do PATH)
MSERVER_STORAGE_SCAN_INTERVAL=6h             # varredura periódica de disco (0 desativa)
MSERVER_RESYNC_INTERVAL=24h                  # reconciliação periódica de segurança (0 desativa)
MSERVER_PLAYBACK_SESSION_TIMEOUT=5m          # inatividade até encerrar a sessão (reason: timeout)
```

## 11. Roadmap

> O detalhamento completo de cada entrega, contratos, endpoints validados contra
> `docs/jellyfin-openapi-stable.json` e critérios de aceite estão em
> [`docs/roadmap.md`](roadmap.md).

### 11.1 MVP (5 Entregas Verticais)

Objetivo: **permitir que o `jellyfin-web` conecte ao `mserver`, autentique com
`<username ou email>#<profile>`, navegue pelo catálogo local de Filmes e Séries
e reproduza mídia via Direct Play com legendas externas `.srt`, operando sobre a
arquitetura ECST e ConnectRPC.**

| Entrega | Nome                                        | Módulo(s)                                       | Escopo Principal                                                                                                                                                                                                                   | Resultado no `jellyfin-web`                                                |
| ------- | ------------------------------------------- | ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| 1       | Fundação + `identity` + Login               | `infra`, `server`, `identity`, `api/jellyfin`   | Fundação (`database` `$1/$2`, `pubsub/memory` CloudEvents 1.0, `kv/memory`, ConnectRPC `.proto`), `identity` (`Account` + `Profile`, sessões/tokens, `Resync`) e rotas base + login `<user/email>#<profile>` em `api/jellyfin`     | Conecta ao servidor, faz login e carrega a tela inicial (Home vazia)       |
| 2       | `storage` + `library` (Filmes)              | `storage`, `plugins/storage/ffprobe`, `library` | Driver `local` (`os.Root`), localizações sem sobreposição, indexação (`FileID` UUIDv7 + `inode`) + `ffprobe` (`FileAnalyzed`), `Resync`, bibliotecas de filmes, parser regex de filmes, IDs UUIDv5 do `FileID`                     | Bibliotecas de filmes visíveis na Home, listagem e página de detalhes      |
| 3       | `library` (Séries)                          | `library`, `api/jellyfin`                       | Parser regex completo de séries (temporadas, `Season 00`, múltiplos episódios `S01E01-E02`, numeração absoluta de anime e episódios sem número), hierarquia `Series → Season → Episode` e rotas `/Shows/*` em `api/jellyfin`       | Navegação completa por Séries → Temporadas → Episódios                     |
| 4       | `playback` (Direct Play) + Stream `storage` | `storage`, `playback`, `api/jellyfin`           | Rota HTTP pura `GET /locations/{LocationID}/files/{FileID}` (com `Range`/`sendfile`) no `storage`, sessões ativas no `playback` (`kv.Store`) emitindo eventos `Playback*`, e `PlaybackInfo` + proxy de streaming em `api/jellyfin` | Reprodução de filmes e episódios com seek (HTTP `206`) e eventos de sessão |
| 5       | `subtitles` (Legendas `.srt`)               | `subtitles`, `library`, `api/jellyfin`          | Match bidirecional por regex de legendas locais `.srt` via eventos do `storage` e da `library`, emissão de `SubtitleAdded`/`SubtitleRemoved`, consolidação na `library` e `DeliveryUrl` + proxy de legenda em `api/jellyfin`       | Seleção e exibição de legendas externas `.srt` durante a reprodução        |

### 11.2 Segundo momento (Pós-MVP)

Sem ordem definida:

- **Backends distribuídos e Escala Horizontal:** `database/postgres` (1 schema
  por módulo + migrations `postgres/`), `pubsub/nats` (NATS JetStream com
  `Nats-Msg-Id` e consumer groups por módulo) e `kv/valkey`, permitindo rodar
  apenas o `storage` no NAS e escalar os demais módulos em outras máquinas.
- **Módulo `watchstate`:** persistência de estado e progresso por `ProfileID`
  (assistido, favorito, posição, contagem de reproduções), filas de continuar
  assistindo / próximos episódios, contadores de itens não assistidos e _watch
  history_.
- **Módulo `transcoder` e localizações de sistema no `storage`:** transcode de
  áudio HLS (`-c:v copy`) controlado por `SIGSTOP`/`SIGCONT`/`SIGTERM`,
  conversão de legendas (`.srt`/`.ass` → `.vtt`) e localizações `cache`,
  `transcodes` e `images` com `PUT`, TTL e URLs assinadas.
- **Compatibilidade adicional Jellyfin (`plugins/api/jellyfin`):** validação e
  suporte aos clientes `jellyfin-android` e `jellyfin-tizen`, fluxo
  `QuickConnect`, protocolo `SyncPlay` + WebSocket (`/socket`), e exposição de
  `Chapters` / `MediaSegments` (`GET /MediaSegments/{itemId}`).
- Analisadores de hash no `storage` (SHA-256, ED2K), com detecção de
  arquivos movidos entre localizações pelo SHA-256, e observação em
  tempo real (`fsnotify`).
- Módulo `metadata` com plugins (TVDB, TMDB, AniDB, AniSkip, IntroDB, ...),
  mantendo a identificação pelo path como fallback.
- Jobs em background no `transcoder`: extração de frames de episódios,
  imagens de capítulos, geração de _trickplay_, extração de legendas
  embutidas e detecção de abertura/créditos por áudio (`chromaprint`) e
  vídeo.
- Imagens na `library` vindas dos eventos do `metadata`, do `transcoder` e
  de arquivos locais.
- SSO via OIDC no `identity`, avatares de perfil e migração de perfis entre
  contas (mantendo todo o histórico do `watchstate`).
- Plugins de sincronização externa no `watchstate` (Trakt, AniList, Simkl).
- Pipeline de aquisição (`indexer`, `downloader`, `acquisition`), `translation`
  e `notification`.
- Interface web própria.

## 12. Pontos em aberto

Nenhum no momento. Novas dúvidas devem ser listadas aqui até serem
decididas.
