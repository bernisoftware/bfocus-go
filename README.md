# bfocus-go

SDK oficial em **Go** da API pública do [bFocus](https://bfocus.com.br): clientes, pessoas
(usuários dos seus clientes), lotes, identificadores extras, produtos, release notes, base de
conhecimento e agentes de IA.

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
| `customers:read` | Ler clientes, pessoas, contatos, produtos vinculados e interações |
| `customers:write` | Cadastrar, atualizar e excluir clientes, pessoas, contatos e interações; lotes; identificadores extras |
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

## Pessoas

Pessoas são quem abre o widget/portal em nome de um cliente — normalmente os **usuários do seu
sistema**. O id da pessoa é o mesmo `user.externalId` que o seu backend assina para o widget (veja
[Identidade do widget](#identidade-do-widget)), por isso **não pode ter `:`**.

```go
p, err := client.People.Upsert(ctx, "erp-1042", "app-77", &bfocus.PersonParams{
	Name:        bfocus.String("Paula Reis"),
	Email:       bfocus.String("paula@padaria.example"),
	Role:        bfocus.String("Financeiro"),
	IsPrimary:   bfocus.Bool(true),
	ExtraEmails: []string{"paula.reis@pessoal.example"}, // somam aos que já existem
})
fmt.Println(p.Status) // "created", "updated" ou "unchanged"

pessoas, err := client.People.List(ctx, "erp-1042") // []bfocus.Person

client.People.Delete(ctx, "erp-1042", "app-77") // retira o acesso (Access=false)
client.People.Upsert(ctx, "erp-1042", "app-77", &bfocus.PersonParams{Access: bfocus.Bool(true)}) // devolve
```

- **Nunca duplica.** O e-mail (ou o telefone) acha a pessoa que já chegou por e-mail ou por outro
  sistema, e ela é **adotada** pelo seu id. A mesma pessoa enviada com **outro cliente** NÃO muda
  de casa: ela é **ligada** também a esse cliente e `Linked` volta `true` — o cadastro é único e a
  mesma pessoa circula por vários clientes e vários produtos.
- **O acesso é do VÍNCULO.** `Delete` (e `Access: Bool(false)`) tira o acesso dela NESTE cliente e
  não nos outros: `PersonRevokeResult.Unlinked = true` quer dizer que ela segue ativa em algum outro.
- **`Delete` retira o acesso**, não apaga: a pessoa continua no histórico dos chamados e volta com
  `Access: false`. Um `Upsert` com `Access: bfocus.Bool(true)` devolve o acesso.
- `PersonParams` segue a regra de [só o que você passa muda](#como-os-métodos-funcionam) (`nil` é
  omitido; `ClearFields` envia `null`, que para pessoas significa "não altera").
- E-mail de alguém da sua equipe: `ErrConflict` com `Code` `PERSON_EMAIL_STAFF`; pessoa inexistente
  no `Delete`: `ErrNotFound` com `PERSON_NOT_FOUND`. Contato já usado: `PERSON_EMAIL_TAKEN`,
  `PERSON_PHONE_TAKEN` ou `PERSON_CONTACT_OTHER_CUSTOMER` — veja logo abaixo. Ao apagar contato:
  `PERSON_CLEAR_FIELD_INVALID` (422) e `PERSON_CLEAR_NOT_OWN_RECORD` (409).

### Campos personalizados da pessoa

`PersonParams.CustomFields` leva o que só existe no seu sistema (matrícula, centro de custo, filial). É a
**exceção** ao "só o que vier muda": a lista enviada **substitui a lista inteira** — campo que
ficar de fora é **removido**. Mande sempre a lista que o seu sistema tem hoje; deixá-lo `nil` não mexe
em nada, como em qualquer outro campo.

A `visibility` é decidida no bFocus e **preservada entre sincronizações** — por isso ela não vai
no envio, só volta na resposta: o seu ERP não rebaixa nem promove a exposição de um dado sem
querer.

Vale no upsert de pessoa, no lote de pessoas e na listagem de pessoas do cliente.

```go
p, err := client.People.Upsert(ctx, "erp-1042", "app-77", &bfocus.PersonParams{
	CustomFields: []bfocus.CustomFieldInput{ // a lista INTEIRA do seu sistema
		{Key: "matricula", Label: "Matrícula", Value: "4471"},
		{Key: "filial", Label: "Filial", Value: "Centro"},
	},
})
for _, campo := range p.CustomFields {
	fmt.Println(campo.Key, campo.Value, campo.Visibility) // Visibility vem do bFocus
}
```

### Apagar o e-mail ou o telefone da pessoa

Um contato gravado errado ficava preso para sempre: enquanto a ficha errada segurasse o telefone,
nenhum reenvio o soltava. `PersonParams.Clear` apaga.

```go
client.People.Upsert(ctx, "erp-1042", "app-77", &bfocus.PersonParams{
	Clear: []string{"phone"}, // ou []string{"email", "phone"}
})
```

Três regras que parecem contraintuitivas e são de propósito:

- **Apagar é explícito, e `Clear` é o único jeito.** `ClearFields: []string{"Phone"}` manda
  `phone: null`, e em pessoa `null` (como `nil` e a lista vazia) quer dizer **"não mexe"** — a SDK
  não traduz `null` em `Clear`. Fazer o `null` apagar teria apagado, em silêncio e na primeira carga
  seguinte, o dado de todo sistema que manda `null` para "não tenho esse valor".
- **Campo fora da lista é recusado, não ignorado**: hoje só `"email"` e `"phone"`; qualquer outro
  volta `ErrValidation` com `Code` `PERSON_CLEAR_FIELD_INVALID`.
- **Só se limpa a própria ficha.** Se você alcançou a pessoa por um identificador **extra**, volta
  `ErrConflict` com `Code` `PERSON_CLEAR_NOT_OWN_RECORD`: apagar o contato de uma ficha alcançada por
  apelido seria apagar dado de outro sistema. Para saber se o id que você tem em mãos é o principal
  ou um extra, use `People.Identifiers.List`.

Vale no `People.Upsert` e no `People.Batch` (`Clear` no item).

### Contato já usado: um 409 que você consegue resolver

`PERSON_EMAIL_TAKEN` e `PERSON_PHONE_TAKEN` (409) não são "tente de novo": o e-mail (ou o
telefone) já é de outra pessoa da conta. O erro diz **de quem**, em `Data` (a API repete o mesmo
detalhe em `Validation`, por compatibilidade):

| campo | o que é |
| --- | --- |
| `field` | `email` ou `phone` — qual contato está tomado |
| `owner_external_id` | o identificador da pessoa que já usa esse contato |
| `owner_name` | o nome dela |
| `owner_customer_external_id` | o cliente a que ela pertence |

**É o `owner_customer_external_id` que decide a ação**, e os dois casos pedem coisas opostas:

- **mesmo cliente que você enviou** → é quase sempre a MESMA pessoa em dois sistemas. Uma pessoa
  tem **N identificadores**: registre o seu como **extra** dela. A partir daí o seu id encontra
  essa pessoa.
- **outro cliente** → ninguém decide sozinho a quem a pessoa pertence. Não force: registre o caso
  e leve para quem conhece o cadastro. Unificar dois clientes é decisão de gente, não de um
  casamento por e-mail.

```go
_, err := client.People.Upsert(ctx, "erp-1042", "app-77", &bfocus.PersonParams{
	Name:  bfocus.String("Paula Reis"),
	Email: bfocus.String("paula@padaria.example"),
})
var e *bfocus.Error
if errors.As(err, &e) && (e.Code == "PERSON_EMAIL_TAKEN" || e.Code == "PERSON_PHONE_TAKEN") {
	dono, _ := e.Data["owner_external_id"].(string)
	if e.Data["owner_customer_external_id"] == "erp-1042" {
		// A mesma pessoa, com dois ids: o seu vira mais um identificador dela.
		_, err = client.People.Identifiers.Add(ctx, dono, "app-77", &bfocus.IdentifierParams{Label: bfocus.String("ERP")})
	} else {
		// Dono em OUTRO cliente: não decida sozinho — registre e leve para o cadastro.
		avisarCadastro(e.Code, e.Data)
	}
}
```

`PERSON_CONTACT_OTHER_CUSTOMER` (409) é o mesmo assunto pelo outro lado, e é **recusa
definitiva**: a API não move mais uma pessoa de um cliente para outro só porque o e-mail (ou o
telefone) casou. Repetir a chamada não resolve — trate como caso para o cadastro, nunca como
falha temporária.

## Lotes — clientes e pessoas

`Customers.Batch` e `People.Batch` gravam **até 500 itens por chamada** (`bfocus.MaxBatchSize`).
Cada item tem o mesmo formato do upsert, mais o id: `bfocus.CustomerBatchItem` (`ExternalID` + os
campos de `CustomerUpsertParams`) e `bfocus.PersonBatchItem` (`CustomerExternalID`, `ExternalID` +
os campos de `PersonParams`).

```go
res, err := client.Customers.Batch(ctx, []bfocus.CustomerBatchItem{
	{ExternalID: "erp-1042", Name: bfocus.String("Padaria Estrela"), Email: bfocus.String("contato@padaria.example")},
	{ExternalID: "erp-1043", Name: bfocus.String("Mercado Sol")},
})
if err != nil {
	return err // erro HTTP do lote inteiro (rede, 401, 429 esgotado…)
}
for _, r := range res.Results {
	switch {
	case r.Status == "error":
		log.Printf("item %d: %s (HTTP %d)", r.Index, *r.Error, *r.Code)
	case r.MergedInto != nil:
		// o id enviado é um identificador extra: este é o principal — atualize o seu lado
	}
}
fmt.Printf("%+v\n", res.Summary) // {Created:1 Updated:1 Unchanged:0 Error:0}

client.People.Batch(ctx, []bfocus.PersonBatchItem{
	{CustomerExternalID: "erp-1042", ExternalID: "app-77", Name: bfocus.String("Paula Reis"), Email: bfocus.String("paula@padaria.example")},
})
```

- **Resultado por item**: `Index` (a posição no lote que você enviou), `Status` (`created`,
  `updated`, `unchanged` ou `error`), `ExternalID`, `MergedInto`, `Error` (código estável, ex.:
  `NAME_REQUIRED`) e `Code` (o status HTTP que o item teria sozinho) + `Summary` com os contadores.
- **Um erro não desfaz os outros**: confira `Summary.Error` e registre os itens com erro.
- **Mais de 500 itens é erro de argumento** (`bfocus.ErrInvalidArgument`), antes de qualquer
  requisição — a SDK **não** divide sozinha, para o `Index` continuar sendo a posição no seu lote.
  Divida você:

  ```go
  for ini := 0; ini < len(clientes); ini += bfocus.MaxBatchSize {
  	fatia := clientes[ini:min(ini+bfocus.MaxBatchSize, len(clientes))]
  	res, err := client.Customers.Batch(ctx, fatia)
  	// … res.Results[i].Index é a posição dentro de fatia (clientes[ini+Index])
  }
  ```

- **Lista vazia** devolve o resultado zerado sem requisição. Um lote é **uma** chamada: aceita
  `bfocus.WithIdempotencyKey(...)` como qualquer escrita.

## Identificadores extras

O mesmo cliente (ou a mesma pessoa) pode ter ids em **vários sistemas seus** — o ERP e o CRM, por
exemplo. Ligue o id do outro sistema ao mesmo cadastro, em vez de criar outro:

```go
c, err := client.Customers.Identifiers.Add(ctx, "erp-1042", "crm-88", &bfocus.IdentifierParams{Label: bfocus.String("CRM")})
fmt.Println(c.Identifiers[0].ExternalID, c.Identifiers[0].Source) // crm-88 api
client.Customers.Identifiers.Remove(ctx, "erp-1042", "crm-88")

client.People.Identifiers.Add(ctx, "app-77", "crm-p5", nil) // sem rótulo: requisição sem corpo
client.People.Identifiers.Remove(ctx, "app-77", "crm-p5")
```

`Add` é idempotente. Se o id já pertence a **outro** cadastro, volta `ErrConflict` com `Code`
`IDENTIFIER_IN_USE`; `Remove` de um id que não está ligado volta `ErrNotFound` com
`IDENTIFIER_NOT_FOUND`. Depois de ligado, o id extra funciona nas outras chamadas e nos lotes — o
resultado traz o principal em `MergedInto`.

### Ler os identificadores da pessoa (para reconciliar)

`People.List` mostra só o identificador **principal** de cada pessoa. Quando dois cadastros seus
eram a mesma pessoa, um dos ids virou **extra** — e some da listagem sem ter sumido do cadastro. É
isso que faz a sua conferência fechar "633 de 636" sem explicar os 3.

`People.Identifiers.List` é a fonte de verdade dessa conferência, e é **leitura**: antes dela era
preciso ESCREVER (tentar um `Add`) para descobrir o que tinha acontecido. Aceita no caminho o id
principal **ou qualquer um dos extras**.

```go
ids, err := client.People.Identifiers.List(ctx, "crm-p5") // o id extra que "sumiu" da listagem
fmt.Println(ids.ExternalID)                               // app-77 — o principal do cadastro
for _, i := range ids.Identifiers {
	fmt.Println(i.ExternalID, i.Source)
}
```

## Sincronizar clientes e usuários do seu sistema

**Ids.** Use o id do seu sistema com um prefixo, **sem `:`** (a assinatura do widget recusa `:`):
`-` como separador — `erp-1042` para clientes, `app-77` para pessoas — ou UUIDs puros. Assim vários
sistemas seus convivem no mesmo bFocus sem colisão.

**Carga inicial (no deploy da integração):** clientes em fatias de 500 → vincule cada cliente ao
produto → pessoas em fatias de 500. Confira `Summary.Error` e registre os itens com erro.

```go
func cargaInicial(ctx context.Context, client *bfocus.Client, clientes []bfocus.CustomerBatchItem, pessoas []bfocus.PersonBatchItem) error {
	for ini := 0; ini < len(clientes); ini += bfocus.MaxBatchSize {
		fatia := clientes[ini:min(ini+bfocus.MaxBatchSize, len(clientes))]
		res, err := client.Customers.Batch(ctx, fatia)
		if err != nil {
			return err
		}
		registrarErros("cliente", ini, res)
	}
	for _, c := range clientes {
		if _, err := client.Customers.Products.Attach(ctx, c.ExternalID, "erp-cloud"); err != nil {
			return err
		}
	}
	for ini := 0; ini < len(pessoas); ini += bfocus.MaxBatchSize {
		res, err := client.People.Batch(ctx, pessoas[ini:min(ini+bfocus.MaxBatchSize, len(pessoas))])
		if err != nil {
			return err
		}
		registrarErros("pessoa", ini, res)
	}
	return nil
}

// registrarErros: Index é a posição dentro da fatia; ini+Index, na sua lista inteira.
func registrarErros(tipo string, ini int, res *bfocus.BatchResult) {
	for _, r := range res.Results {
		if r.Status == "error" {
			log.Printf("%s #%d: %s", tipo, ini+r.Index, *r.Error)
		}
	}
}
```

**Depois, no dia a dia**, espelhe cada evento do seu sistema:

| No seu sistema | No bFocus |
| --- | --- |
| criou/alterou cliente | `client.Customers.Upsert` (e `Customers.Products.Attach` para vincular ao produto) |
| criou/alterou usuário | `client.People.Upsert` |
| excluiu/desativou usuário | `client.People.Delete` (retira o acesso) |
| excluiu cliente | `client.Customers.Delete` |

Se um resultado trouxer `MergedInto`, atualize o id do seu lado.

**Nunca bloqueie a requisição do seu usuário esperando o bFocus.** Enfileire o evento (um job, uma
tabela de outbox) e processe fora da requisição, tentando de novo com backoff. A SDK já repete
429/5xx com a mesma `Idempotency-Key`; a fila cobre indisponibilidades longas:

```go
// No handler do seu sistema: só grava o evento na fila (mesma transação do seu dado).
fila.Enfileirar(ctx, Evento{ID: novoID(), Tipo: "usuario_salvo", UsuarioID: u.ID})

// No worker (com novas tentativas e backoff da sua fila):
func processar(ctx context.Context, client *bfocus.Client, ev Evento) error {
	idem := bfocus.WithIdempotencyKey("evento-" + ev.ID) // reprocessar o evento não duplica
	switch ev.Tipo {
	case "usuario_salvo":
		u := carregarUsuario(ev.UsuarioID)
		_, err := client.People.Upsert(ctx, fmt.Sprintf("erp-%d", u.EmpresaID), fmt.Sprintf("app-%d", u.ID), &bfocus.PersonParams{
			Name: bfocus.String(u.Nome), Email: bfocus.String(u.Email), Access: bfocus.Bool(u.Ativo),
		}, idem)
		return err
	case "usuario_excluido":
		_, err := client.People.Delete(ctx, fmt.Sprintf("erp-%d", ev.EmpresaID), fmt.Sprintf("app-%d", ev.UsuarioID), idem)
		return err
	}
	return nil
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
`RequestID`, `Validation`, `Data`, `RetryAfter` e `RequiredScope`. O `Data` é o `data` do corpo: o
detalhe estruturado que alguns erros trazem (`nil` quando não há) — é por ele que um 409 de contato
tomado diz de **quem** é o contato (veja [Pessoas](#pessoas)). O tipo sai do status, e cada um tem um
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
`external_id` de um artigo; nome desconhecido em `ClearFields`; `Page`/`PageSize` < 1; mais de 500
itens em `Customers.Batch`/`People.Batch`; `:` no usuário da assinatura v2) devolve, na
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

### Identidade do widget v2 (com validade)

A v2 carrega o instante da assinatura, então uma assinatura vazada deixa de valer sozinha:

```go
assinatura, err := bfocus.SignWidgetIdentityV2(
	os.Getenv("BFOCUS_WIDGET_SECRET"),
	"app-77",   // o usuário no seu sistema — SEM ":"
	"erp-1042", // a empresa (cliente) dele
)
// "v2.<ts>.<hex>": ts = segundos unix de agora; hex = HMAC-SHA256 em hex minúsculo de
// "v2:<ts>:app-77:erp-1042". Vai no userHash do widget, no mesmo lugar da v1.
```

- Vale de **7 dias atrás até 5 minutos à frente**: gere a cada renderização da página, **nunca
  guarde** a assinatura.
- O id do usuário **não pode ter `:`** (é o separador) — erro de argumento (`ErrInvalidArgument`),
  assim como argumento vazio.
- Para um instante fixo (testes), use `bfocus.SignWidgetIdentityV2At(segredo, usuario, cliente, at)`
  (`at` é um `time.Time`; antes de 1970, inclusive o `time.Time{}`, é erro de argumento).
- A v1 (`SignWidgetIdentity`) continua aceita.

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
