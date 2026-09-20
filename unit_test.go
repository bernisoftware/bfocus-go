package bfocus

// Testes unitários: lotes do BatchUpsert, ListAll, novas tentativas, rede e tempo esgotado,
// codificação de caminho/query/corpo, erros de argumento, formato dos erros, campos
// desconhecidos, cliente e assinatura do widget.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func customerJSON(n int) map[string]any {
	return map[string]any{"id": fmt.Sprintf("id-%d", n), "external_id": fmt.Sprintf("C%d", n), "name": fmt.Sprintf("Cliente %d", n)}
}

func mustNoRequests(t *testing.T, srv *fakeServer) {
	t.Helper()
	if got := srv.recorded(); len(got) != 0 {
		t.Fatalf("não devia ter ido à rede, foram %d requisições (1ª: %s %s)", len(got), got[0].Method, got[0].Path)
	}
}

func mustArgErr(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("esperava erro de argumento (ErrInvalidArgument), veio %T: %v", err, err)
	}
	var e *Error
	if errors.As(err, &e) {
		t.Fatalf("erro de argumento não pode ser *bfocus.Error: %v", err)
	}
}

// ── BatchUpsert ──────────────────────────────────────────────────────────────

func batchArticles(n int) []KBBatchArticle {
	out := make([]KBBatchArticle, n)
	for i := range out {
		out[i] = KBBatchArticle{ExternalID: fmt.Sprintf("git:a%d", i), Title: String(fmt.Sprintf("A%d", i))}
	}
	return out
}

func chunkResponse(articles []KBBatchArticle) fakeResponse {
	results := make([]map[string]any, len(articles))
	for i, a := range articles {
		results[i] = map[string]any{"external_id": a.ExternalID, "ok": true, "action": "created", "error": nil, "article": nil}
	}
	return ok(map[string]any{"results": results, "created": len(articles), "updated": 0, "unchanged": 0, "failed": 0})
}

func chunkResponses(articles []KBBatchArticle) []fakeResponse {
	var out []fakeResponse
	for i := 0; i < len(articles); i += KBBatchSize {
		end := i + KBBatchSize
		if end > len(articles) {
			end = len(articles)
		}
		out = append(out, chunkResponse(articles[i:end]))
	}
	return out
}

func TestBatchUpsertSplitsIn100AndAggregates(t *testing.T) {
	srv := newFakeServer(t)
	articles := batchArticles(250)
	c, _ := newTestClient(t, srv, chunkResponses(articles))

	out, err := c.KB.Articles.BatchUpsert(context.Background(), articles)
	if err != nil {
		t.Fatal(err)
	}
	reqs := srv.recorded()
	if len(reqs) != 3 {
		t.Fatalf("%d requisições, esperado 3", len(reqs))
	}
	var sizes []int
	var sent []string
	keys, ids := map[string]bool{}, map[string]bool{}
	for _, r := range reqs {
		if r.Method != "POST" || r.Path != "/api/v1/integration/kb/articles/batch" {
			t.Fatalf("requisição %s %s", r.Method, r.Path)
		}
		var body struct {
			Articles []map[string]any `json:"articles"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(body.Articles))
		for _, a := range body.Articles {
			sent = append(sent, a["external_id"].(string))
		}
		keys[r.Header.Get("Idempotency-Key")] = true
		ids[r.Header.Get("X-Request-Id")] = true
	}
	if !reflect.DeepEqual(sizes, []int{100, 100, 50}) {
		t.Errorf("lotes %v, esperado [100 100 50]", sizes)
	}
	var want []string
	for _, a := range articles {
		want = append(want, a.ExternalID)
	}
	if !reflect.DeepEqual(sent, want) {
		t.Error("os artigos não foram enviados na ordem")
	}
	if len(keys) != 3 || len(ids) != 3 {
		t.Errorf("cada lote é uma chamada lógica: %d Idempotency-Keys e %d X-Request-Ids distintos, esperado 3 e 3", len(keys), len(ids))
	}
	var got []string
	for _, r := range out.Results {
		got = append(got, r.ExternalID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("Results fora da ordem enviada")
	}
	if out.Created != 250 || out.Updated != 0 || out.Unchanged != 0 || out.Failed != 0 {
		t.Errorf("contadores %+v", out)
	}
}

func TestBatchUpsertExactly100IsOneRequest(t *testing.T) {
	srv := newFakeServer(t)
	articles := batchArticles(100)
	c, _ := newTestClient(t, srv, chunkResponses(articles))
	if _, err := c.KB.Articles.BatchUpsert(context.Background(), articles); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.recorded()); n != 1 {
		t.Fatalf("%d requisições, esperado 1", n)
	}
}

func TestBatchUpsertUserIdempotencyKeyPerChunk(t *testing.T) {
	srv := newFakeServer(t)
	for _, tc := range []struct {
		n    int
		key  string
		want []string
	}{
		{150, "sync-42", []string{"sync-42", "sync-42:2"}},
		{250, "k", []string{"k", "k:2", "k:3"}},
		{1, "sync-43", []string{"sync-43"}},
	} {
		articles := batchArticles(tc.n)
		c, _ := newTestClient(t, srv, chunkResponses(articles))
		if _, err := c.KB.Articles.BatchUpsert(context.Background(), articles, WithIdempotencyKey(tc.key)); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range srv.recorded() {
			got = append(got, r.Header.Get("Idempotency-Key"))
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%d artigos com a chave %q: %v, esperado %v", tc.n, tc.key, got, tc.want)
		}
	}
}

func TestBatchUpsertSumsMixedCounters(t *testing.T) {
	srv := newFakeServer(t)
	first := make([]map[string]any, 100)
	for i := range first {
		first[i] = map[string]any{"external_id": "x", "ok": true}
	}
	c, _ := newTestClient(t, srv, []fakeResponse{
		ok(map[string]any{"results": first, "created": 60, "updated": 30, "unchanged": 9, "failed": 1}),
		ok(map[string]any{"results": []any{map[string]any{"external_id": "y", "ok": false, "error": "KB_ARTICLE_TITLE_REQUIRED"}},
			"created": 0, "updated": 0, "unchanged": 0, "failed": 1}),
	})
	out, err := c.KB.Articles.BatchUpsert(context.Background(), batchArticles(101))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 101 {
		t.Errorf("%d resultados, esperado 101", len(out.Results))
	}
	if out.Created != 60 || out.Updated != 30 || out.Unchanged != 9 || out.Failed != 2 {
		t.Errorf("contadores %+v, esperado 60/30/9/2", out)
	}
	if last := out.Results[100]; last.OK || last.Error == nil || *last.Error != "KB_ARTICLE_TITLE_REQUIRED" {
		t.Errorf("resultado com falha: %+v", last)
	}
}

func TestBatchUpsertEmptyMakesNoRequest(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	for _, empty := range [][]KBBatchArticle{nil, {}} {
		out, err := c.KB.Articles.BatchUpsert(context.Background(), empty)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(mustJSON(out)); got != `{"results":[],"created":0,"updated":0,"unchanged":0,"failed":0}` {
			t.Errorf("resultado zerado: %s", got)
		}
	}
	mustNoRequests(t, srv)
}

func TestBatchUpsertValidatesEverythingBeforeSending(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	valid := batchArticles(150)
	for name, articles := range map[string][]KBBatchArticle{
		"sem ExternalID":          {{Title: String("sem id")}},
		"barra no ExternalID":     {{ExternalID: "docs/guia"}},
		"ClearFields inválido":    {{ExternalID: "git:x", ClearFields: []string{"nao_existe"}}},
		"ExternalID não limpa":    {{ExternalID: "git:x", ClearFields: []string{"external_id"}}},
		"inválido no 2º lote":     append(append([]KBBatchArticle(nil), valid...), KBBatchArticle{ExternalID: "a/b"}),
		"inválido depois de 1 ok": {{ExternalID: "git:ok"}, {}},
	} {
		_, err := c.KB.Articles.BatchUpsert(context.Background(), articles)
		if err == nil {
			t.Errorf("%s: esperava erro de argumento", name)
			continue
		}
		mustArgErr(t, err)
	}
	mustNoRequests(t, srv)
}

func TestBatchUpsertClearedProductGoesAsNull(t *testing.T) {
	srv := newFakeServer(t)
	article := KBBatchArticle{ExternalID: "git:global", Title: String("G"), ClearFields: []string{"product"}}
	c, _ := newTestClient(t, srv, chunkResponses([]KBBatchArticle{article}))
	if _, err := c.KB.Articles.BatchUpsert(context.Background(), []KBBatchArticle{article}); err != nil {
		t.Fatal(err)
	}
	if got := string(srv.recorded()[0].Body); got != `{"articles":[{"external_id":"git:global","title":"G","product":null}]}` {
		t.Errorf("corpo %s", got)
	}
}

func TestBatchUpsertHTTPErrorKeepsSavedChunks(t *testing.T) {
	srv := newFakeServer(t)
	articles := batchArticles(250)
	c, _ := newTestClient(t, srv, []fakeResponse{chunkResponse(articles[:100]), fail(409, "KB_BATCH_CONFLICT", nil)})
	out, err := c.KB.Articles.BatchUpsert(context.Background(), articles)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("esperava ErrConflict, veio %v", err)
	}
	if out == nil || len(out.Results) != 100 || out.Created != 100 {
		t.Errorf("o agregado dos lotes já gravados precisa voltar junto do erro: %+v", out)
	}
	if n := len(srv.recorded()); n != 2 {
		t.Errorf("%d requisições, esperado 2 (o erro interrompe os lotes seguintes)", n)
	}
}

// ── Customers.Batch / People.Batch (até MaxBatchSize, sem dividir) ───────────

func customerBatch(n int) []CustomerBatchItem {
	out := make([]CustomerBatchItem, n)
	for i := range out {
		out[i] = CustomerBatchItem{ExternalID: fmt.Sprintf("erp-%d", i), Name: String(fmt.Sprintf("Cliente %d", i))}
	}
	return out
}

func peopleBatch(n int) []PersonBatchItem {
	out := make([]PersonBatchItem, n)
	for i := range out {
		out[i] = PersonBatchItem{CustomerExternalID: "erp-1", ExternalID: fmt.Sprintf("app-%d", i), Name: String(fmt.Sprintf("Pessoa %d", i))}
	}
	return out
}

func batchOK(n int) fakeResponse {
	results := make([]map[string]any, n)
	for i := range results {
		results[i] = map[string]any{"index": i, "status": "created", "external_id": fmt.Sprintf("x-%d", i), "merged_into": nil, "error": nil, "code": nil}
	}
	return ok(map[string]any{"results": results, "summary": map[string]int{"created": n, "updated": 0, "unchanged": 0, "error": 0}})
}

func TestBatchOverMaxIsArgumentErrorWithoutRequest(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	ctx := context.Background()
	_, err := c.Customers.Batch(ctx, customerBatch(MaxBatchSize+1))
	mustArgErr(t, err)
	if want := "Customers.Batch aceita até 500 itens por chamada (recebeu 501); divida em lotes de 500"; !strings.Contains(err.Error(), want) {
		t.Errorf("mensagem %q sem %q", err, want)
	}
	_, err = c.People.Batch(ctx, peopleBatch(MaxBatchSize+1))
	mustArgErr(t, err)
	if !strings.Contains(err.Error(), "People.Batch aceita até 500") {
		t.Errorf("mensagem %q", err)
	}
	mustNoRequests(t, srv)
}

func TestBatchExactlyMaxIsOneRequest(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{batchOK(MaxBatchSize), batchOK(MaxBatchSize)})
	ctx := context.Background()
	out, err := c.Customers.Batch(ctx, customerBatch(MaxBatchSize), WithIdempotencyKey("carga-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != MaxBatchSize || out.Summary.Created != MaxBatchSize {
		t.Errorf("resultado: %d itens, summary %+v", len(out.Results), out.Summary)
	}
	if _, err := c.People.Batch(ctx, peopleBatch(MaxBatchSize)); err != nil {
		t.Fatal(err)
	}
	reqs := srv.recorded()
	if len(reqs) != 2 {
		t.Fatalf("%d requisições, esperado 2 (uma por lote)", len(reqs))
	}
	for i, path := range []string{"/api/v1/integration/customers/batch", "/api/v1/integration/people/batch"} {
		r := reqs[i]
		if r.Method != "POST" || r.Path != path {
			t.Errorf("requisição %d: %s %s", i, r.Method, r.Path)
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Items) != MaxBatchSize {
			t.Errorf("requisição %d: %d itens no corpo, esperado %d", i, len(body.Items), MaxBatchSize)
		}
	}
	if k := reqs[0].Header.Get("Idempotency-Key"); k != "carga-1" {
		t.Errorf("Idempotency-Key do lote: %q", k)
	}
	if want := `{"items":[{"customer_external_id":"erp-1","person":{"external_id":"app-0","name":"Pessoa 0"}},`; !strings.HasPrefix(string(reqs[1].Body), want) {
		t.Errorf("corpo de People.Batch precisa começar com %s", want)
	}
}

func TestBatchEmptyMakesNoRequest(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	ctx := context.Background()
	const zero = `{"results":[],"summary":{"created":0,"updated":0,"unchanged":0,"error":0}}`
	for _, empty := range [][]CustomerBatchItem{nil, {}} {
		out, err := c.Customers.Batch(ctx, empty)
		if err != nil || string(mustJSON(out)) != zero {
			t.Errorf("Customers.Batch vazio: %s, %v", mustJSON(out), err)
		}
	}
	for _, empty := range [][]PersonBatchItem{nil, {}} {
		out, err := c.People.Batch(ctx, empty)
		if err != nil || string(mustJSON(out)) != zero {
			t.Errorf("People.Batch vazio: %s, %v", mustJSON(out), err)
		}
	}
	mustNoRequests(t, srv)
}

func TestBatchValidatesItemsBeforeSending(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	ctx := context.Background()
	for name, items := range map[string][]CustomerBatchItem{
		"sem ExternalID":        {{Name: String("x")}},
		"ClearFields inválido":  {{ExternalID: "erp-1", ClearFields: []string{"nao_existe"}}},
		"ExternalID não limpa":  {{ExternalID: "erp-1", ClearFields: []string{"external_id"}}},
		"inválido depois de ok": {{ExternalID: "erp-1"}, {}},
	} {
		_, err := c.Customers.Batch(ctx, items)
		if err == nil {
			t.Errorf("Customers.Batch %s: esperava erro de argumento", name)
			continue
		}
		mustArgErr(t, err)
	}
	for name, items := range map[string][]PersonBatchItem{
		"sem cliente":           {{ExternalID: "app-1"}},
		"sem ExternalID":        {{CustomerExternalID: "erp-1"}},
		"cliente não limpa":     {{CustomerExternalID: "erp-1", ExternalID: "app-1", ClearFields: []string{"customer_external_id"}}},
		"ClearFields inválido":  {{CustomerExternalID: "erp-1", ExternalID: "app-1", ClearFields: []string{"cargo"}}},
		"inválido depois de ok": {{CustomerExternalID: "erp-1", ExternalID: "app-1"}, {ExternalID: "app-2"}},
	} {
		_, err := c.People.Batch(ctx, items)
		if err == nil {
			t.Errorf("People.Batch %s: esperava erro de argumento", name)
			continue
		}
		mustArgErr(t, err)
	}
	mustNoRequests(t, srv)
}

func TestBatchItemsOmittedVersusNull(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{batchOK(1), batchOK(1)})
	ctx := context.Background()
	_, _ = c.Customers.Batch(ctx, []CustomerBatchItem{{ExternalID: "erp-1", Email: String("a@b.example"), ClearFields: []string{"document"}}})
	_, _ = c.People.Batch(ctx, []PersonBatchItem{{CustomerExternalID: "erp-1", ExternalID: "app-1", Access: Bool(false), ClearFields: []string{"Phone"}}})
	want := []string{
		`{"items":[{"external_id":"erp-1","document":null,"email":"a@b.example"}]}`,
		`{"items":[{"customer_external_id":"erp-1","person":{"external_id":"app-1","phone":null,"access":false}}]}`,
	}
	for i, r := range srv.recorded() {
		if string(r.Body) != want[i] {
			t.Errorf("corpo %s\n    esperado %s", r.Body, want[i])
		}
	}
}

// ── pessoas e identificadores ────────────────────────────────────────────────

func TestPeopleUpsertWrapsBodyInPerson(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	_, _ = c.People.Upsert(ctx, "erp-1042", "app-77", nil)
	_, _ = c.People.Upsert(ctx, "erp-1042", "app-77", &PersonParams{
		Name: String("Paula"), Email: String("p@x.example"), Phone: String("1"), Role: String("Fin"), Access: Bool(true),
		IsPrimary: Bool(false), ExtraEmails: []string{"p2@x.example"}, ExtraPhones: []string{},
	})
	_, _ = c.People.Upsert(ctx, "erp-1042", "app-77", &PersonParams{ClearFields: []string{"role", "ExtraPhones"}})
	want := []string{
		`{"person":{}}`,
		`{"person":{"name":"Paula","email":"p@x.example","phone":"1","role":"Fin","access":true,"is_primary":false,"extra_emails":["p2@x.example"],"extra_phones":[]}}`,
		`{"person":{"role":null,"extra_phones":null}}`,
	}
	for i, r := range srv.recorded() {
		if r.Method != "PUT" || r.Path != "/api/v1/integration/customers/erp-1042/people/app-77" {
			t.Errorf("%s %s", r.Method, r.Path)
		}
		if string(r.Body) != want[i] {
			t.Errorf("corpo %s\n    esperado %s", r.Body, want[i])
		}
	}
}

func TestIdentifiersBodyOnlyWithLabelAndPaths(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	_, _ = c.Customers.Identifiers.Add(ctx, "ERP 1042", "crm 88", &IdentifierParams{Label: String("CRM")})
	_, _ = c.Customers.Identifiers.Add(ctx, "erp-1042", "crm-88", nil)
	_, _ = c.Customers.Identifiers.Add(ctx, "erp-1042", "crm-88", &IdentifierParams{})
	_, _ = c.People.Identifiers.Add(ctx, "app 77", "crm/p5", &IdentifierParams{Label: String("CRM")})
	_, _ = c.People.Identifiers.Remove(ctx, "app-77", "crm-p5")
	want := []struct{ method, path, body string }{
		{"PUT", "/api/v1/integration/customers/ERP%201042/identifiers/crm%2088", `{"label":"CRM"}`},
		{"PUT", "/api/v1/integration/customers/erp-1042/identifiers/crm-88", ""},
		{"PUT", "/api/v1/integration/customers/erp-1042/identifiers/crm-88", ""},
		{"PUT", "/api/v1/integration/people/app%2077/identifiers/crm%2Fp5", `{"label":"CRM"}`},
		{"DELETE", "/api/v1/integration/people/app-77/identifiers/crm-p5", ""},
	}
	for i, r := range srv.recorded() {
		if r.Method != want[i].method || r.Path != want[i].path || string(r.Body) != want[i].body {
			t.Errorf("chamada %d: %s %s %q, esperado %s %s %q", i, r.Method, r.Path, r.Body, want[i].method, want[i].path, want[i].body)
		}
		if want[i].body == "" && r.has("Content-Type") {
			t.Errorf("chamada %d: Content-Type sem corpo", i)
		}
	}
}

func TestPeopleInvalidPathParamsFailBeforeAnyRequest(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"upsert sem cliente":        func() error { _, err := c.People.Upsert(ctx, "", "app-1", nil); return err },
		"upsert pessoa ..":          func() error { _, err := c.People.Upsert(ctx, "erp-1", "..", nil); return err },
		"list cliente .":            func() error { _, err := c.People.List(ctx, "."); return err },
		"delete pessoa vazia":       func() error { _, err := c.People.Delete(ctx, "erp-1", ""); return err },
		"identificador vazio":       func() error { _, err := c.Customers.Identifiers.Add(ctx, "erp-1", "", nil); return err },
		"cliente .. no identif.":    func() error { _, err := c.Customers.Identifiers.Remove(ctx, "..", "crm-1"); return err },
		"pessoa vazia no identif.":  func() error { _, err := c.People.Identifiers.Add(ctx, "", "crm-1", nil); return err },
		"identificador . da pessoa": func() error { _, err := c.People.Identifiers.Remove(ctx, "app-1", "."); return err },
		"ClearFields desconhecido": func() error {
			_, err := c.People.Upsert(ctx, "erp-1", "app-1", &PersonParams{ClearFields: []string{"cargo"}})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: esperava erro de argumento", name)
		} else {
			mustArgErr(t, err)
		}
	}
	mustNoRequests(t, srv)
}

// ── ListAll ──────────────────────────────────────────────────────────────────

func TestListAllWalksEveryPage(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{
		okPage([]any{customerJSON(1), customerJSON(2)}, 1, 2, 5, 3),
		okPage([]any{customerJSON(3), customerJSON(4)}, 2, 2, 5, 3),
		okPage([]any{customerJSON(5)}, 3, 2, 5, 3),
	})
	it := c.Customers.ListAll(&CustomerListParams{Q: String("padaria"), PageSize: Int(2)})
	mustNoRequests(t, srv) // preguiçoso: nada vai à rede antes do 1º Next

	var got []string
	for it.Next(context.Background()) {
		got = append(got, it.Current().ExternalID)
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"C1", "C2", "C3", "C4", "C5"}) {
		t.Errorf("itens %v", got)
	}
	if it.Next(context.Background()) {
		t.Error("Next depois do fim devolveu true")
	}
	reqs := srv.recorded()
	if len(reqs) != 3 {
		t.Fatalf("%d requisições, esperado 3", len(reqs))
	}
	ids := map[string]bool{}
	for i, r := range reqs {
		want := fmt.Sprintf("q=padaria&page=%d&page_size=2", i+1)
		if r.RawQuery != want {
			t.Errorf("página %d: query %q, esperado %q", i+1, r.RawQuery, want)
		}
		ids[r.Header.Get("X-Request-Id")] = true
	}
	if len(ids) != 3 {
		t.Error("cada página é uma chamada lógica nova: X-Request-Id próprio")
	}
}

func TestListAllDefaultPageSize100(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{okPage([]any{}, 1, 100, 0, 0)})
	it := c.Customers.ListAll(nil)
	for it.Next(context.Background()) {
		t.Fatal("não devia ter itens")
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if q := srv.recorded()[0].RawQuery; q != "page=1&page_size=100" {
		t.Errorf("query %q", q)
	}
}

func TestListAllStopsOnEmptyPage(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{okPage([]any{}, 1, 2, 9, 5)})
	it := c.Customers.ListAll(&CustomerListParams{PageSize: Int(2)})
	for it.Next(context.Background()) {
		t.Fatal("não devia ter itens")
	}
	if n := len(srv.recorded()); n != 1 {
		t.Errorf("%d requisições, esperado 1 (página vazia encerra)", n)
	}
}

func TestListAllOtherResources(t *testing.T) {
	srv := newFakeServer(t)
	one := okPage([]any{map[string]any{"id": "i"}}, 1, 100, 1, 1)
	c, _ := newTestClient(t, srv, []fakeResponse{one, one, one})
	ctx := context.Background()
	n := 0
	for it := c.Customers.Interactions.ListAll("ERP 1", nil); it.Next(ctx); {
		n++
	}
	for it := c.ReleaseNotes.ListAll("erp", &ReleaseNoteListParams{Published: Bool(false)}); it.Next(ctx); {
		n++
	}
	for it := c.KB.Articles.ListAll(&KBArticleListParams{Product: String("erp"), Status: String("draft")}); it.Next(ctx); {
		n++
	}
	if n != 3 {
		t.Errorf("%d itens, esperado 3", n)
	}
	want := []string{
		"/api/v1/integration/customers/ERP%201/interactions?page=1&page_size=100",
		"/api/v1/integration/products/erp/release-notes?published=false&page=1&page_size=100",
		"/api/v1/integration/kb/articles?product=erp&status=draft&page=1&page_size=100",
	}
	var got []string
	for _, r := range srv.recorded() {
		got = append(got, r.Path+"?"+r.RawQuery)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requisições\n  %v\nesperado\n  %v", got, want)
	}
}

func TestListAllStartPageAndInvalidSizes(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{okPage([]any{customerJSON(7)}, 3, 10, 21, 3)})
	it := c.Customers.ListAll(&CustomerListParams{Page: Int(3), PageSize: Int(10)})
	for it.Next(context.Background()) {
	}
	if q := srv.recorded()[0].RawQuery; q != "page=3&page_size=10" {
		t.Errorf("query %q", q)
	}

	srv.reset()
	for _, p := range []*CustomerListParams{{PageSize: Int(0)}, {Page: Int(0)}, {Page: Int(-1)}} {
		it := c.Customers.ListAll(p)
		if it.Next(context.Background()) {
			t.Fatal("iterador inválido devolveu item")
		}
		mustArgErr(t, it.Err())
	}
	mustNoRequests(t, srv)
}

func TestListAllStopsOnError(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{
		okPage([]any{customerJSON(1)}, 1, 1, 3, 3),
		fail(404, "TENANT_NOT_FOUND", nil),
	})
	it := c.Customers.ListAll(&CustomerListParams{PageSize: Int(1)})
	var got []string
	for it.Next(context.Background()) {
		got = append(got, it.Current().ExternalID)
	}
	if !reflect.DeepEqual(got, []string{"C1"}) {
		t.Errorf("itens entregues antes do erro: %v", got)
	}
	if e := apiErr(t, it.Err()); e.Code != "TENANT_NOT_FOUND" {
		t.Errorf("Code %q", e.Code)
	}
	if it.Next(context.Background()) || len(srv.recorded()) != 2 {
		t.Error("depois de um erro, Next não busca de novo")
	}
}

func TestPageFields(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{okPage([]any{customerJSON(1)}, 1, 1, 2, 2)})
	p, err := c.Customers.List(context.Background(), &CustomerListParams{PageSize: Int(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].ExternalID != "C1" || p.Page != 1 || p.PageSize != 1 || p.Total != 2 || p.Pages != 2 {
		t.Errorf("página %+v", p)
	}
	if !p.HasNext() {
		t.Error("HasNext")
	}
	if got := string(mustJSON(&Page[int]{Items: []int{}, Page: 2, PageSize: 3, Total: 4, Pages: 5})); got != `{"items":[],"page":2,"page_size":3,"total":4,"pages":5}` {
		t.Errorf("JSON da Page: %s", got)
	}
}

// ── novas tentativas ─────────────────────────────────────────────────────────

func TestRetryExponentialBackoffWithJitterKeepsIDs(t *testing.T) {
	srv := newFakeServer(t)
	c, sleeps := newTestClient(t, srv, []fakeResponse{fail(503, "X", nil), fail(504, "X", nil), ok(map[string]any{"deleted": true})})
	out, err := c.Customers.Delete(context.Background(), "C1")
	if err != nil || !out.Deleted {
		t.Fatalf("Delete: %+v %v", out, err)
	}
	waits := sleeps.all()
	if len(waits) != 2 {
		t.Fatalf("esperas %v", waits)
	}
	if waits[0] < 500*time.Millisecond || waits[0] > 625*time.Millisecond {
		t.Errorf("1ª espera %s, esperado 0,5–0,625 s", waits[0])
	}
	if waits[1] < time.Second || waits[1] > 1250*time.Millisecond {
		t.Errorf("2ª espera %s, esperado 1–1,25 s", waits[1])
	}
	keys, ids := map[string]bool{}, map[string]bool{}
	for _, r := range srv.recorded() {
		keys[r.Header.Get("Idempotency-Key")] = true
		ids[r.Header.Get("X-Request-Id")] = true
	}
	if len(keys) != 1 || len(ids) != 1 {
		t.Errorf("as novas tentativas repetem os ids: %d chaves, %d request ids", len(keys), len(ids))
	}
}

func TestRetryOnlyOnRetryableStatuses(t *testing.T) {
	srv := newFakeServer(t)
	for status, retried := range map[int]bool{
		429: true, 502: true, 503: true, 504: true,
		400: false, 401: false, 403: false, 404: false, 409: false, 422: false, 500: false, 501: false,
	} {
		c, sleeps := newTestClient(t, srv, []fakeResponse{fail(status, "X", nil), ok(map[string]any{"id": "x"})}, WithMaxRetries(1))
		_, err := c.Products.Get(context.Background(), "erp")
		n := len(srv.recorded())
		if retried && (err != nil || n != 2 || len(sleeps.all()) != 1) {
			t.Errorf("%d devia ser repetido: %d requisições, err %v", status, n, err)
		}
		if !retried && (err == nil || n != 1 || len(sleeps.all()) != 0) {
			t.Errorf("%d não devia ser repetido: %d requisições, err %v", status, n, err)
		}
	}
}

func TestRetryAfterIsCappedAt60s(t *testing.T) {
	srv := newFakeServer(t)
	c, sleeps := newTestClient(t, srv, []fakeResponse{fail(429, "RATE_LIMITED", map[string]string{"Retry-After": "120"}), ok(map[string]any{"id": "x"})})
	if _, err := c.Products.Get(context.Background(), "erp"); err != nil {
		t.Fatal(err)
	}
	if w := sleeps.all(); !reflect.DeepEqual(w, []time.Duration{60 * time.Second}) {
		t.Errorf("esperas %v, esperado [1m0s]", w)
	}
}

func TestRetryAfterOnAnyRetriedStatus(t *testing.T) {
	srv := newFakeServer(t)
	c, sleeps := newTestClient(t, srv, []fakeResponse{fail(503, "X", map[string]string{"Retry-After": "3"}), ok(map[string]any{"id": "x"})})
	if _, err := c.Products.Get(context.Background(), "erp"); err != nil {
		t.Fatal(err)
	}
	if w := sleeps.all(); !reflect.DeepEqual(w, []time.Duration{3 * time.Second}) {
		t.Errorf("esperas %v, esperado [3s]", w)
	}

	// O campo RetryAfter do erro só é preenchido em 429.
	c, _ = newTestClient(t, srv, []fakeResponse{fail(503, "X", map[string]string{"Retry-After": "3"})}, WithMaxRetries(0))
	_, err := c.Products.Get(context.Background(), "erp")
	if e := apiErr(t, err); e.RetryAfter != 0 || !errors.Is(err, ErrServer) {
		t.Errorf("503: RetryAfter %s (esperado 0), err %v", e.RetryAfter, err)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	srv := newFakeServer(t)
	when := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	c, sleeps := newTestClient(t, srv, []fakeResponse{fail(429, "RATE_LIMITED", map[string]string{"Retry-After": when}), ok(map[string]any{"id": "x"})})
	if _, err := c.Products.Get(context.Background(), "erp"); err != nil {
		t.Fatal(err)
	}
	if w := sleeps.all(); len(w) != 1 || w[0] < 25*time.Second || w[0] > 31*time.Second {
		t.Errorf("esperas %v, esperado ~30s", w)
	}
}

func TestMaxRetriesZeroAndRateLimitError(t *testing.T) {
	srv := newFakeServer(t)
	c, sleeps := newTestClient(t, srv, []fakeResponse{fail(429, "RATE_LIMITED", map[string]string{"Retry-After": "2"})}, WithMaxRetries(0))
	_, err := c.Products.Get(context.Background(), "erp")
	if !errors.Is(err, ErrRateLimit) {
		t.Fatalf("esperava ErrRateLimit, veio %v", err)
	}
	if e := apiErr(t, err); e.RetryAfter != 2*time.Second || e.Status != 429 {
		t.Errorf("RetryAfter %s, Status %d", e.RetryAfter, e.Status)
	}
	if len(srv.recorded()) != 1 || len(sleeps.all()) != 0 {
		t.Error("MaxRetries 0 não tenta de novo")
	}
}

func TestNonJSONErrorExhausted(t *testing.T) {
	srv := newFakeServer(t)
	bad := text(502, "Bad Gateway", nil)
	c, _ := newTestClient(t, srv, []fakeResponse{bad, bad, bad})
	_, err := c.Products.Get(context.Background(), "erp")
	e := apiErr(t, err)
	if e.Code != "HTTP_502" || e.Status != 502 || !errors.Is(err, ErrServer) {
		t.Errorf("erro %+v", e)
	}
	if n := len(srv.recorded()); n != 3 {
		t.Errorf("%d requisições, esperado 3 (1 + 2 novas tentativas)", n)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	for raw, want := range map[string]time.Duration{
		"7":   7 * time.Second,
		"1.5": 1500 * time.Millisecond,
		" 0 ": 0,
		now.Add(30 * time.Second).Format(http.TimeFormat):  30 * time.Second,
		now.Add(-30 * time.Second).Format(http.TimeFormat): 0,
	} {
		got := parseRetryAfter(raw, now)
		if got == nil || *got != want {
			t.Errorf("parseRetryAfter(%q) = %v, esperado %s", raw, got, want)
		}
	}
	for _, raw := range []string{"", "amanhã", "-1", "NaN"} {
		if got := parseRetryAfter(raw, now); got != nil {
			t.Errorf("parseRetryAfter(%q) = %s, esperado nil", raw, *got)
		}
	}
}

func TestBackoff(t *testing.T) {
	for attempt, base := range map[int]time.Duration{0: 500 * time.Millisecond, 1: time.Second, 2: 2 * time.Second, 5: 8 * time.Second, 10: 8 * time.Second} {
		for i := 0; i < 20; i++ {
			if d := backoff(attempt, nil); d < base || d > base+base/4 {
				t.Fatalf("backoff(%d) = %s, esperado entre %s e %s", attempt, d, base, base+base/4)
			}
		}
	}
	for ra, want := range map[time.Duration]time.Duration{120 * time.Second: 60 * time.Second, 5 * time.Second: 5 * time.Second, -time.Second: 0} {
		ra := ra
		if got := backoff(0, &ra); got != want {
			t.Errorf("backoff com Retry-After %s = %s, esperado %s", ra, got, want)
		}
	}
}

// ── rede, tempo esgotado e cancelamento ─────────────────────────────────────

// idRecorder anota o X-Request-Id de cada tentativa antes de delegar ao transporte real.
type idRecorder struct {
	mu   sync.Mutex
	ids  []string
	base http.RoundTripper
}

func (r *idRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.ids = append(r.ids, req.Header.Get("X-Request-Id"))
	r.mu.Unlock()
	return r.base.RoundTrip(req)
}

func TestNetworkErrorWhenServerIsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // ninguém escuta nesta porta

	rec := &idRecorder{base: http.DefaultTransport.(*http.Transport).Clone()}
	c, sleeps := newClientAt(t, "http://"+addr, WithTimeout(2*time.Second), WithHTTPClient(&http.Client{Transport: rec}))
	_, err = c.Customers.Get(context.Background(), "C1")

	e := apiErr(t, err)
	if e.Status != 0 || e.Code != CodeNetworkError || e.Type != ErrorTypeNetwork || !errors.Is(err, ErrNetwork) {
		t.Errorf("erro de rede: %+v", e)
	}
	if len(sleeps.all()) != 2 || len(rec.ids) != 3 {
		t.Errorf("rede é repetida MaxRetries vezes: %d esperas, %d tentativas", len(sleeps.all()), len(rec.ids))
	}
	for _, id := range rec.ids {
		if id != rec.ids[0] {
			t.Error("as tentativas repetem o X-Request-Id")
		}
	}
	if e.RequestID != rec.ids[0] || e.RequestID == "" {
		t.Errorf("RequestID %q, esperado o enviado %q", e.RequestID, rec.ids[0])
	}
	if !strings.Contains(err.Error(), "NETWORK_ERROR") || e.Unwrap() == nil {
		t.Errorf("mensagem/causa: %v (causa %v)", err, e.Unwrap())
	}
}

func TestAttemptTimeoutIsNetworkErrorAndRetried(t *testing.T) {
	srv := newFakeServer(t)
	srv.block = true
	c, sleeps := newClientAt(t, srv.URL, WithTimeout(200*time.Millisecond), WithMaxRetries(1))
	started := time.Now()
	_, err := c.Customers.Get(context.Background(), "C1")
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("demorou %s", elapsed)
	}
	e := apiErr(t, err)
	if !errors.Is(err, ErrNetwork) || e.Code != CodeNetworkError || !strings.Contains(e.Message, "tempo esgotado") {
		t.Errorf("tempo esgotado vira erro de rede: %v", err)
	}
	reqs := srv.recorded()
	if len(reqs) != 2 || len(sleeps.all()) != 1 {
		t.Fatalf("%d tentativas, %d esperas; esperado 2 e 1", len(reqs), len(sleeps.all()))
	}
	if reqs[0].Header.Get("X-Request-Id") != reqs[1].Header.Get("X-Request-Id") || e.RequestID != reqs[0].Header.Get("X-Request-Id") {
		t.Error("o X-Request-Id se repete na nova tentativa e vai no erro")
	}
}

func TestCallerCancellationIsNotRetried(t *testing.T) {
	srv := newFakeServer(t)
	srv.block = true
	c, sleeps := newClientAt(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-srv.arrived
		cancel()
	}()
	_, err := c.Customers.Get(ctx, "C1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperava context.Canceled, veio %v", err)
	}
	var e *Error
	if errors.As(err, &e) {
		t.Error("cancelamento por quem chama não é *bfocus.Error")
	}
	if len(srv.recorded()) != 1 || len(sleeps.all()) != 0 {
		t.Error("cancelamento não gera nova tentativa")
	}
}

func TestSleepContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("espera com ctx cancelado: %v", err)
	}
	if err := sleepContext(context.Background(), 0); err != nil {
		t.Errorf("espera zero: %v", err)
	}
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("espera curta: %v", err)
	}
}

// ── caminho, query e corpo ──────────────────────────────────────────────────

func TestQueryDatesInUTCWithZ(t *testing.T) {
	srv := newFakeServer(t)
	brt := time.FixedZone("BRT", -3*60*60)
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 9, 1, 0, 0, 0, 0, brt), "2026-09-01T03:00:00Z"},
		{time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), "2026-09-01T03:00:00Z"},
		{time.Date(2026, 9, 1, 3, 0, 0, 250_000_000, time.UTC), "2026-09-01T03:00:00.25Z"},
	}
	var responses []fakeResponse
	for range cases {
		responses = append(responses, okPage([]any{}, 1, 50, 0, 0))
	}
	c, _ := newTestClient(t, srv, responses)
	for _, tc := range cases {
		if _, err := c.Customers.List(context.Background(), &CustomerListParams{UpdatedSince: Time(tc.in)}); err != nil {
			t.Fatal(err)
		}
	}
	for i, r := range srv.recorded() {
		if got := r.Query.Get("updated_since"); got != cases[i].want {
			t.Errorf("updated_since %q, esperado %q", got, cases[i].want)
		}
	}
}

func TestQueryBooleansOmittedAndEncoded(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok([]any{}), ok([]any{}), ok([]any{}), ok([]any{})})
	ctx := context.Background()
	_, _ = c.Products.List(ctx, &ProductListParams{IncludeInactive: Bool(false)})
	_, _ = c.Products.List(ctx, nil)
	_, _ = c.KB.Search(ctx, "nota fiscal & NFS-e", &KBSearchParams{Limit: Int(3)})
	_, _ = c.Customers.Contacts.List(ctx, "C1")
	reqs := srv.recorded()
	for i, want := range []string{"include_inactive=false", "", "q=nota%20fiscal%20%26%20NFS-e&limit=3", ""} {
		if reqs[i].RawQuery != want {
			t.Errorf("requisição %d: query %q, esperado %q", i, reqs[i].RawQuery, want)
		}
	}
}

func TestBodyOmittedVersusNull(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	calls := []*CustomerUpsertParams{
		nil,
		{},
		{Name: String("Novo"), ClearFields: []string{"phone"}},
		{CustomFields: []CustomFieldInput{}},
		{Phone: String("11 3333-4444"), ClearFields: []string{"Phone", "Website"}}, // valor vence; nome Go vale
	}
	for _, p := range calls {
		if _, err := c.Customers.Upsert(ctx, "C1", p); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{`{}`, `{}`, `{"name":"Novo","phone":null}`, `{"custom_fields":[]}`, `{"phone":"11 3333-4444","website":null}`}
	for i, r := range srv.recorded() {
		if string(r.Body) != want[i] {
			t.Errorf("chamada %d: corpo %s, esperado %s", i, r.Body, want[i])
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("chamada %d: Content-Type %q", i, r.Header.Get("Content-Type"))
		}
	}
}

func TestClearFieldsRejectsUnknownOrUnclearable(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	ctx := context.Background()
	_, err := c.Customers.Upsert(ctx, "C1", &CustomerUpsertParams{ClearFields: []string{"telefone"}})
	mustArgErr(t, err)
	_, err = c.ReleaseNotes.Upsert(ctx, "erp", "2.3.0", &ReleaseNoteUpsertParams{ClearFields: []string{"publish"}})
	mustArgErr(t, err)
	_, err = c.Products.Upsert(ctx, "erp", &ProductUpsertParams{ClearFields: []string{"ClearFields"}})
	mustArgErr(t, err)
	mustNoRequests(t, srv)
}

func TestEveryUpsertParamsFieldIsSent(t *testing.T) {
	// Todo campo (menos ClearFields) de todo struct de upsert precisa ir no corpo com o
	// nome JSON da spec quando informado — e sumir quando nil.
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	_, _ = c.Customers.Contacts.Upsert(ctx, "C1", "CT-1", &ContactUpsertParams{
		Name: String("Ana"), Role: String("Fin"), Email: String("a@b.example"), Phone: String("1"), Notes: String("n"), IsPrimary: Bool(false),
	})
	_, _ = c.Products.Upsert(ctx, "erp", &ProductUpsertParams{
		Name: String("ERP"), Description: String("d"), Color: String("#6366F1"), Icon: String("i"), IsActive: Bool(true), SortOrder: Int(0),
	})
	_, _ = c.ReleaseNotes.Upsert(ctx, "erp", "2.3.0", &ReleaseNoteUpsertParams{
		Title: String("t"), DescriptionHTML: String("<p>x</p>"), DescriptionMarkdown: String("x"), Audience: String("both"),
		RequireAckInternal: Bool(true), RequireAckExternal: Bool(false), Publish: Bool(true),
	})
	_, _ = c.KB.Articles.Upsert(ctx, "git:x", &KBArticleUpsertParams{
		Title: String("t"), BodyHTML: String("<p>&</p>"), BodyMarkdown: String("m"), Product: String("erp"), Status: String("draft"),
	})
	want := []string{
		`{"name":"Ana","role":"Fin","email":"a@b.example","phone":"1","notes":"n","is_primary":false}`,
		`{"name":"ERP","description":"d","color":"#6366F1","icon":"i","is_active":true,"sort_order":0}`,
		`{"title":"t","description_html":"<p>x</p>","description_markdown":"x","audience":"both","require_ack_internal":true,"require_ack_external":false,"publish":true}`,
		`{"title":"t","body_html":"<p>&</p>","body_markdown":"m","product":"erp","status":"draft"}`,
	}
	for i, r := range srv.recorded() {
		if string(r.Body) != want[i] {
			t.Errorf("corpo %s\n    esperado %s", r.Body, want[i])
		}
	}
}

func TestPathSegmentsArePercentEncoded(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	_, _ = c.Customers.Get(ctx, "ERP/1042 ç")
	_, _ = c.Customers.Contacts.Delete(ctx, "A B", "c?d")
	_, _ = c.ReleaseNotes.Get(ctx, "erp", "v2.3.0")
	_, _ = c.Customers.Get(ctx, "a:b@c+d#e%f")
	_, _ = c.KB.Articles.Get(ctx, "git:guia:instalação")
	want := []string{
		"/api/v1/integration/customers/ERP%2F1042%20%C3%A7",
		"/api/v1/integration/customers/A%20B/contacts/c%3Fd",
		"/api/v1/integration/products/erp/release-notes/v2.3.0",
		"/api/v1/integration/customers/a%3Ab%40c%2Bd%23e%25f",
		"/api/v1/integration/kb/articles/git%3Aguia%3Ainstala%C3%A7%C3%A3o",
	}
	for i, r := range srv.recorded() {
		if r.Path != want[i] {
			t.Errorf("caminho %q, esperado %q", r.Path, want[i])
		}
	}
}

func TestInvalidPathParamsFailBeforeAnyRequest(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, nil)
	ctx := context.Background()
	iterErr := func(err error) error { return err }
	for name, call := range map[string]func() error{
		"artigo com /": func() error { _, err := c.KB.Articles.Get(ctx, "docs/guia"); return err },
		"upsert de artigo com /": func() error {
			_, err := c.KB.Articles.Upsert(ctx, "a/b", &KBArticleUpsertParams{Title: String("x")})
			return err
		},
		"cliente vazio":             func() error { _, err := c.Customers.Get(ctx, ""); return err },
		"cliente .":                 func() error { _, err := c.Customers.Get(ctx, "."); return err },
		"cliente ..":                func() error { _, err := c.Customers.Delete(ctx, ".."); return err },
		"contato vazio":             func() error { _, err := c.Customers.Contacts.Upsert(ctx, "C1", "", nil); return err },
		"produto ..":                func() error { _, err := c.Customers.Products.Attach(ctx, "C1", ".."); return err },
		"versão .":                  func() error { _, err := c.ReleaseNotes.Get(ctx, "erp", "."); return err },
		"list_all de notas vazio":   func() error { return iterErr(c.ReleaseNotes.ListAll("", nil).Err()) },
		"list_all de interações ..": func() error { return iterErr(c.Customers.Interactions.ListAll("..", nil).Err()) },
		"publicar artigo ..":        func() error { _, err := c.KB.Articles.Publish(ctx, ".."); return err },
		"agente vazio":              func() error { _, err := c.AIAgents.Get(ctx, ""); return err },
		"preview em agente .":       func() error { _, err := c.AIAgents.Preview(ctx, ".", "oi", nil); return err },
		"produto vazio":             func() error { _, err := c.Products.Archive(ctx, ""); return err },
		"notas de produto ..":       func() error { _, err := c.ReleaseNotes.List(ctx, "..", nil); return err },
		"interação em cliente .":    func() error { _, err := c.Customers.Interactions.Create(ctx, ".", "x", nil); return err },
	} {
		err := call()
		if err == nil {
			t.Errorf("%s: esperava erro de argumento", name)
			continue
		}
		mustArgErr(t, err)
	}
	mustNoRequests(t, srv)
}

func TestIdempotencyKeyOnlyOnWritesAndPerCall(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	_, _ = c.Customers.Upsert(ctx, "C1", &CustomerUpsertParams{Name: String("x")}, WithIdempotencyKey("minha-chave"))
	_, _ = c.Customers.Get(ctx, "C1")
	_, _ = c.Customers.Products.Attach(ctx, "C1", "erp")
	_, _ = c.Customers.Delete(ctx, "C1")
	_, _ = c.Customers.Delete(ctx, "C1")
	r := srv.recorded()
	if got := r[0].Header.Get("Idempotency-Key"); got != "minha-chave" {
		t.Errorf("chave do usuário: %q", got)
	}
	if r[1].has("Idempotency-Key") || r[1].has("Content-Type") || len(r[1].Body) != 0 {
		t.Error("GET não leva Idempotency-Key, Content-Type nem corpo")
	}
	if r[2].Header.Get("Idempotency-Key") == "" || r[2].has("Content-Type") || len(r[2].Body) != 0 {
		t.Error("PUT sem corpo continua sendo escrita (Idempotency-Key), sem Content-Type")
	}
	if r[3].Header.Get("Idempotency-Key") == r[4].Header.Get("Idempotency-Key") || r[3].Header.Get("X-Request-Id") == r[4].Header.Get("X-Request-Id") {
		t.Error("cada chamada lógica tem ids próprios")
	}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if k := r[3].Header.Get("Idempotency-Key"); !uuid.MatchString(k) {
		t.Errorf("Idempotency-Key gerada %q não é uuid4", k)
	}
}

func TestBodyIsUTF8WithoutHTMLEscaping(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{}), ok(map[string]any{})})
	ctx := context.Background()
	_, _ = c.Customers.Interactions.Create(ctx, "C1", "Pedido faturado — ação ✓ <b>&</b>", &InteractionCreateParams{IsInternal: Bool(false)})
	_, _ = c.AIAgents.Preview(ctx, "ag-1", "Oi", nil)
	r := srv.recorded()
	if got, want := string(r[0].Body), `{"content":"Pedido faturado — ação ✓ <b>&</b>","is_internal":false}`; got != want {
		t.Errorf("corpo %s\n    esperado %s", got, want)
	}
	if got := string(r[1].Body); got != `{"message":"Oi"}` {
		t.Errorf("preview sem histórico: %s", got)
	}
}

// ── erros ────────────────────────────────────────────────────────────────────

func TestErrorFieldsAndMessage(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{{
		Status:  403,
		Headers: map[string]string{"X-Required-Scope": "kb:write"},
		Body: mustJSON(map[string]any{"code": 403, "data": nil, "message": "INTEGRATION_SCOPE_MISSING",
			"error": "INTEGRATION_SCOPE_MISSING", "validation": map[string]any{}, "request_id": "req-9"}),
	}})
	_, err := c.KB.Articles.Publish(context.Background(), "git:x")
	e := apiErr(t, err)
	if e.RequiredScope != "kb:write" || !strings.Contains(e.Message, "kb:write") || !strings.Contains(err.Error(), "req-9") {
		t.Errorf("erro %+v / %v", e, err)
	}
	if e.RetryAfter != 0 || len(e.Validation) != 0 || !errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrNotFound) {
		t.Errorf("erro %+v", e)
	}
}

func TestModuleNotContractedIsPermissionDenied(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{fail(403, "MODULE_NOT_CONTRACTED", map[string]string{"X-Required-Module": "atendimento"})})
	_, err := c.KB.Search(context.Background(), "nota", nil)
	e := apiErr(t, err)
	if !errors.Is(err, ErrPermissionDenied) || e.Code != "MODULE_NOT_CONTRACTED" || !strings.Contains(e.Message, "atendimento") {
		t.Errorf("erro %v", err)
	}
}

func TestValidationError(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{raw(422,
		`{"code":422,"data":null,"message":"Dados inválidos","error":"VALIDATION_ERROR","validation":{"email":"value is not a valid email address","name":["obrigatório"]}}`, nil)})
	_, err := c.Customers.Contacts.Upsert(context.Background(), "C1", "CT-1", &ContactUpsertParams{Email: String("x")})
	e := apiErr(t, err)
	if !errors.Is(err, ErrValidation) || e.Validation["email"] != "value is not a valid email address" || e.Validation["name"] != `["obrigatório"]` {
		t.Errorf("Validation %v", e.Validation)
	}
	if !strings.Contains(e.Message, "email: value is not a valid email address") || !strings.Contains(e.Message, "Dados inválidos") {
		t.Errorf("mensagem %q", e.Message)
	}
}

func TestErrorDataCarriesContactOwner(t *testing.T) {
	// 409 acionável: Data diz de QUEM é o contato (e a API repete em Validation).
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{
		raw(409, `{"code":409,"data":{"field":"email","owner_external_id":"app-12","owner_name":"Paula Reis","owner_customer_external_id":"erp-1042"},`+
			`"message":"PERSON_EMAIL_TAKEN","error":"PERSON_EMAIL_TAKEN","validation":{"field":"email","owner_external_id":"app-12"},"request_id":"r1"}`, nil),
		raw(404, `{"code":404,"data":null,"error":"CUSTOMER_NOT_FOUND"}`, nil),
	})
	_, err := c.People.Upsert(context.Background(), "erp-1042", "app-77", &PersonParams{Email: String("paula@padaria.example")})
	e := apiErr(t, err)
	if !errors.Is(err, ErrConflict) || e.Code != "PERSON_EMAIL_TAKEN" {
		t.Fatalf("erro %v", err)
	}
	if e.Data["owner_external_id"] != "app-12" || e.Data["owner_customer_external_id"] != "erp-1042" ||
		e.Data["owner_name"] != "Paula Reis" || e.Data["field"] != "email" {
		t.Errorf("Data %v", e.Data)
	}
	if e.Validation["owner_external_id"] != "app-12" {
		t.Errorf("Validation %v", e.Validation)
	}

	_, err = c.Customers.Get(context.Background(), "erp-1042")
	if e := apiErr(t, err); e.Data != nil {
		t.Errorf("sem detalhe, Data devia ser nil: %v", e.Data)
	}
}

func TestInvalidSuccessBodyIsInvalidResponse(t *testing.T) {
	srv := newFakeServer(t)
	bodies := []fakeResponse{
		text(200, "<html>proxy</html>", nil),
		{Status: 200},                              // corpo vazio
		raw(200, `{"id":"sem envelope"}`, nil),     // sem data
		raw(200, `{"code":200,"data":null}`, nil),  // data null
		raw(200, `{"code":200,"data":[1,2]}`, nil), // data de outro formato
		raw(201, `[1,2]`, map[string]string{"X-Request-Id": "req-do-header"}),
	}
	c, _ := newTestClient(t, srv, bodies)
	var errs []*Error
	for range bodies {
		_, err := c.Products.Get(context.Background(), "erp")
		e := apiErr(t, err)
		for _, s := range errorSentinels {
			if errors.Is(err, s) {
				t.Errorf("INVALID_RESPONSE é a classe base (Type api), casou com %v", s)
			}
		}
		errs = append(errs, e)
	}
	reqs := srv.recorded()
	for i, e := range errs {
		if e.Code != CodeInvalidResponse || e.Type != ErrorTypeAPI || !strings.Contains(e.Message, CodeInvalidResponse) {
			t.Errorf("resposta %d: %+v", i, e)
		}
		wantStatus, wantID := 200, reqs[i].Header.Get("X-Request-Id")
		if i == len(errs)-1 {
			wantStatus, wantID = 201, "req-do-header"
		}
		if e.Status != wantStatus || e.RequestID != wantID {
			t.Errorf("resposta %d: status %d / request id %q, esperado %d / %q", i, e.Status, e.RequestID, wantStatus, wantID)
		}
	}
}

func TestErrorRequestIDFallsBackToSent(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{text(404, "Not Found", nil), raw(409, `{"code":409,"error":"X"}`, nil)})
	for i := 0; i < 2; i++ {
		_, err := c.Products.Get(context.Background(), "erp")
		sent := srv.recorded()[i].Header.Get("X-Request-Id")
		if e := apiErr(t, err); e.RequestID != sent || sent == "" {
			t.Errorf("RequestID %q, esperado o enviado %q", e.RequestID, sent)
		}
	}
}

func TestErrorCodeFallsBackToMessage(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{raw(404, `{"code":404,"message":"TENANT_NOT_FOUND"}`, nil), raw(400, `{"code":400}`, nil)})
	_, err := c.Products.List(context.Background(), nil)
	if e := apiErr(t, err); e.Code != "TENANT_NOT_FOUND" || len(e.Validation) != 0 || !errors.Is(err, ErrNotFound) {
		t.Errorf("erro %+v", e)
	}
	_, err = c.Products.List(context.Background(), nil)
	if e := apiErr(t, err); e.Code != "HTTP_400" || e.Type != ErrorTypeAPI {
		t.Errorf("erro %+v", e)
	}
}

// ── campos desconhecidos e modelos ──────────────────────────────────────────

func TestUnknownResponseFieldsAreIgnoredOrPreserved(t *testing.T) {
	srv := newFakeServer(t)
	customer := customerJSON(1)
	customer["campo_novo"] = map[string]any{"qualquer": []any{1, 2}}
	customer["custom_fields"] = []any{map[string]any{"key": "plano", "value": 3, "cor": "azul"}}
	c, _ := newTestClient(t, srv, []fakeResponse{
		raw(200, `{"code":200,"data":`+string(mustJSON(customer))+`,"message":"ok","extra_no_envelope":true}`, nil),
		ok(map[string]any{"action": "answer", "answer_html": nil, "escalated": false, "refused": false, "handoff_reason": nil,
			"confidence": nil, "topic": nil, "guards": []any{}, "citations": []any{}, "sources": []any{}, "collected": map[string]any{},
			"missing": []any{}, "debug": map[string]any{"latency_ms": 42}}),
	})
	got, err := c.Customers.Get(context.Background(), "C1")
	if err != nil {
		t.Fatal(err)
	}
	f := got.CustomFields[0]
	if f.Key != "plano" || f.Type != "text" || f.Visibility != "interno" || f.Value != float64(3) || string(f.Extra["cor"]) != `"azul"` {
		t.Errorf("campo personalizado %+v", f)
	}
	// O que a API mandou volta igual, inclusive o que a SDK não conhece; o que ela NÃO
	// mandou não é inventado de volta (campo personalizado de pessoa vem sem `type`, e o
	// JSON do modelo precisa bater com a resposta — só a leitura assume o padrão da spec).
	if b := string(mustJSON(f)); !strings.Contains(b, `"cor":"azul"`) || strings.Contains(b, `"visibility"`) || strings.Contains(b, `"type"`) {
		t.Errorf("Extra volta no JSON: %s", b)
	}

	preview, err := c.AIAgents.Preview(context.Background(), "ag-1", "oi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(preview.Extra["debug"]) != `{"latency_ms":42}` || !strings.Contains(string(mustJSON(preview)), `"debug":{"latency_ms":42}`) {
		t.Errorf("diagnóstico extra do preview: %v", preview.Extra)
	}
}

// ── cliente ──────────────────────────────────────────────────────────────────

func TestNewClientEmptyKeyIsArgumentError(t *testing.T) {
	for _, key := range []string{"", "   ", "\t\n"} {
		c, err := NewClient(key)
		if c != nil {
			t.Errorf("NewClient(%q) devolveu cliente", key)
		}
		mustArgErr(t, err)
	}
}

func TestNewClientDefaultsAndOptions(t *testing.T) {
	c, err := NewClient("bf_live_unit")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "https://api.bfocus.com.br" || c.timeout != 30*time.Second || c.maxRetries != 2 {
		t.Errorf("padrões: %v", c)
	}
	c, err = NewClient("bf_live_unit", WithBaseURL("http://127.0.0.1:1/"), WithTimeout(5*time.Second), WithMaxRetries(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "http://127.0.0.1:1" || c.timeout != 5*time.Second || c.maxRetries != 0 {
		t.Errorf("opções: %v", c)
	}
	for name, opt := range map[string]Option{
		"retries negativo":   WithMaxRetries(-1),
		"timeout negativo":   WithTimeout(-time.Second),
		"esquema ftp":        WithBaseURL("ftp://api.bfocus.com.br"),
		"sem esquema":        WithBaseURL("api.bfocus.com.br"),
		"com query":          WithBaseURL("https://api.bfocus.com.br?x=1"),
		"vazia":              WithBaseURL(""),
		"com espaço no host": WithBaseURL("http://a b"),
	} {
		c, err := NewClient("bf_live_unit", opt)
		if c != nil || err == nil {
			t.Errorf("%s: esperava erro", name)
			continue
		}
		mustArgErr(t, err)
	}
}

func TestNewClientMakesNoRequestAndHidesKey(t *testing.T) {
	srv := newFakeServer(t)
	c, err := NewClient("bf_live_segredo", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	mustNoRequests(t, srv)
	for _, s := range []string{fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s", c, c, c, c), c.String()} {
		if strings.Contains(s, "bf_live_segredo") {
			t.Errorf("a chave vazou: %s", s)
		}
	}
	var nilClient *Client
	if nilClient.String() == "" {
		t.Error("String de cliente nil")
	}
}

func TestRequestHeaders(t *testing.T) {
	srv := newFakeServer(t)
	c, _ := newTestClient(t, srv, []fakeResponse{ok(map[string]any{})})
	_, _ = c.Customers.Get(context.Background(), "C1")
	h := srv.recorded()[0].Header
	for name, want := range map[string]string{
		"Authorization":   "Bearer bf_live_unit",
		"Accept":          "application/json",
		"X-Bfocus-Client": "bfocus-go/" + Version,
		"User-Agent":      "bfocus-go/" + Version,
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s: %q, esperado %q", name, got, want)
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(h.Get("X-Request-Id")) {
		t.Errorf("X-Request-Id %q", h.Get("X-Request-Id"))
	}
}

func TestConcurrentUse(t *testing.T) {
	srv := newFakeServer(t)
	const n = 16
	responses := make([]fakeResponse, n)
	for i := range responses {
		responses[i] = ok(customerJSON(i))
	}
	c, _ := newTestClient(t, srv, responses)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := c.Customers.Get(context.Background(), fmt.Sprintf("C%d", i)); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	ids := map[string]bool{}
	for _, r := range srv.recorded() {
		ids[r.Header.Get("X-Request-Id")] = true
	}
	if len(ids) != n {
		t.Errorf("%d X-Request-Ids distintos em %d chamadas", len(ids), n)
	}
}

// ── identidade do widget ────────────────────────────────────────────────────

func TestSignWidgetIdentity(t *testing.T) {
	got, err := SignWidgetIdentity("bf_whs_x", "USR-1", "ACME-1")
	if err != nil || got != "9a15d2527b855a048094ea7826c3b0f16ae5db3035ac3537324d45007ef15141" {
		t.Errorf("assinatura %q, %v", got, err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) {
		t.Errorf("formato %q", got)
	}
	for _, args := range [][3]string{{"", "u", "c"}, {"s", "", "c"}, {"s", "u", ""}} {
		sig, err := SignWidgetIdentity(args[0], args[1], args[2])
		if sig != "" {
			t.Errorf("%v: assinatura com argumento vazio", args)
		}
		mustArgErr(t, err)
	}
}

func TestSignWidgetIdentityV2(t *testing.T) {
	at := time.Unix(1789000000, 999_000_000) // fração de segundo: ts arredonda para baixo
	got, err := SignWidgetIdentityV2At("bf_whs_x", "USR-1", "ACME-1", at)
	if err != nil || got != "v2.1789000000.bfbf2a0390fbb9d65f268899acce2b4d7a2606ba13bb25b453ff3a7971fbd7be" {
		t.Errorf("assinatura %q, %v", got, err)
	}
	// o id do CLIENTE pode ter ':'
	if _, err := SignWidgetIdentityV2At("bf_whs_x", "app-77", "erp:1042", at); err != nil {
		t.Errorf("cliente com ':' devia ser aceito: %v", err)
	}

	before := time.Now().Unix()
	sig, err := SignWidgetIdentityV2("bf_whs_x", "USR-1", "ACME-1")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^v2\.(\d+)\.([0-9a-f]{64})$`).FindStringSubmatch(sig)
	if m == nil {
		t.Fatalf("formato %q", sig)
	}
	ts, _ := strconv.ParseInt(m[1], 10, 64)
	if now := time.Now().Unix(); ts < before-5 || ts > now+5 {
		t.Errorf("ts %d fora de ±5 s de agora (%d)", ts, now)
	}
	if again, _ := SignWidgetIdentityV2At("bf_whs_x", "USR-1", "ACME-1", time.Unix(ts, 0)); again != sig {
		t.Errorf("sem instante ≠ com o mesmo instante: %s × %s", sig, again)
	}

	for name, call := range map[string]func() (string, error){
		"segredo vazio":  func() (string, error) { return SignWidgetIdentityV2("", "u", "c") },
		"usuário vazio":  func() (string, error) { return SignWidgetIdentityV2At("s", "", "c", at) },
		"cliente vazio":  func() (string, error) { return SignWidgetIdentityV2("s", "u", "") },
		"':' no usuário": func() (string, error) { return SignWidgetIdentityV2("s", "app:77", "c") },
		"':' no usuário (At)": func() (string, error) {
			return SignWidgetIdentityV2At("s", "erp:1:u", "c", at)
		},
		"instante negativo": func() (string, error) { return SignWidgetIdentityV2At("s", "u", "c", time.Unix(-1, 0)) },
		"time.Time zero":    func() (string, error) { return SignWidgetIdentityV2At("s", "u", "c", time.Time{}) },
	} {
		sig, err := call()
		if sig != "" {
			t.Errorf("%s: assinatura %q", name, sig)
		}
		mustArgErr(t, err)
	}
	// a v1 não mudou
	if v1, err := SignWidgetIdentity("bf_whs_x", "USR-1", "ACME-1"); err != nil || v1 != "9a15d2527b855a048094ea7826c3b0f16ae5db3035ac3537324d45007ef15141" {
		t.Errorf("v1: %q, %v", v1, err)
	}
}

// ── versão ───────────────────────────────────────────────────────────────────
//
// Go não tem manifesto com versão (a versão publicada é a tag). A constante Version vai no
// X-Bfocus-Client de toda requisição, então é travada contra: o formato SemVer, a linha que
// o scripts/release-sdks.sh reescreve (se o padrão não casar, o bump não acontece e o header
// congela), o header enviado e — no monorepo — o clients/release.json, que o mesmo
// `release-sdks.sh prepare` grava junto do bump. No espelho público, a trava final é o
// publish.yml: a tag vX.Y.Z precisa ser igual a Version.

var releaseScriptVersionRE = regexp.MustCompile(`(?m)^(const Version = ")([^"]+)(")`)

func TestVersionIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Errorf("Version %q não é MAJOR.MINOR.PATCH", Version)
	}
	if clientHeader != "bfocus-go/"+Version {
		t.Errorf("X-Bfocus-Client %q", clientHeader)
	}
}

func TestVersionLineMatchesReleaseScript(t *testing.T) {
	src, err := os.ReadFile("version.go")
	if err != nil {
		t.Fatal(err)
	}
	m := releaseScriptVersionRE.FindAllStringSubmatch(string(src), -1)
	if len(m) != 1 {
		t.Fatalf("o padrão do release-sdks.sh casou %d vezes em version.go (precisa ser 1)", len(m))
	}
	if m[0][2] != Version {
		t.Errorf("version.go tem %q na linha do release, Version é %q", m[0][2], Version)
	}
}

func TestVersionMatchesMonorepoRelease(t *testing.T) {
	data, err := os.ReadFile("../release.json")
	if err != nil {
		t.Skip("clients/release.json só existe no monorepo")
	}
	var release struct {
		Version string   `json:"version"`
		Langs   []string `json:"langs"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		t.Fatal(err)
	}
	if release.Version != Version {
		t.Errorf("clients/release.json declara %q e version.go tem %q — o `release-sdks.sh prepare` bumpa os dois", release.Version, Version)
	}
}

func TestGoModDeclaresModuleAndFloor(t *testing.T) {
	src, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod := string(src)
	if !regexp.MustCompile(`(?m)^module github\.com/bernisoftware/bfocus-go$`).MatchString(mod) {
		t.Error("go.mod precisa declarar module github.com/bernisoftware/bfocus-go")
	}
	if !regexp.MustCompile(`(?m)^go 1\.21$`).MatchString(mod) {
		t.Error("go.mod precisa declarar `go 1.21` (o piso suportado)")
	}
	if regexp.MustCompile(`(?m)^(toolchain|require)\b`).MatchString(mod) {
		t.Error("go.mod sem toolchain e sem dependências (só biblioteca padrão)")
	}
}
