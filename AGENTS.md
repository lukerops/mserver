# AGENTS.md — Diretrizes de Arquitetura e Desenvolvimento do `mserver`

Este documento contém as regras inegociáveis de arquitetura, design e implementação do **mserver**. Qualquer agente de IA ou desenvolvedor trabalhando neste repositório **deve** seguir estas instruções estritamente.

Consulte também os documentos de referência em `docs/`:

- [`docs/planejamento.md`](docs/planejamento.md) — Decisões de arquitetura e princípios.
- [`docs/modulos-e-eventos.md`](docs/modulos-e-eventos.md) — Contexto de cada módulo e catálogo de eventos.
- [`docs/roadmap.md`](docs/roadmap.md) — Entregas do MVP e validação OpenAPI do Jellyfin.

---

## 1. Visão Geral e Governança de Escopo (Regras Inegociáveis)

1. **Isolamento Total do Jellyfin em `plugins/api/jellyfin`:**
   - **100% das regras de negócio, rotas, DTOs, unidades (como `.NET Ticks`), parsing de `<username ou email>#<profile>`, `DeviceProfile`/`PlaybackInfo`, `MediaSources`/`MediaStreams`/`DeliveryUrl`, `UserData`, `QuickConnect` e `SyncPlay` referentes ao Jellyfin devem viver estritamente dentro de `plugins/api/jellyfin`.**
   - Nenhum módulo de domínio (`identity`, `storage`, `library`, `playback`, `subtitles`, etc.) pode conhecer conceitos, nomes ou formatos do Jellyfin.
2. **Proibição de Conceitos Não Discutidos Fora do Plugin Jellyfin:**
   - **Nenhum conceito, campo, método RPC, tabela ou evento que não tenha sido previamente discutido e aprovado pelo usuário deve ser adicionado fora de `plugins/api/jellyfin`.**
   - Se uma tarefa parecer exigir uma mudança nos contratos dos módulos internos, pare e pergunte ao usuário antes de implementar.
3. **Execução Híbrida (Monolito Autossuficiente vs. Microserviços Distribuídos):**
   - O mesmo binário deve rodar como **monolito autossuficiente** em hardwares modestos como Raspberry Pi e NAS (SQLite + `pubsub/memory` + `kv/memory` + chamadas entre módulos em memória) ou como **microserviços distribuídos** para escala horizontal (Postgres + NATS JetStream + Valkey + chamadas remotas via HTTP/2), mudando apenas variáveis de ambiente.

---

## 2. Os 4 Pilares Técnicos e Padrões de Código

### Pilar 1: Comunicação Síncrona entre Módulos (ConnectRPC + Protobuf + In-Process)

1. **Contratos em Protobuf (`proto/<módulo>/v1/<módulo>.proto`):**
   - Usa-se o **connect-go v2** (`connectrpc.com/connect/v2`): handlers e clientes usam a assinatura simples `func(ctx, *Request) (*Response, error)`, o servidor é um `*connect.Server` e o transporte HTTP vem de `connecthttp`.
   - Toda comunicação síncrona de controle entre módulos é definida em arquivos `.proto` e gerada via `buf` (`protoc-gen-go` e `protoc-gen-connect-go`) dentro de `gen/<módulo>/v1`. Gere com `make generate` (o `buf` roda via `go run`, em versão fixa); o código gerado é versionado e **nunca editado à mão**.
   - **Não crie pacotes `router/` ou `client/` manuais para módulos.**
2. **Pacote `handler/` em cada módulo:**
   - Cada módulo implementa um pacote `handler/` cuja struct `Handler` satisfaz a interface `<Service>Handler` gerada pelo ConnectRPC:
     ```go
     var _ <mod>v1connect.<Service>Handler = (*Handler)(nil)
     ```
   - O `module.go` registra o handler no `*connect.Server` recebido do `cmd/server`: `<mod>v1connect.Register<Service>Handler(server, h)`. Só o `cmd/server` expõe o server por HTTP, com `connecthttp.Mount(mux, server)`.
   - Embutir `<mod>v1connect.Unimplemented<Service>Handler` no `Handler` não é permitido: toda RPC do contrato deve ser implementada explicitamente.
3. **In-Process Transport no Monolito vs. HTTP/2 (`h2c`) no Protocolo Padrão Connect nos Microserviços:**
   - Em `internal/clients/clients.go`, a struct `Clients` armazena as interfaces `<Service>Client` geradas pelo ConnectRPC.
   - No `cmd/server`:
     - **Modo Monolito (`MSERVER_<MÓDULO>_URL` vazio):** utilize o `In-process transport` do ConnectRPC (`connectinprocess`) sobre o `*connect.Server`, sem rede e sem serialização (as mensagens são copiadas pelo `ProtoCopy` padrão do Connect):

       ```go
       // server é o *connect.Server em que os módulos locais registraram seus handlers
       transport := connectinprocess.New(server)

       client := <mod>v1connect.New<Service>Client(connect.NewClient(transport))
       ```

     - **Modo Microserviços (`MSERVER_<MÓDULO>_URL` definido):** instancie o cliente com `<mod>v1connect.New<Service>Client(connect.NewClient(connecthttp.NewTransport(h2cClient, url)))`, usando `h2c` (HTTP/2 Cleartext: `http.Transport` com `Protocols.SetUnencryptedHTTP2(true)`) no **protocolo padrão do Connect** (sem gRPC nem gRPC-Web).
       - O servidor HTTP do `cmd/server` usa `http.Protocols` com `SetHTTP1(true)` e `SetUnencryptedHTTP2(true)` (h2c nativo, sem `x/net`).

4. **Mapeamento de Erros:**
   - O `service/` retorna erros sentinela de domínio (`ErrInvalidArgument`, `ErrNotFound`, `ErrAlreadyExists`, `ErrUnauthenticated`, `ErrPermissionDenied`, `ErrFailedPrecondition`).
   - O `handler/` traduz os erros do `service/` usando `connect.NewError(connect.Code..., err.Error()).WithCause(err)`.
   - Os consumidores verificam erros exclusivamente via `connect.CodeOf(err) == connect.Code...`.
5. **Exceção — Plano de Dados do `storage` (HTTP Puro):**
   - **Bytes brutos de mídia (vídeo, áudio, legendas, imagens) NUNCA trafegam por Protobuf/ConnectRPC nem pelo `pubsub`.**
   - O módulo `storage` serve arquivos via **HTTP puro** em `GET /locations/{LocationID}/files/{FileID}` (e `HEAD`/`PUT`) usando `http.ServeContent` para garantir suporte a `Range` (`206 Partial Content`) e _zero-copy_ (`sendfile` no kernel Linux).

---

### Pilar 2: Banco de Dados Unificado (SQL Manual Portável para SQLite e Postgres)

1. **Sem ORMs, sem Wrappers sobre `*sql.DB` e tudo direto em `store/`:**
   - Não use ORMs (`gorm`, `ent`, `bun`) nem geradores de queries (`sqlc`), e não crie wrappers que reescrevam strings SQL em tempo de execução.
   - Como haverá apenas uma implementação de banco de dados por módulo, **todo o código de persistência fica diretamente dentro de `internal/modules/<módulo>/store/`** (sem subpasta `sqlstore` ou `sqlite`), operando sobre `*sql.DB`. `Store` é uma struct concreta (não uma interface), criada com `store.New(db)`; os testes dos módulos usam um SQLite em memória em vez de mocks. Se um consumidor precisar de um fake, ele declara a própria interface mínima.
     - **Transações são internas ao `store`:** um `withTx` privado, usado só pelos métodos que precisam de atomicidade (ex.: `ApplyScan`). Dentro dele, todas as queries usam o `*sql.Tx` (o pool do SQLite tem uma única conexão) e as linhas são fechadas antes da próxima query.
   - **Queries base em `queries.go`:** todas as queries SQL estáticas/base do módulo devem ser declaradas como constantes em um arquivo dedicado `internal/modules/<módulo>/store/queries.go` (apenas a concatenação condicional de cláusulas em queries dinâmicas fica nos métodos em si, ainda partindo da query base definida em `queries.go`).
2. **Regras Estritas de SQL Portável (SQLite + Postgres):**
   - **Placeholders sempre no padrão `$1, $2, $3...`:** nunca use `?` ou `?1`. Tanto o SQLite moderno quanto o Postgres (`pgx`) suportam `$1, $2` nativamente.
   - **Queries Dinâmicas (`WHERE` / `IN`):** use sempre funções auxiliares baseadas no tamanho atual do slice `args` para evitar desalinhamento de índices:

     ```go
     func arg(args *[]any, v any) string {
         *args = append(*args, v)
         return fmt.Sprintf("$%d", len(*args))
     }

     func inClause[T any](args *[]any, vals []T) string {
         ph := make([]string, len(vals))
         for i, v := range vals {
             ph[i] = arg(args, v)
         }
         return "(" + strings.Join(ph, ", ") + ")"
     }
     ```

   - **Paginação (`LIMIT` / `OFFSET`):** **NUNCA** use `LIMIT -1` (causa erro de execução no Postgres). Só concatene `LIMIT $N` na query quando `Limit > 0` e `OFFSET $N` quando `Offset > 0`. Como o SQLite não aceita `OFFSET` sem `LIMIT`, um `Offset` sozinho usa `LIMIT` com `math.MaxInt64`.
     - **Coleções grandes são paginadas** (`Limit`/`Offset`) nos contratos e no `store`.
   - **Upserts:** use sempre `INSERT INTO ... VALUES ($1, ...) ON CONFLICT (...) DO UPDATE SET col = excluded.col`.
   - **Tipos de Dados nas Colunas e no Scan:**
     - **IDs:** `uuid.UUID` (`TEXT` no SQLite, `UUID` no Postgres). Para UUIDs anuláveis, use `uuid.NullUUID`.
     - **Timestamps:** armazene sempre como `int64` em nanossegundos UTC (`t.UnixNano()` → `INTEGER` no SQLite, `BIGINT` no Postgres) e reconstrua com `time.Unix(0, n).UTC()`.
     - **Booleanos:** `bool` (`INTEGER` 0/1 no SQLite, `BOOLEAN` no Postgres).
     - **Violação de Unicidade:** verifique sempre com `database.IsUniqueViolation(err)` (que detecta tanto `"UNIQUE constraint failed"` do SQLite quanto `SQLSTATE 23505` do Postgres).

3. **Migrations Separadas por Dialeto:**
   - Dentro de `internal/modules/<módulo>/store/migrations/`, mantenha:
     - `sqlite/*.sql`
     - `postgres/*.sql`
   - Ambas embutidas via `//go:embed migrations/*/*.sql` (exportadas como `store.Migrations`) e selecionadas conforme `MSERVER_DB_DRIVER` (`sqlite` ou `postgres`).
   - O `module.go` abre o banco com `database.Open(ctx, cfg, "<módulo>", store.Migrations)` e cria o store com `store.New(db)`.
   - No Postgres, cada módulo roda confinado em seu próprio schema (`CREATE SCHEMA IF NOT EXISTS <módulo>` e `search_path=<módulo>`).

---

### Pilar 3: Mensageria (`pubsub`), KV (`kv`) e ECST sem Outbox

1. **Sem Tabela `outbox`:**
   - O projeto **não utiliza** tabela `outbox` no banco de dados (evitando o dobro de I/O de escrita em cartões SD/discos do NAS).
   - O `service` faz a escrita no `store` e, logo após o commit, publica os eventos diretamente via `pubsub.Publisher`.
   - Não propague transações de banco pelo `context.Context` para fins de publicação de eventos; transações (`withTx`) são internas à operação de persistência do `store`.
2. **Contrato do `internal/pubsub`:**
   - Todo evento usa o envelope CloudEvents 1.0 (`pubsub.Envelope[T]`) com `ID` **UUIDv7**.
   - `pubsub.Message` deve carregar `{ID string, Topic string, Payload []byte}`, onde `ID` é o `UUIDv7` do envelope (usado como `Nats-Msg-Id` no NATS JetStream para deduplicação nativa).
   - `pubsub.Subscriber` tem a assinatura `Subscribe(ctx context.Context, consumer, topic string, h Handler) error`, onde `consumer` é o nome do módulo consumidor (ex.: `"library"`, `"subtitles"`), permitindo que o NATS JetStream balanceie a carga entre réplicas do mesmo módulo sem duplicar entregas.
3. **Idempotência e `Resync`:**
   - Todo handler de evento deve ser **100% idempotente** (fazer `UPSERT` pelo ID ou ignorar eventos já aplicados/antigos).
   - Todo módulo produtor de estado persistido deve implementar métodos de `Resync` em seu serviço, capazes de reler o estado atual do banco e republicar os eventos no `pubsub` (usados para _cold start_, autocura após desligamento abrupto e reconciliação periódica `MSERVER_RESYNC_INTERVAL`).
4. **Contrato do `internal/kv`:**
   - Usado para estado efêmero compartilhado entre réplicas (ex.: sessões ativas do `playback` e mapeamento `PlaySessionId` do plugin Jellyfin).
   - Restrito às operações atômicas nativas do Valkey (`Get`, `Create` [`SET NX PX`], `Update` [`SET XX PX`], `Delete` [`DEL`], `List` [`SCAN`]), sem scripts Lua.

---

### Pilar 4: Estrutura de Pastas e Fronteiras de Módulo

```text
internal/modules/<módulo>/
├── service/               # Regra de negócio pura (struct Service), erros sentinela e Resync
├── handler/               # Implementação ConnectRPC (<Service>Handler)
├── store/                 # struct Store, models, erros, queries.go e SQL único ($1, $2)
│   ├── queries.go         # Constantes com todas as queries SQL base do módulo
│   └── migrations/        # Migrations embutidas (go:embed): sqlite/*.sql e postgres/*.sql
└── module.go              # New/Close: abre o banco, inicializa store, service, handler (registrado no *connect.Server), subscrições e plugins
```

**Regras de Importação (Fronteiras Rígidas):**

- Um módulo **JAMAIS** pode importar `service/`, `store/` ou `handler/` de outro módulo.
- Para chamar outro módulo, importe apenas a interface gerada em `gen/<outro>/v1/<outro>v1connect` (recebida via `clients.Clients`).
- Para produzir ou consumir eventos, importe apenas `internal/events` e `internal/pubsub`.
- **Todos os eventos vivem em um único pacote, `internal/events`**, separados por contexto em arquivos (ex.: `storage_files.go`, `storage_ffprobe.go`): constantes de tópicos versionados, `Source<Módulo>` e structs de payload com sufixo de versão (`V1`). Módulos não declaram pacote `events/` próprio.
- Apenas o módulo `storage` pode importar `os.Root` ou manipular arquivos de mídia no sistema de arquivos.
- O módulo `storage` expõe também um `Mux` com as rotas HTTP puras do plano de dados; o `cmd/server` o monta ao lado do `connecthttp.Mount`.
- Divida cada pacote em arquivos pequenos e coesos por contexto (ex.: `accounts.go`, `profiles.go`, `sessions.go`), evitando arquivos monolíticos.

---

## 3. Estilo de Código, Testes e Git

- **Simplicidade e Clareza:** Escreva código Go idiomático, direto e explícito. Evite `reflect`, abstrações genéricas complexas ou "mágica" difícil de depurar (_"Clear is better than clever"_).
- **Alocações:** evite alocações e cópias desnecessárias. Entre funções, passe e retorne structs grandes por ponteiro (`*File`, `*Analysis`) (parâmetros e retornos únicos). Slices continuam por valor (`[]File`), pois `[]*File` aloca uma vez por elemento; ao ler linhas do banco, escaneie em uma variável local e faça `append` dela.
- **Logs:** nunca passe pares chave/valor soltos (`"id", id`). Use sempre os atributos tipados do `slog` (`slog.String`, `slog.Int`, `slog.Int64`, `slog.Bool`...), com UUIDs via `.String()`, erros via `slog.String("error", err.Error())` e durações via `slog.Int64("duration", int64(d))`.
- **Testes:**
  - Sempre execute `go test ./...` após alterações para garantir que todos os testes passem.
  - Ao alterar `proto/`, rode `make lint` e `make generate` e inclua o código gerado.
  - Parsers de regex (`library`, `subtitles`) devem ter testes baseados em tabela cobrindo casos de borda.
- **Commits (quando solicitados pelo usuário):**
  - Siga o padrão **Conventional Commits** e escreva as mensagens **em português**.
  - Linha de assunto no modo imperativo, capitalizada, sem ponto final e limitada a ~50 caracteres.
