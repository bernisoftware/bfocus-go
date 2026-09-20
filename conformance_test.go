package bfocus

// Conformidade (BRIEF §7): roda TODOS os casos de testdata/conformance/cases.json contra um
// servidor httptest que confere cada troca — método, caminho EXATAMENTE como codificado,
// query, corpo JSON (igualdade profunda), Authorization, X-Bfocus-Client, X-Request-Id,
// Idempotency-Key — e, entre novas tentativas da mesma chamada, que os ids se repetem.
// Depois compara o retorno (ou o erro) com `expect`.
//
// A cópia testdata/conformance/cases.json é escrita pelo clients/conformance/generate.py do
// monorepo (não edite à mão); o espelho público só recebe clients/go, então a suíte lê a
// cópia e um teste trava "cópia == original" quando o original existe.
//
// Endpoint novo sem método na SDK quebra esta suíte: todo `op` dos casos precisa estar em
// conformanceOps (a menos que esteja em sdk_excluded_ops).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	vendoredCases = filepath.Join("testdata", "conformance", "cases.json")
	// No monorepo: <raiz>/clients/go. No espelho público (ou num container que só monta
	// clients/go) estes caminhos não existem e os testes que dependem deles são pulados.
	monorepoCases     = filepath.Join("..", "conformance", "cases.json")
	monorepoGenerator = filepath.Join("..", "conformance", "generate.py")
	monorepoSpec      = filepath.Join("..", "..", "api", "openapi", "public.json")
)

// casesFormat é a versão do formato do cases.json que esta suíte entende.
const casesFormat = 1

type conformanceFile struct {
	Version    int               `json:"version"`
	APIKey     string            `json:"api_key"`
	Excluded   []string          `json:"sdk_excluded_ops"`
	Helpers    map[string]string `json:"sdk_helper_ops"`
	Cases      []conformanceCase `json:"cases"`
	Signatures []struct {
		Secret             string `json:"secret"`
		UserExternalID     string `json:"user_external_id"`
		CustomerExternalID string `json:"customer_external_id"`
		Expected           string `json:"expected"`
	} `json:"signatures"`
	SignaturesV2 []struct {
		Secret             string `json:"secret"`
		UserExternalID     string `json:"user_external_id"`
		CustomerExternalID string `json:"customer_external_id"`
		Timestamp          int64  `json:"timestamp"`
		Expected           string `json:"expected"`
	} `json:"signatures_v2"`
}

type conformanceCase struct {
	ID        string                     `json:"id"`
	Op        string                     `json:"op"`
	Args      map[string]json.RawMessage `json:"args"`
	Exchanges []struct {
		Retry   *bool `json:"retry"`
		Request struct {
			Method string            `json:"method"`
			Path   string            `json:"path"`
			Query  map[string]string `json:"query"`
			Body   json.RawMessage   `json:"body"`
		} `json:"request"`
		Response fakeResponse `json:"response"`
	} `json:"exchanges"`
	Expect map[string]json.RawMessage `json:"expect"`
}

type expectedError struct {
	Type          string             `json:"type"`
	Code          string             `json:"code"`
	Status        int                `json:"status"`
	RequestID     *string            `json:"request_id"`
	RetryAfter    *float64           `json:"retry_after"`
	RequiredScope *string            `json:"required_scope"`
	Validation    *map[string]string `json:"validation"`
}

func loadConformance(t *testing.T) *conformanceFile {
	t.Helper()
	data, err := os.ReadFile(vendoredCases)
	if err != nil {
		t.Fatalf("não li %s: %v", vendoredCases, err)
	}
	var f conformanceFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("%s inválido: %v", vendoredCases, err)
	}
	if f.Version != casesFormat {
		t.Fatalf("cases.json no formato %d; esta suíte entende o %d — atualize conformance_test.go", f.Version, casesFormat)
	}
	return &f
}

// ── tabela op → chamada da SDK ───────────────────────────────────────────────

// caseArgs são os `args` neutros (snake_case) de um caso.
type caseArgs struct {
	t   *testing.T
	raw map[string]json.RawMessage
}

// str lê um argumento posicional obrigatório.
func (a caseArgs) str(name string) string {
	a.t.Helper()
	raw, ok := a.raw[name]
	if !ok {
		a.t.Fatalf("args sem %q", name)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		a.t.Fatalf("args[%q] não é string: %s", name, raw)
	}
	return s
}

// only exige que os args sejam exatamente estes (todos posicionais).
func (a caseArgs) only(names ...string) {
	a.t.Helper()
	for key := range a.raw {
		if !containsString(names, key) {
			a.t.Fatalf("arg %q sem parâmetro correspondente na SDK", key)
		}
	}
	for _, name := range names {
		a.str(name)
	}
}

// items lê o único arg `items` (lista de objetos) dos lotes.
func (a caseArgs) items() []map[string]json.RawMessage {
	a.t.Helper()
	for key := range a.raw {
		if key != "items" {
			a.t.Fatalf("arg %q sem parâmetro correspondente na SDK", key)
		}
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(a.raw["items"], &items); err != nil {
		a.t.Fatalf("args.items: %v", err)
	}
	return items
}

// params converte os args (menos os posicionais) no struct de parâmetros T. Chave com
// valor null vira ClearFields (é assim que se envia null na SDK Go); chave sem campo em T
// falha — arg novo no cases.json sem parâmetro na SDK quebra o teste.
func params[T any](a caseArgs, positional ...string) *T {
	a.t.Helper()
	return decodeParams[T](a.t, a.raw, positional...)
}

func decodeParams[T any](t *testing.T, args map[string]json.RawMessage, positional ...string) *T {
	t.Helper()
	rest := map[string]json.RawMessage{}
	var clear []string
	for key, value := range args {
		switch {
		case containsString(positional, key):
		case isJSONNull(value):
			clear = append(clear, key)
		default:
			rest[key] = value
		}
	}
	p := new(T)
	dec := json.NewDecoder(bytes.NewReader(mustJSON(rest)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		t.Fatalf("args → %T: %v", p, err)
	}
	if len(clear) > 0 {
		sort.Strings(clear)
		field := reflect.ValueOf(p).Elem().FieldByName("ClearFields")
		if !field.IsValid() {
			t.Fatalf("%T não tem ClearFields para enviar null em %v", p, clear)
		}
		field.Set(reflect.ValueOf(clear))
	}
	return p
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func ret[T any](v T, err error) (any, error) { return v, err }

func collect[T any](ctx context.Context, it *Iter[T]) (any, error) {
	out := []T{}
	for it.Next(ctx) {
		out = append(out, it.Current())
	}
	return out, it.Err()
}

type opFunc func(ctx context.Context, c *Client, a caseArgs) (any, error)

var conformanceOps = map[string]opFunc{
	"customers.upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Customers.Upsert(ctx, a.str("external_id"), params[CustomerUpsertParams](a, "external_id")))
	},
	"customers.get": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.Customers.Get(ctx, a.str("external_id")))
	},
	"customers.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Customers.List(ctx, params[CustomerListParams](a)))
	},
	"customers.list_all": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return collect(ctx, c.Customers.ListAll(params[CustomerListParams](a)))
	},
	"customers.delete": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.Customers.Delete(ctx, a.str("external_id")))
	},
	"customers.contacts.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.Customers.Contacts.List(ctx, a.str("external_id")))
	},
	"customers.contacts.upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Customers.Contacts.Upsert(ctx, a.str("external_id"), a.str("contact_external_id"),
			params[ContactUpsertParams](a, "external_id", "contact_external_id")))
	},
	"customers.contacts.delete": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id", "contact_external_id")
		return ret(c.Customers.Contacts.Delete(ctx, a.str("external_id"), a.str("contact_external_id")))
	},
	"customers.products.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.Customers.Products.List(ctx, a.str("external_id")))
	},
	"customers.products.attach": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id", "product_slug")
		return ret(c.Customers.Products.Attach(ctx, a.str("external_id"), a.str("product_slug")))
	},
	"customers.products.detach": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id", "product_slug")
		return ret(c.Customers.Products.Detach(ctx, a.str("external_id"), a.str("product_slug")))
	},
	"customers.interactions.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Customers.Interactions.List(ctx, a.str("external_id"), params[InteractionListParams](a, "external_id")))
	},
	"customers.interactions.list_all": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return collect(ctx, c.Customers.Interactions.ListAll(a.str("external_id"), params[InteractionListParams](a, "external_id")))
	},
	"customers.interactions.create": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Customers.Interactions.Create(ctx, a.str("external_id"), a.str("content"),
			params[InteractionCreateParams](a, "external_id", "content")))
	},
	"customers.batch": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		raw := a.items()
		items := make([]CustomerBatchItem, len(raw))
		for i, item := range raw {
			items[i] = *decodeParams[CustomerBatchItem](a.t, item)
		}
		return ret(c.Customers.Batch(ctx, items))
	},
	"customers.identifiers.add": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Customers.Identifiers.Add(ctx, a.str("external_id"), a.str("extra_id"),
			params[IdentifierParams](a, "external_id", "extra_id")))
	},
	"customers.identifiers.remove": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id", "extra_id")
		return ret(c.Customers.Identifiers.Remove(ctx, a.str("external_id"), a.str("extra_id")))
	},
	"people.upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.People.Upsert(ctx, a.str("customer_external_id"), a.str("person_external_id"),
			params[PersonParams](a, "customer_external_id", "person_external_id")))
	},
	"people.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("customer_external_id")
		return ret(c.People.List(ctx, a.str("customer_external_id")))
	},
	"people.delete": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("customer_external_id", "person_external_id")
		return ret(c.People.Delete(ctx, a.str("customer_external_id"), a.str("person_external_id")))
	},
	"people.batch": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		raw := a.items()
		items := make([]PersonBatchItem, len(raw))
		for i, item := range raw {
			items[i] = *decodeParams[PersonBatchItem](a.t, item)
		}
		return ret(c.People.Batch(ctx, items))
	},
	"people.identifiers.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("person_external_id")
		return ret(c.People.Identifiers.List(ctx, a.str("person_external_id")))
	},
	"people.identifiers.add": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.People.Identifiers.Add(ctx, a.str("person_external_id"), a.str("extra_id"),
			params[IdentifierParams](a, "person_external_id", "extra_id")))
	},
	"people.identifiers.remove": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("person_external_id", "extra_id")
		return ret(c.People.Identifiers.Remove(ctx, a.str("person_external_id"), a.str("extra_id")))
	},
	"products.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Products.List(ctx, params[ProductListParams](a)))
	},
	"products.get": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("slug")
		return ret(c.Products.Get(ctx, a.str("slug")))
	},
	"products.upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.Products.Upsert(ctx, a.str("slug"), params[ProductUpsertParams](a, "slug")))
	},
	"products.archive": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("slug")
		return ret(c.Products.Archive(ctx, a.str("slug")))
	},
	"release_notes.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.ReleaseNotes.List(ctx, a.str("product_slug"), params[ReleaseNoteListParams](a, "product_slug")))
	},
	"release_notes.list_all": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return collect(ctx, c.ReleaseNotes.ListAll(a.str("product_slug"), params[ReleaseNoteListParams](a, "product_slug")))
	},
	"release_notes.get": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("product_slug", "version")
		return ret(c.ReleaseNotes.Get(ctx, a.str("product_slug"), a.str("version")))
	},
	"release_notes.upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.ReleaseNotes.Upsert(ctx, a.str("product_slug"), a.str("version"),
			params[ReleaseNoteUpsertParams](a, "product_slug", "version")))
	},
	"release_notes.publish": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("product_slug", "version")
		return ret(c.ReleaseNotes.Publish(ctx, a.str("product_slug"), a.str("version")))
	},
	"kb.articles.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.KB.Articles.List(ctx, params[KBArticleListParams](a)))
	},
	"kb.articles.list_all": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return collect(ctx, c.KB.Articles.ListAll(params[KBArticleListParams](a)))
	},
	"kb.articles.get": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.KB.Articles.Get(ctx, a.str("external_id")))
	},
	"kb.articles.upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.KB.Articles.Upsert(ctx, a.str("external_id"), params[KBArticleUpsertParams](a, "external_id")))
	},
	"kb.articles.batch_upsert": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		for key := range a.raw {
			if key != "articles" {
				a.t.Fatalf("arg %q sem parâmetro correspondente na SDK", key)
			}
		}
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(a.raw["articles"], &items); err != nil {
			a.t.Fatalf("args.articles: %v", err)
		}
		articles := make([]KBBatchArticle, len(items))
		for i, item := range items {
			articles[i] = *decodeParams[KBBatchArticle](a.t, item)
		}
		return ret(c.KB.Articles.BatchUpsert(ctx, articles))
	},
	"kb.articles.publish": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.KB.Articles.Publish(ctx, a.str("external_id")))
	},
	"kb.articles.unpublish": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.KB.Articles.Unpublish(ctx, a.str("external_id")))
	},
	"kb.articles.delete": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("external_id")
		return ret(c.KB.Articles.Delete(ctx, a.str("external_id")))
	},
	"kb.search": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.KB.Search(ctx, a.str("q"), params[KBSearchParams](a, "q")))
	},
	"ai_agents.list": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only()
		return ret(c.AIAgents.List(ctx))
	},
	"ai_agents.get": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		a.only("agent_id")
		return ret(c.AIAgents.Get(ctx, a.str("agent_id")))
	},
	"ai_agents.preview": func(ctx context.Context, c *Client, a caseArgs) (any, error) {
		return ret(c.AIAgents.Preview(ctx, a.str("agent_id"), a.str("message"),
			params[AIAgentPreviewParams](a, "agent_id", "message")))
	},
}

// errorSentinels: `expect.error.type` → sentinela (errors.Is). "api" não tem sentinela: é
// um *Error que não casa com nenhum deles.
var errorSentinels = map[string]error{
	"authentication":    ErrAuthentication,
	"permission_denied": ErrPermissionDenied,
	"not_found":         ErrNotFound,
	"conflict":          ErrConflict,
	"validation":        ErrValidation,
	"rate_limit":        ErrRateLimit,
	"server":            ErrServer,
	"network":           ErrNetwork,
}

var (
	clientHeaderRE = regexp.MustCompile(`^bfocus-go/` + regexp.QuoteMeta(Version) + `$`)
	requestIDRE    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

const sentRequestID = "$sent" // em expect.error.request_id: "o X-Request-Id que a SDK enviou"

// ── suíte ────────────────────────────────────────────────────────────────────

func TestConformance(t *testing.T) {
	file := loadConformance(t)
	if len(file.Cases) == 0 {
		t.Fatal("cases.json sem casos")
	}
	srv := newFakeServer(t)
	seen := map[string]bool{}
	for _, tc := range file.Cases {
		tc := tc
		if seen[tc.ID] {
			t.Fatalf("id de caso duplicado: %s", tc.ID)
		}
		seen[tc.ID] = true
		t.Run(tc.ID, func(t *testing.T) { runConformanceCase(t, file, srv, tc) })
	}
}

func runConformanceCase(t *testing.T, file *conformanceFile, srv *fakeServer, tc conformanceCase) {
	op, ok := conformanceOps[tc.Op]
	if !ok {
		if containsString(file.Excluded, tc.Op) {
			t.Skipf("op %s está em sdk_excluded_ops", tc.Op)
		}
		t.Fatalf("op %q sem método na SDK Go (e fora de sdk_excluded_ops) — implemente e mapeie em conformanceOps", tc.Op)
	}

	responses := make([]fakeResponse, len(tc.Exchanges))
	for i, ex := range tc.Exchanges {
		responses[i] = ex.Response
	}
	srv.reset(responses...)
	client, err := NewClient(file.APIKey, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sleeps := &sleepLog{}
	client.sleep = sleeps.sleep // esperas desligadas: a suíte não dorme

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, callErr := op(ctx, client, caseArgs{t: t, raw: tc.Args})

	requests := srv.recorded()
	checkExchanges(t, file.APIKey, tc, requests)
	checkWaits(t, tc, sleeps.all())

	if rawErr, isError := tc.Expect["error"]; isError {
		var want expectedError
		if err := json.Unmarshal(rawErr, &want); err != nil {
			t.Fatalf("expect.error inválido: %v", err)
		}
		if callErr == nil {
			t.Fatalf("esperava erro %s/%s, veio sucesso: %s", want.Type, want.Code, mustJSON(result))
		}
		checkError(t, want, callErr, requests)
		return
	}
	rawResult, hasResult := tc.Expect["result"]
	if !hasResult {
		t.Fatalf("caso sem expect.result nem expect.error")
	}
	if callErr != nil {
		t.Fatalf("erro inesperado: %v", callErr)
	}
	var got, want any
	if err := json.Unmarshal(mustJSON(result), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawResult, &want); err != nil {
		t.Fatal(err)
	}
	if diff := jsonDiff("resultado", got, want, true); diff != "" {
		t.Errorf("%s\n  retorno:  %s\n  esperado: %s", diff, mustJSON(got), rawResult)
	}
}

func checkExchanges(t *testing.T, apiKey string, tc conformanceCase, requests []recorded) {
	t.Helper()
	if len(requests) != len(tc.Exchanges) {
		var got []string
		for _, r := range requests {
			got = append(got, r.Method+" "+r.Path)
		}
		t.Fatalf("nº de requisições: %d, esperado %d (%s)", len(requests), len(tc.Exchanges), strings.Join(got, ", "))
	}
	var previous *recorded
	for i, ex := range tc.Exchanges {
		got := requests[i]
		want := ex.Request
		where := fmt.Sprintf("troca %d", i)

		if got.Method != want.Method {
			t.Errorf("%s: método %s, esperado %s", where, got.Method, want.Method)
		}
		if got.Path != want.Path {
			t.Errorf("%s: caminho %q, esperado %q (exatamente como codificado)", where, got.Path, want.Path)
		}
		checkQuery(t, where, got, want.Query)

		if len(want.Body) == 0 || isJSONNull(want.Body) {
			if len(got.Body) != 0 {
				t.Errorf("%s: não devia ter corpo, veio %q", where, got.Body)
			}
			if got.has("Content-Type") {
				t.Errorf("%s: Content-Type sem corpo", where)
			}
		} else {
			if ct := got.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("%s: Content-Type %q", where, ct)
			}
			var wantBody any
			if err := json.Unmarshal(want.Body, &wantBody); err != nil {
				t.Fatal(err)
			}
			if diff := jsonDiff("corpo", got.jsonBody(t), wantBody, false); diff != "" {
				t.Errorf("%s: %s\n  enviado:  %s\n  esperado: %s", where, diff, got.Body, want.Body)
			}
		}

		if v := got.Header.Get("Authorization"); v != "Bearer "+apiKey {
			t.Errorf("%s: Authorization %q", where, v)
		}
		if v := got.Header.Get("Accept"); v != "application/json" {
			t.Errorf("%s: Accept %q", where, v)
		}
		clientID := got.Header.Get("X-Bfocus-Client")
		if !clientHeaderRE.MatchString(clientID) {
			t.Errorf("%s: X-Bfocus-Client %q não casa com %s", where, clientID, clientHeaderRE)
		}
		if ua := got.Header.Get("User-Agent"); ua != clientID {
			t.Errorf("%s: User-Agent %q, esperado o X-Bfocus-Client %q", where, ua, clientID)
		}
		if id := got.Header.Get("X-Request-Id"); !requestIDRE.MatchString(id) {
			t.Errorf("%s: X-Request-Id %q (esperado uuid4 hex, 32 caracteres)", where, id)
		}
		if isWrite(want.Method) {
			if got.Header.Get("Idempotency-Key") == "" {
				t.Errorf("%s: escrita sem Idempotency-Key", where)
			}
		} else if got.has("Idempotency-Key") {
			t.Errorf("%s: Idempotency-Key em %s", where, want.Method)
		}

		if ex.Retry == nil {
			t.Fatalf("%s: troca sem o campo 'retry'", where)
		}
		switch {
		case i == 0:
			if *ex.Retry {
				t.Fatalf("%s: a 1ª troca não pode ser nova tentativa", where)
			}
		case *ex.Retry:
			// nova tentativa da MESMA chamada lógica: ids repetidos
			if a, b := got.Header.Get("X-Request-Id"), previous.Header.Get("X-Request-Id"); a != b {
				t.Errorf("%s: nova tentativa com X-Request-Id %q ≠ %q", where, a, b)
			}
			if a, b := got.Header.Get("Idempotency-Key"), previous.Header.Get("Idempotency-Key"); a != b {
				t.Errorf("%s: nova tentativa com Idempotency-Key %q ≠ %q", where, a, b)
			}
		default:
			// chamada lógica nova (ex.: próxima página do ListAll): ids novos
			if got.Header.Get("X-Request-Id") == previous.Header.Get("X-Request-Id") {
				t.Errorf("%s: chamada nova precisa de X-Request-Id novo", where)
			}
			if k := got.Header.Get("Idempotency-Key"); k != "" && k == previous.Header.Get("Idempotency-Key") {
				t.Errorf("%s: chamada nova precisa de Idempotency-Key nova", where)
			}
		}
		previous = &requests[i]
	}
}

func checkQuery(t *testing.T, where string, got recorded, want map[string]string) {
	t.Helper()
	for key, values := range got.Query {
		wantValue, expected := want[key]
		switch {
		case !expected:
			t.Errorf("%s: query com %s=%v, que não era esperado (query crua %q)", where, key, values, got.RawQuery)
		case len(values) != 1 || values[0] != wantValue:
			t.Errorf("%s: query %s=%v, esperado %q", where, key, values, wantValue)
		}
	}
	for key := range want {
		if _, sent := got.Query[key]; !sent {
			t.Errorf("%s: query sem %s (query crua %q)", where, key, got.RawQuery)
		}
	}
}

// checkWaits: uma espera por nova tentativa — o Retry-After da resposta anterior quando
// houver (teto 60 s), senão min(8, 0.5 × 2^n) s + até 25%.
func checkWaits(t *testing.T, tc conformanceCase, waits []time.Duration) {
	t.Helper()
	var retries []int
	for i, ex := range tc.Exchanges {
		if ex.Retry != nil && *ex.Retry {
			retries = append(retries, i)
		}
	}
	if len(waits) != len(retries) {
		t.Fatalf("esperas: %v, esperado %d (uma por nova tentativa)", waits, len(retries))
	}
	for n, i := range retries {
		prev := tc.Exchanges[i-1].Response
		if ra := headerValue(prev.Headers, "Retry-After"); ra != "" {
			secs, err := strconv.ParseFloat(ra, 64)
			if err != nil {
				t.Fatalf("Retry-After %q do caso não é número", ra)
			}
			want := time.Duration(math.Min(secs, 60) * float64(time.Second))
			if waits[n] != want {
				t.Errorf("espera %d: %s, esperado o Retry-After %s", n, waits[n], want)
			}
			continue
		}
		base := time.Duration(math.Min(8, 0.5*math.Pow(2, float64(n))) * float64(time.Second))
		if waits[n] < base || waits[n] > base+base/4 {
			t.Errorf("espera %d: %s, esperado entre %s e %s", n, waits[n], base, base+base/4)
		}
	}
}

func headerValue(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func checkError(t *testing.T, want expectedError, err error, requests []recorded) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("esperava *bfocus.Error (%s), veio %T: %v", want.Type, err, err)
	}
	if string(e.Type) != want.Type {
		t.Errorf("Type %q, esperado %q", e.Type, want.Type)
	}
	if _, known := errorSentinels[want.Type]; !known && want.Type != "api" {
		t.Fatalf("tipo de erro desconhecido no caso: %q", want.Type)
	}
	for typ, sentinel := range errorSentinels {
		if is := errors.Is(err, sentinel); is != (typ == want.Type) {
			t.Errorf("errors.Is(err, sentinela de %q) = %v (tipo esperado %q)", typ, is, want.Type)
		}
	}
	if e.Code != want.Code {
		t.Errorf("Code %q, esperado %q", e.Code, want.Code)
	}
	if e.Status != want.Status {
		t.Errorf("Status %d, esperado %d", e.Status, want.Status)
	}
	if want.RequestID != nil {
		expected := *want.RequestID
		if expected == sentRequestID {
			if len(requests) == 0 {
				t.Fatal("$sent sem requisição enviada")
			}
			expected = requests[len(requests)-1].Header.Get("X-Request-Id")
			if expected == "" {
				t.Fatal("$sent: a SDK não enviou X-Request-Id")
			}
		}
		if e.RequestID != expected {
			t.Errorf("RequestID %q, esperado %q", e.RequestID, expected)
		}
	}
	if want.RetryAfter != nil {
		if expected := time.Duration(*want.RetryAfter * float64(time.Second)); e.RetryAfter != expected {
			t.Errorf("RetryAfter %s, esperado %s", e.RetryAfter, expected)
		}
	}
	if want.RequiredScope != nil && e.RequiredScope != *want.RequiredScope {
		t.Errorf("RequiredScope %q, esperado %q", e.RequiredScope, *want.RequiredScope)
	}
	if want.Validation != nil {
		if len(e.Validation) != len(*want.Validation) || (len(e.Validation) > 0 && !reflect.DeepEqual(e.Validation, *want.Validation)) {
			t.Errorf("Validation %v, esperado %v", e.Validation, *want.Validation)
		}
	}
	if !strings.Contains(e.Message, want.Code) || !strings.Contains(err.Error(), want.Code) {
		t.Errorf("a mensagem precisa trazer o code %q: %q", want.Code, err.Error())
	}
}

// jsonDiff compara dois valores JSON decodificados (map[string]any, []any, string,
// float64, bool, nil) e descreve a primeira diferença ("" = iguais). Com instants, uma
// string de data-hora RFC 3339 é igual a outra que represente o mesmo instante: os modelos
// Go decodificam datas em time.Time, que volta como "…Z" em vez de "…+00:00".
func jsonDiff(path string, got, want any, instants bool) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: esperado objeto, veio %s", path, mustJSON(got))
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			gv, inGot := g[k]
			wv, inWant := w[k]
			switch {
			case !inGot:
				return fmt.Sprintf("%s.%s: ausente (esperado %s)", path, k, mustJSON(wv))
			case !inWant:
				return fmt.Sprintf("%s.%s: sobrando (%s)", path, k, mustJSON(gv))
			}
			if d := jsonDiff(path+"."+k, gv, wv, instants); d != "" {
				return d
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("%s: esperada lista, veio %s", path, mustJSON(got))
		}
		if len(g) != len(w) {
			return fmt.Sprintf("%s: %d itens, esperado %d", path, len(g), len(w))
		}
		for i := range w {
			if d := jsonDiff(fmt.Sprintf("%s[%d]", path, i), g[i], w[i], instants); d != "" {
				return d
			}
		}
		return ""
	case string:
		g, ok := got.(string)
		if ok && (g == w || (instants && sameInstant(g, w))) {
			return ""
		}
		return fmt.Sprintf("%s: %s, esperado %s", path, mustJSON(got), mustJSON(w))
	default: // float64, bool, nil — tipos distintos em Go, então true ≠ 1
		if reflect.DeepEqual(got, want) {
			return ""
		}
		return fmt.Sprintf("%s: %s, esperado %s", path, mustJSON(got), mustJSON(want))
	}
}

func sameInstant(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	return errA == nil && errB == nil && ta.Equal(tb)
}

// ── cobertura, cópia e assinaturas ───────────────────────────────────────────

func TestConformanceCoverage(t *testing.T) {
	file := loadConformance(t)
	inCases := map[string]bool{}
	for _, tc := range file.Cases {
		inCases[tc.Op] = true
		if _, mapped := conformanceOps[tc.Op]; !mapped && !containsString(file.Excluded, tc.Op) {
			t.Errorf("op %q dos casos sem método na SDK Go — implemente ou exclua (sdk_excluded_ops)", tc.Op)
		}
	}
	for _, op := range file.Excluded {
		if _, mapped := conformanceOps[op]; mapped {
			t.Errorf("op %q está em sdk_excluded_ops mas mapeada na SDK", op)
		}
	}
	for op := range conformanceOps {
		if !inCases[op] {
			t.Errorf("op %q mapeada na SDK sem nenhum caso de conformidade", op)
		}
	}
	for helper, base := range file.Helpers {
		for _, op := range []string{helper, base} {
			if _, mapped := conformanceOps[op]; !mapped {
				t.Errorf("sdk_helper_ops: %q sem método na SDK", op)
			}
		}
	}
}

func TestSpecOperationsHaveMethods(t *testing.T) {
	data, err := os.ReadFile(monorepoSpec)
	if err != nil {
		t.Skip("api/openapi/public.json só existe no monorepo")
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	file := loadConformance(t)
	for path, item := range spec.Paths {
		for method, raw := range item {
			var op struct {
				OperationID string `json:"operationId"`
			}
			if json.Unmarshal(raw, &op) != nil || op.OperationID == "" {
				continue
			}
			if _, mapped := conformanceOps[op.OperationID]; !mapped && !containsString(file.Excluded, op.OperationID) {
				t.Errorf("%s %s (%s) sem método na SDK Go", strings.ToUpper(method), path, op.OperationID)
			}
		}
	}
}

func TestConformanceCopyUpToDate(t *testing.T) {
	source, err := os.ReadFile(monorepoCases)
	if err != nil {
		t.Skip("fonte dos casos só existe no monorepo")
	}
	if _, err := os.Stat(monorepoGenerator); err != nil {
		t.Skip("clients/conformance/generate.py ausente — não é o monorepo")
	}
	vendored, err := os.ReadFile(vendoredCases)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vendored, source) {
		t.Fatal("testdata/conformance/cases.json desatualizado — rode `python3 clients/conformance/generate.py` (ele escreve a cópia; não edite à mão)")
	}
}

func TestSignatureVectors(t *testing.T) {
	file := loadConformance(t)
	if len(file.Signatures) == 0 {
		t.Fatal("cases.json sem vetores de assinatura")
	}
	for _, v := range file.Signatures {
		got, err := SignWidgetIdentity(v.Secret, v.UserExternalID, v.CustomerExternalID)
		if err != nil {
			t.Fatalf("%s: %v", v.UserExternalID, err)
		}
		if got != v.Expected {
			t.Errorf("SignWidgetIdentity(%q, %q, %q) = %s, esperado %s", v.Secret, v.UserExternalID, v.CustomerExternalID, got, v.Expected)
		}
	}
}

func TestSignatureV2Vectors(t *testing.T) {
	file := loadConformance(t)
	if len(file.SignaturesV2) == 0 {
		t.Fatal("cases.json sem vetores de assinatura v2")
	}
	for _, v := range file.SignaturesV2 {
		got, err := SignWidgetIdentityV2At(v.Secret, v.UserExternalID, v.CustomerExternalID, time.Unix(v.Timestamp, 0))
		if err != nil {
			t.Fatalf("%s: %v", v.UserExternalID, err)
		}
		if got != v.Expected {
			t.Errorf("SignWidgetIdentityV2At(%q, %q, %q, %d) = %s, esperado %s", v.Secret, v.UserExternalID, v.CustomerExternalID, v.Timestamp, got, v.Expected)
		}
	}
}
