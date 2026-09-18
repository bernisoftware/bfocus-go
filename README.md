# bfocus-go

SDK oficial em **Go** da API pública do [bFocus](https://bfocus.com.br): clientes, produtos,
release notes, base de conhecimento e agentes de IA.

Só biblioteca padrão (zero dependências) · Go 1.21+ · tipada · `context.Context` em toda chamada ·
novas tentativas e idempotência automáticas · segura para uso concorrente.

## Instalação

```bash
go get github.com/bernisoftware/bfocus-go@v0.1.0
```

## Hello world

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	bfocus "github.com/bernisoftware/bfocus-go"
)

func main() {
	client, err := bfocus.NewClient(os.Getenv("BFOCUS_API_KEY"))
	if err != nil {
		log.Fatal(err) // chave vazia
	}

	cliente, err := client.Customers.Upsert(context.Background(), "ERP 1042", &bfocus.CustomerUpsertParams{
		Name:  bfocus.String("Padaria Estrela"),
		Email: bfocus.String("contato@padaria.example"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(cliente.ID, cliente.Name)
}
```

`Upsert` cria ou atualiza pelo `external_id` do **seu** sistema — rodar de novo não duplica.

## Autenticação

Crie a chave no bFocus em **Integrações → Chaves de API**, marcando só os escopos de que a
integração precisa. Ela vai em `Authorization: Bearer <chave>` em toda requisição (a SDK cuida disso).

| Escopo | Permite |
| --- | --- |
| `customers:read` | Ler clientes, contatos, produtos vinculados e interações |
| `customers:write` | Cadastrar, atualizar e excluir clientes, contatos e interações |
| `products:read` | Ler o catálogo de produtos |
| `products:write` | Cadastrar, atualizar e arquivar produtos |
| `kb:read` | Ler e buscar artigos da base de conhecimento |
| `kb:write` | Criar, atualizar, publicar e excluir artigos da base de conhecimento |
| `ai_agents:read` | Ler os agentes de IA |
| `ai_agents:preview` | Testar a resposta de um agente de IA (consome IA da conta) |
| `release_notes:read` | Ler release notes |
| `release_notes:write` | Criar, atualizar e publicar release notes |

A chave legada (`bf_sk_…`) só alcança clientes (`customers:*`). A base de conhecimento e os agentes
de IA exigem o módulo de Atendimento contratado (sem ele: `MODULE_NOT_CONTRACTED`, veja
[Erros](#erros)). Guarde a chave fora do código:

```go
client, err := bfocus.NewClient(
	os.Getenv("BFOCUS_API_KEY"),
	bfocus.WithBaseURL("https://api.bfocus.com.br"), // padrão; em dev: "http://localhost:8000"
	bfocus.WithTimeout(30*time.Second),              // por tentativa (padrão 30 s)
	bfocus.WithMaxRetries(2),                        // novas tentativas além da primeira (0 desliga)
	// bfocus.WithHTTPClient(meuHTTPClient),         // proxy, transporte próprio, instrumentação…
)
```

`NewClient` não faz chamada de rede. Chave vazia (ou opção inválida) devolve um erro que satisfaz
`errors.Is(err, bfocus.ErrInvalidArgument)`. Crie **um** `*bfocus.Client` por chave e reutilize-o —
ele é seguro para uso concorrente por várias goroutines. `fmt.Print(client)` não mostra a chave.

## Como os métodos funcionam

- **`ctx` primeiro.** Todo método recebe um `context.Context`, que limita a chamada inteira
  (todas as tentativas). Cancelar o `ctx` devolve `ctx.Err()`, sem nova tentativa. O
  `WithTimeout` limita cada tentativa.
- **Retorno tipado e desembrulhado**: o método devolve o `data` da resposta já decodificado
  (`*bfocus.Customer`, `[]bfocus.Product`…). Campos novos que a API passar a devolver são
  ignorados, nunca viram erro (em `CustomField` e `AIAgentPreview`, que a spec declara abertos,
  eles ficam em `Extra`). Datas vêm como `*time.Time`.
- **Listas paginadas** devolvem `*bfocus.Page[T]` (`Items`, `Page`, `PageSize`, `Total`, `Pages`,
  `HasNext()`). Para percorrer tudo, use `ListAll` (clientes, interações, release notes e artigos):
  um iterador preguiçoso que busca página por página (`PageSize` padrão 100) e para na última
  página ou numa página vazia.

  ```go
  it := client.Customers.ListAll(&bfocus.CustomerListParams{Q: bfocus.String("padaria")})
  for it.Next(ctx) {
  	c := it.Current()
  	fmt.Println(c.ExternalID, c.Name)
  }
  if err := it.Err(); err != nil {
  	// erro da API, de rede ou de argumento — os itens já entregues continuam válidos
  }
  ```

- **Parâmetros**: obrigatórios são argumentos; opcionais ficam num struct `…Params` (pode ser
  `nil`). Campos opcionais são ponteiros — use os ajudantes `bfocus.String`, `bfocus.Int`,
  `bfocus.Bool` e `bfocus.Time`.
- **Só o que você passa muda.** Os upserts são parciais: campo `nil` **não é enviado** (fica como
  está). Para enviar `null` e **limpar** o campo, ponha o nome dele em `ClearFields` (nome JSON,
  `"phone"`, ou Go, `"Phone"`; nome desconhecido é erro de argumento antes de ir à rede):

  ```go
  client.Customers.Upsert(ctx, "ERP 1042", &bfocus.CustomerUpsertParams{
  	Phone: bfocus.String("11 3333-4444"), // só o telefone muda
  })
  client.Customers.Upsert(ctx, "ERP 1042", &bfocus.CustomerUpsertParams{
  	ClearFields: []string{"phone"}, // apaga o telefone — corpo {"phone":null}
  })
  ```

  Em slices, `nil` é omitido e um slice vazio vai como `[]` (ex.: `CustomFields:
  []bfocus.CustomFieldInput{}` apaga todos os campos personalizados).
- **Datas** (`UpdatedSince`) são `time.Time`, enviadas em ISO 8601 UTC com `Z`.
- Toda escrita aceita `bfocus.WithIdempotencyKey(...)` como última opção (veja
  [Novas tentativas](#novas-tentativas-e-idempotência)).

## Clientes

```go
ctx := context.Background()

client.Customers.Upsert(ctx, "ERP 1042", &bfocus.CustomerUpsertParams{
	Name:     bfocus.String("Padaria Estrela"),
	Document: bfocus.String("12.345.678/0001-90"),
	CustomFields: []bfocus.CustomFieldInput{ // substitui a lista
		{Key: "plano", Label: "Plano", Value: "ouro"},
	},
})

cliente, err := client.Customers.Get(ctx, "ERP 1042")

pagina, err := client.Customers.List(ctx, &bfocus.CustomerListParams{
	Q: bfocus.String("padaria"), Page: bfocus.Int(1), PageSize: bfocus.Int(50),
})
fmt.Println(pagina.Total, len(pagina.Items))

// Sincronização incremental: tudo o que mudou desde a última rodada, todas as páginas.
desde := time.Now().Add(-time.Hour)
it := client.Customers.ListAll(&bfocus.CustomerListParams{UpdatedSince: bfocus.Time(desde)})
for it.Next(ctx) {
	fmt.Println(it.Current().ExternalID, it.Current().UpdatedAt)
}
if err := it.Err(); err != nil {
	log.Fatal(err)
}

client.Customers.Delete(ctx, "ERP 1042")
```

### Contatos, produtos vinculados e interações

```go
client.Customers.Contacts.Upsert(ctx, "ERP 1042", "CT-1", &bfocus.ContactUpsertParams{
	Name: bfocus.String("Ana Souza"), Role: bfocus.String("Financeiro"),
	Email: bfocus.String("ana@padaria.example"), IsPrimary: bfocus.Bool(true),
})
client.Customers.Contacts.List(ctx, "ERP 1042")
client.Customers.Contacts.Delete(ctx, "ERP 1042", "CT-1")

client.Customers.Products.Attach(ctx, "ERP 1042", "erp-cloud")
client.Customers.Products.List(ctx, "ERP 1042")
client.Customers.Products.Detach(ctx, "ERP 1042", "erp-cloud")

client.Customers.Interactions.Create(ctx, "ERP 1042", "Pedido 1042 faturado.",
	&bfocus.InteractionCreateParams{AuthorEmail: bfocus.String("carla@suaempresa.com.br")})
for it := client.Customers.Interactions.ListAll("ERP 1042", nil); it.Next(ctx); {
	fmt.Println(it.Current().CreatedAt, it.Current().Content)
}
```

## Produtos

```go
client.Products.Upsert(ctx, "erp-cloud", &bfocus.ProductUpsertParams{
	Name: bfocus.String("ERP Cloud"), Description: bfocus.String("Gestão na nuvem"),
	Color: bfocus.String("#6366F1"),
})
client.Products.Get(ctx, "erp-cloud")
client.Products.List(ctx, &bfocus.ProductListParams{IncludeInactive: bfocus.Bool(true)})
client.Products.Archive(ctx, "erp-cloud") // arquiva, não apaga
```

## Release notes — publicar direto do CI

Um passo no pipeline de release: cria ou atualiza a nota da versão e já publica.

```go
// cmd/publicar-release-note/main.go — roda no CI a cada tag
package main

import (
	"context"
	"log"
	"os"
	"strings"

	bfocus "github.com/bernisoftware/bfocus-go"
)

func main() {
	client, err := bfocus.NewClient(os.Getenv("BFOCUS_API_KEY")) // escopo release_notes:write
	if err != nil {
		log.Fatal(err)
	}
	versao := os.Getenv("GITHUB_REF_NAME") // "v2.3.0" — o "v" na frente é aceito
	texto, err := os.ReadFile("release-notes/" + versao + ".md")
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.ReleaseNotes.Upsert(context.Background(), "erp-cloud", versao, &bfocus.ReleaseNoteUpsertParams{
		Title:               bfocus.String("Versão " + strings.TrimPrefix(versao, "v")),
		DescriptionMarkdown: bfocus.String(string(texto)),
		Audience:            bfocus.String("external"), // "internal" | "external" | "both"
		Publish:             bfocus.Bool(true),         // cria/atualiza e publica numa chamada só
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

Rodar de novo para a mesma versão atualiza a nota (é upsert). Também há:

```go
client.ReleaseNotes.Get(ctx, "erp-cloud", "2.3.0")
client.ReleaseNotes.List(ctx, "erp-cloud", &bfocus.ReleaseNoteListParams{Published: bfocus.Bool(false)}) // rascunhos
client.ReleaseNotes.Publish(ctx, "erp-cloud", "2.3.0")
```

## Base de conhecimento — sincronizar a partir de arquivos Markdown

Mantenha a documentação no repositório e sincronize a cada push. `BatchUpsert` aceita **qualquer
quantidade** de artigos: a SDK divide em lotes de 100 (o limite da API), envia em sequência e
devolve um único resultado.

```go
client, err := bfocus.NewClient(os.Getenv("BFOCUS_API_KEY")) // escopos kb:read e kb:write
if err != nil {
	log.Fatal(err)
}
ctx := context.Background()

var artigos []bfocus.KBBatchArticle
err = filepath.WalkDir("docs", func(path string, d fs.DirEntry, err error) error {
	if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
		return err
	}
	texto, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel("docs", strings.TrimSuffix(path, ".md"))
	titulo := filepath.Base(rel)
	for _, linha := range strings.Split(string(texto), "\n") {
		if strings.HasPrefix(linha, "# ") {
			titulo = strings.TrimSpace(linha[2:])
			break
		}
	}
	artigos = append(artigos, bfocus.KBBatchArticle{
		// id estável e SEM "/": o caminho do arquivo com ":" no lugar das barras.
		// Aceita letras, números e . _ : ~ @ + = -
		ExternalID:   "git:" + strings.ReplaceAll(filepath.ToSlash(rel), "/", ":"),
		Title:        bfocus.String(titulo),
		BodyMarkdown: bfocus.String(string(texto)),
		Product:      bfocus.String("erp-cloud"), // ou ClearFields: []string{"product"} para um artigo global
	})
	return nil
})
if err != nil {
	log.Fatal(err)
}

res, err := client.KB.Articles.BatchUpsert(ctx, artigos)
if err != nil {
	log.Fatal(err) // res traz o agregado dos lotes já gravados; rodar de novo é seguro
}
fmt.Printf("%d criados, %d atualizados, %d sem mudança, %d com falha\n",
	res.Created, res.Updated, res.Unchanged, res.Failed)

for _, r := range res.Results { // na mesma ordem enviada
	switch {
	case !r.OK:
		fmt.Println("falhou:", r.ExternalID, *r.Error) // ex.: KB_ARTICLE_TITLE_REQUIRED
	case *r.Action == "created" || *r.Action == "updated":
		client.KB.Articles.Publish(ctx, r.ExternalID) // publica o que entrou ou mudou
	}
}

// Remove do bFocus o que saiu do repositório.
locais := map[string]bool{}
for _, a := range artigos {
	locais[a.ExternalID] = true
}
for it := client.KB.Articles.ListAll(&bfocus.KBArticleListParams{Product: bfocus.String("erp-cloud")}); it.Next(ctx); {
	if ext := it.Current().ExternalID; ext != nil && strings.HasPrefix(*ext, "git:") && !locais[*ext] {
		client.KB.Articles.Delete(ctx, *ext)
	}
}
```

Um item com problema não derruba os outros: ele volta com `OK == false` e o motivo em `Error`. Para
publicar já no lote, mande `Status: bfocus.String("published")` em cada item. Artigo a artigo:

```go
client.KB.Articles.Upsert(ctx, "notion:emitir-nfse", &bfocus.KBArticleUpsertParams{
	Title:        bfocus.String("Como emitir NFS-e"),
	BodyMarkdown: bfocus.String("# Passo a passo\n\n1. Abra o menu **Fiscal**"),
	Status:       bfocus.String("published"),
	ClearFields:  []string{"product"}, // artigo global
})
client.KB.Articles.Get(ctx, "notion:emitir-nfse") // artigo completo, com BodyHTML
client.KB.Articles.List(ctx, &bfocus.KBArticleListParams{Status: bfocus.String("draft"), Q: bfocus.String("nota")}) // Page de resumos
client.KB.Articles.Unpublish(ctx, "notion:emitir-nfse")
client.KB.Articles.Delete(ctx, "notion:emitir-nfse")
```

### Busca

```go
hits, err := client.KB.Search(ctx, "como emitir nota fiscal",
	&bfocus.KBSearchParams{Product: bfocus.String("erp-cloud"), Limit: bfocus.Int(3)})
for _, hit := range hits {
	fmt.Println(hit.Title, "—", hit.Excerpt)
}
```

## Agentes de IA

```go
agentes, err := client.AIAgents.List(ctx)
agente, err := client.AIAgents.Get(ctx, agentes[0].ID)

resposta, err := client.AIAgents.Preview(ctx, agente.ID, "Como emito uma NFS-e?", &bfocus.AIAgentPreviewParams{
	History: []bfocus.AIAgentPreviewTurn{
		{Role: "customer", Content: "Oi"},
		{Role: "bot", Content: "Olá! Como posso ajudar?"},
	},
})
fmt.Println(resposta.Action, resposta.AnswerHTML, resposta.Sources)
```

`Preview` consome IA da conta (escopo `ai_agents:preview`).

## Erros

Qualquer resposta fora de 2xx devolve um `*bfocus.Error` com `Code`, `Message`, `Status`,
`RequestID`, `Validation`, `RetryAfter` e `RequiredScope`. O tipo sai do status, e cada um tem um
sentinela para `errors.Is`:

| Sentinela (`errors.Is`) | `Type` | Quando |
| --- | --- | --- |
| `ErrAuthentication` | `authentication` | 401 — chave ausente, inválida ou revogada |
| `ErrPermissionDenied` | `permission_denied` | 403 — chave desligada, IP não liberado, escopo faltando (`RequiredScope`) ou módulo não contratado (`MODULE_NOT_CONTRACTED`) |
| `ErrNotFound` | `not_found` | 404 |
| `ErrConflict` | `conflict` | 409 — ex.: `KB_ARTICLE_EMPTY`, `AI_DISABLED`, `RELEASE_NOTE_CONFLICT` |
| `ErrValidation` | `validation` | 422 — motivos por campo em `Validation` |
| `ErrRateLimit` | `rate_limit` | 429 — `RetryAfter` (depois de esgotar as novas tentativas) |
| `ErrServer` | `server` | 5xx |
| `ErrNetwork` | `network` | conexão/tempo esgotado — `Status == 0`, `Code == "NETWORK_ERROR"` |
| — | `api` | qualquer outro status (ex.: 400) |

**Decida pelo `Code`** — ele é estável (`CUSTOMER_NOT_FOUND`, `INTEGRATION_SCOPE_MISSING`,
`VALIDATION_ERROR`…). O `Message` é texto para humanos e pode mudar. Ao falar com o suporte,
informe o `RequestID`: ele vem do corpo da resposta, senão do header `X-Request-Id`, senão é o id
que a própria SDK enviou (a API ecoa o do cliente) — então está sempre preenchido, inclusive em erro
de rede.

```go
_, err := client.Customers.Get(ctx, "ERP 9999")

var e *bfocus.Error
switch {
case err == nil:
case errors.Is(err, bfocus.ErrNotFound):
	fmt.Println("não existe")
case errors.As(err, &e) && e.Code == "INTEGRATION_SCOPE_MISSING":
	fmt.Println("a chave não tem o escopo", e.RequiredScope)
case errors.As(err, &e) && e.Code == "VALIDATION_ERROR":
	fmt.Println(e.Validation) // map[email:value is not a valid email address]
case errors.As(err, &e):
	fmt.Println(e.Code, e.Status, e.RequestID)
default:
	fmt.Println(err) // ctx cancelado ou erro de argumento
}
```

Se uma resposta 2xx chegar sem o envelope JSON da API (um proxy devolvendo HTML, corpo vazio), a SDK
não devolve `nil` calado: devolve `*bfocus.Error` com `Code == "INVALID_RESPONSE"` (`Type` `api`) e
o status recebido. Corpo de erro que não é JSON vira `Code == "HTTP_<status>"`.

Argumento inválido no seu código (chave vazia; parâmetro de caminho vazio, `"."` ou `".."`; `/` no
`external_id` de um artigo; nome desconhecido em `ClearFields`; `Page`/`PageSize` < 1) devolve, na
hora e sem chamar a API, um erro que satisfaz `errors.Is(err, bfocus.ErrInvalidArgument)` — e que
**não** é `*bfocus.Error`.

## Novas tentativas e idempotência

A SDK tenta de novo sozinha em **erro de rede/tempo esgotado, 429, 502, 503 e 504** — até
`WithMaxRetries` vezes (padrão 2). Espera o `Retry-After` quando a API manda (segundos ou data HTTP,
teto de 60 s); senão 0,5 s, 1 s, 2 s… (teto de 8 s) + até 25% de variação aleatória. Um 500 ou outro
4xx volta na hora. A espera respeita o `ctx`.

Toda escrita (POST/PUT/DELETE) leva um `Idempotency-Key`, e **a mesma chave vai em todas as
tentativas** da chamada: se a primeira chegou a executar e só a resposta se perdeu, a API devolve a
resposta original (`Idempotent-Replayed: true`) em vez de executar de novo. O `X-Request-Id` também
se repete, para o suporte ver as tentativas como uma chamada só.

Para que a proteção valha também quando o **seu** processo roda de novo (um job reexecutado), passe
uma chave derivada do evento:

```go
client.Customers.Interactions.Create(ctx, "ERP 1042", "Pedido 1042 faturado.", nil,
	bfocus.WithIdempotencyKey("pedido-1042-faturado"))
```

A mesma chave com outra requisição volta `IDEMPOTENCY_KEY_REUSED`. No `BatchUpsert`, o 1º lote usa
a sua chave como veio e os seguintes `"<chave>:2"`, `"<chave>:3"`… (sem chave, cada lote gera a sua).

## Identidade do widget

Para o widget de atendimento reconhecer o usuário logado, o **seu backend** assina a identidade
dele com o segredo do widget (que nunca vai para o navegador). É local — sem rede e sem chave de API:

```go
assinatura, err := bfocus.SignWidgetIdentity(
	os.Getenv("BFOCUS_WIDGET_SECRET"),
	"USR-1",    // o usuário no seu sistema
	"ERP 1042", // a empresa (cliente) dele
)
// HMAC-SHA256 em hex minúsculo de "v1:USR-1:ERP 1042" — entregue junto dos dois ids à página
// que abre o widget. Argumento vazio é erro (ErrInvalidArgument).
```

## Versões

**Fixe a versão exata** (`go get github.com/bernisoftware/bfocus-go@v0.1.0`, que grava
`require github.com/bernisoftware/bfocus-go v0.1.0` no seu `go.mod`) e suba de uma versão para a
outra de propósito — evite `@latest` no CI. Cada release declara se muda a superfície pública
(`additive` ou `breaking: …`), então dá para saber o que revisar antes de subir.

A SDK se identifica em toda requisição (`X-Bfocus-Client: bfocus-go/<versão>`, a constante
`bfocus.Version`): quando uma correção exigir atualizar, o bFocus avisa as contas que rodam a versão
afetada.

## Exemplo

Um programa rodável está em [`examples/quickstart`](examples/quickstart/main.go):

```bash
BFOCUS_API_KEY=bf_live_... go run github.com/bernisoftware/bfocus-go/examples/quickstart@v0.1.0
```

## Licença

MIT © Berni Software
