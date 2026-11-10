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
   - Toda comunicação síncrona de controle entre módulos é definida em arquivos `.proto` e gerada via `buf` (`protoc-gen-go` e `protoc-gen-connect-go`) dentro de `internal/gen/<módulo>/v1`.
   - **Não crie pacotes `router/` ou `client/` manuais para módulos.**
2. **Pacote `handler/` em cada módulo:**
   - Cada módulo implementa um pacote `handler/` cuja struct `Handler` satisfaz a interface `<Service>Handler` gerada pelo ConnectRPC:
     ```go
     var _ <mod>v1connect.<Service>Handler = (*Handler)(nil)
     ```
   - O `module.go` registra `mux.Handle(<mod>v1connect.New<Service>Handler(h))` no `http.ServeMux` do módulo.
3. **In-Process Transport (Zero-Copy) no Monolito vs. HTTP/2 (`h2c`) no Protocolo Padrão Connect nos Microserviços:**
   - Em `internal/clients/clients.go`, a struct `Clients` armazena as interfaces `<Service>Client` geradas pelo ConnectRPC.
   - No `cmd/server`:
     - **Modo Monolito (`MSERVER_<MÓDULO>_URL` vazio):** utilize o `In-process transport` do ConnectRPC (`connectinprocess`) sobre o `mux` do módulo, desativando a função de cópia para trafegar puramente por ponteiros (Zero-Copy):

       ```go
       server := connect.NewServer()
       transport := connectinprocess.New(server, connectinprocess.WithCopyFunc(
           func(src any) any {
               return src // Zero-Copy: retorna a mesma referência de memória
           },
       ))

       client := <mod>v1connect.New<Service>Client(connect.NewClient(transport))
       ```

     - **Modo Microserviços (`MSERVER_<MÓDULO>_URL` definido):** instancie `New<Service>Client(h2cClient, url)` usando `h2c` (HTTP/2 Cleartext) no **protocolo padrão do Connect** (sem `connect.WithGRPC()`).

4. **Regras de Ouro do Zero-Copy (`connectinprocess`):**
   - **1. Trate os Payloads como Imutáveis:**
     - O único risco do Zero-Copy é o efeito colateral (_side effects_). Se o Módulo A enviar uma struct contendo um slice, e o Módulo B modificar diretamente um elemento desse slice, o Módulo A verá a alteração.
     - **Regra de ouro:** considere que o payload recebido em um `Handler` (ou retornado para um cliente) é **somente leitura**.
     - Se o Módulo B precisar modificar o dado recebido para processá-lo internamente ou salvá-lo, ele deve fazer uma cópia local ou criar uma nova struct com as alterações.
   - **2. Evite o Reaproveitamento de Ponteiros no Módulo Emissor:**
     - Se o cliente (Módulo A) envia mensagens em um loop ou canal, instancie sempre uma **nova struct Protobuf a cada iteração** (`req := &pb.MinhaReq{...}`), em vez de criar uma única struct fora do loop e apenas alterar os campos dela antes de cada chamada. Como o servidor lê a memória original no Zero-Copy, alterar a struct enquanto o servidor ainda a processa geraria uma _Race Condition_.
5. **Mapeamento de Erros:**
   - O `service/` retorna erros sentinela de domínio (`ErrInvalidArgument`, `ErrNotFound`, `ErrAlreadyExists`, `ErrUnauthenticated`, `ErrPermissionDenied`, `ErrFailedPrecondition`).
   - O `handler/` traduz os erros do `service/` usando `connect.NewError(connect.Code..., err)`.
   - Os consumidores verificam erros exclusivamente via `connect.CodeOf(err) == connect.Code...`.
6. **Exceção — Plano de Dados do `storage` (HTTP Puro):**
   - **Bytes brutos de mídia (vídeo, áudio, legendas, imagens) NUNCA trafegam por Protobuf/ConnectRPC nem pelo `pubsub`.**
   - O módulo `storage` serve arquivos via **HTTP puro** em `GET /locations/{LocationID}/files/{FileID}` (e `HEAD`/`PUT`) usando `http.ServeContent` para garantir suporte a `Range` (`206 Partial Content`) e _zero-copy_ (`sendfile` no kernel Linux).

---

### Pilar 2: Banco de Dados Unificado (SQL Manual Portável para SQLite e Postgres)

1. **Sem ORMs, sem Wrappers sobre `*sql.DB` e tudo direto em `store/`:**
   - Não use ORMs (`gorm`, `ent`, `bun`) nem geradores de queries (`sqlc`), e não crie wrappers que reescrevam strings SQL em tempo de execução.
   - Como haverá apenas uma implementação de banco de dados por módulo, **todo o código de persistência fica diretamente dentro de `internal/modules/<módulo>/store/`** (sem subpasta `sqlstore` ou `sqlite`), operando sobre `*sql.DB` / `database.Conn`.
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

   - **Paginação (`LIMIT` / `OFFSET`):** **NUNCA** use `LIMIT -1` (causa erro de execução no Postgres). Só concatene `LIMIT $N` na query quando `Limit > 0` e `OFFSET $N` quando `Offset > 0`.
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
   - Ambas embutidas via `//go:embed migrations/*/*.sql` e selecionadas conforme `MSERVER_DB_DRIVER` (`sqlite` ou `postgres`).
   - No Postgres, cada módulo roda confinado em seu próprio schema (`CREATE SCHEMA IF NOT EXISTS <módulo>` e `search_path=<módulo>`).

---

### Pilar 3: Mensageria (`pubsub`), KV (`kv`) e ECST sem Outbox

1. **Sem Tabela `outbox`:**
   - O projeto **não utiliza** tabela `outbox` no banco de dados (evitando o dobro de I/O de escrita em cartões SD/discos do NAS).
   - O `service` faz a escrita no `store` e, logo após o commit, publica os eventos diretamente via `pubsub.Publisher`.
   - Não propague transações de banco pelo `context.Context` para fins de publicação de eventos; transações (`WithTx`) são internas à operação de persistência do `store`.
2. **Contrato do `internal/pubsub`:**
   - Todo evento usa o envelope CloudEvents 1.0 (`pubsub.Envelope[T]`) com `ID` **UUIDv7**.
   - `pubsub.Message` deve carregar `{ID string, Topic string, Payload []byte}`, onde `ID` é o `UUIDv7` do envelope (usado como `Nats-Msg-Id` no NATS JetStream para deduplicação nativa).
   - `pubsub.Subscriber` tem a assinatura `Subscribe(ctx context.Context, consumer, topic string, h Handler) error`, onde `consumer` é o nome do módulo consumidor (ex.: `"library"`, `"subtitles"`), permitindo que o NATS JetStream balanceie a carga entre réplicas do mesmo módulo sem duplicar entregas.
3. **Idempotência e `Resync`:**
   - Todo handler de evento deve ser **100% idempotente** (fazer `UPSERT` pelo ID ou ignorar eventos já aplicados/antigos).
   - Todo módulo produtor de estado deve implementar métodos de `Resync` em seu serviço, capazes de reler o estado atual do banco e republicar os eventos no `pubsub` (usados para _cold start_, autocura após desligamento abrupto e reconciliação periódica `MSERVER_RESYNC_INTERVAL`).
4. **Contrato do `internal/kv`:**
   - Usado para estado efêmero compartilhado entre réplicas (ex.: sessões ativas do `playback` e mapeamento `PlaySessionId` do plugin Jellyfin).
   - Restrito às operações atômicas nativas do Valkey (`Get`, `Create` [`SET NX PX`], `Update` [`SET XX PX`], `Delete` [`DEL`], `List` [`SCAN`]), sem scripts Lua.

---

### Pilar 4: Estrutura de Pastas e Fronteiras de Módulo

```text
internal/modules/<módulo>/
├── service/               # Regra de negócio pura, interface Service, erros sentinela e Resync
├── handler/               # Implementação ConnectRPC (<Service>Handler)
├── events/                # Constantes de tópicos e structs de payload versionadas (ex.: MovieEventV1)
├── store/                 # Interface Store, models, erros, queries.go e implementação SQL única ($1, $2)
│   ├── queries.go         # Constantes com todas as queries SQL base do módulo
│   └── migrations/        # Migrations embutidas (go:embed): sqlite/*.sql e postgres/*.sql
└── module.go              # New/Close: inicializa store, service, handler, subscrições e plugins
```

**Regras de Importação (Fronteiras Rígidas):**

- Um módulo **JAMAIS** pode importar `service/`, `store/` ou `handler/` de outro módulo.
- Para chamar outro módulo, importe apenas a interface gerada em `internal/gen/<outro>/v1/<outro>v1connect` (recebida via `clients.Clients`).
- Para produzir ou consumir eventos, importe apenas `internal/modules/<módulo>/events` e `internal/pubsub`.
- Apenas o módulo `storage` pode importar `os.Root` ou manipular arquivos de mídia no sistema de arquivos.
- Divida cada pacote em arquivos pequenos e coesos por contexto (ex.: `accounts.go`, `profiles.go`, `sessions.go`), evitando arquivos monolíticos.

---

## 3. Estilo de Código, Testes e Git

- **Simplicidade e Clareza:** Escreva código Go idiomático, direto e explícito. Evite `reflect`, abstrações genéricas complexas ou "mágica" difícil de depurar (_"Clear is better than clever"_).
- **Testes:**
  - Sempre execute `go test ./...` após alterações para garantir que todos os testes passem.
  - Parsers de regex (`library`, `subtitles`) devem ter testes baseados em tabela cobrindo casos de borda.
- **Commits (quando solicitados pelo usuário):**
  - Siga o padrão **Conventional Commits** e escreva as mensagens **em português**.
  - Linha de assunto no modo imperativo, capitalizada, sem ponto final e limitada a ~50 caracteres.
