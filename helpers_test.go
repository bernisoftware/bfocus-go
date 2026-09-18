package bfocus

// Apoio dos testes: servidor HTTP falso (net/http/httptest) que responde, em ordem, as
// respostas enfileiradas e grava cada requisição exatamente como chegou (caminho cru da
// linha de requisição, query, headers, corpo); e um cliente com as esperas de retry
// desligadas — a suíte não dorme de verdade.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeResponse é uma resposta enfileirada (o mesmo formato do cases.json): Body null ou
// ausente = sem corpo; string JSON = corpo texto (text/plain); o resto vai como JSON.
type fakeResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

// recorded é uma requisição recebida pelo servidor falso.
type recorded struct {
	Method   string
	Path     string // caminho CRU, como veio na linha de requisição (sem decodificar)
	RawQuery string
	Query    url.Values
	Header   http.Header
	Body     []byte
}

func (r recorded) has(header string) bool {
	_, ok := r.Header[http.CanonicalHeaderKey(header)]
	return ok
}

func (r recorded) jsonBody(t *testing.T) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		t.Fatalf("corpo não é JSON (%v): %q", err, r.Body)
	}
	return v
}

type fakeServer struct {
	*httptest.Server
	mu        sync.Mutex
	responses []fakeResponse
	requests  []recorded
	// block, quando não nil, segura cada requisição até o cliente desistir (timeout ou
	// cancelamento) — e avisa em arrived que ela chegou.
	block   bool
	arrived chan struct{}
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fs := &fakeServer{arrived: make(chan struct{}, 64)}
	fs.Server = httptest.NewServer(http.HandlerFunc(fs.handle))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path, rawQuery, _ := strings.Cut(r.RequestURI, "?")
	query, _ := url.ParseQuery(rawQuery)

	fs.mu.Lock()
	fs.requests = append(fs.requests, recorded{
		Method: r.Method, Path: path, RawQuery: rawQuery, Query: query,
		Header: r.Header.Clone(), Body: body,
	})
	block := fs.block
	var resp *fakeResponse
	if !block && len(fs.responses) > 0 {
		resp = &fs.responses[0]
		fs.responses = fs.responses[1:]
	}
	fs.mu.Unlock()

	if block {
		fs.arrived <- struct{}{}
		<-r.Context().Done() // só termina quando o cliente desiste
		return
	}
	if resp == nil {
		// Requisição a mais: 418 não é repetido pela SDK e o teste acusa pela contagem.
		resp = &fakeResponse{Status: 418, Body: json.RawMessage(
			`{"code":418,"data":null,"message":"UNEXPECTED_REQUEST","error":"UNEXPECTED_REQUEST"}`)}
	}

	var payload []byte
	contentType := ""
	switch raw := bytes.TrimSpace(resp.Body); {
	case len(raw) == 0 || string(raw) == "null":
	case raw[0] == '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		payload, contentType = []byte(s), "text/plain; charset=utf-8"
	default:
		payload, contentType = raw, "application/json"
	}
	h := w.Header()
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	for k, v := range resp.Headers {
		h.Set(k, v)
	}
	h.Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(resp.Status)
	_, _ = w.Write(payload)
}

// reset enfileira as respostas e zera as requisições gravadas.
func (fs *fakeServer) reset(responses ...fakeResponse) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.responses = append([]fakeResponse(nil), responses...)
	fs.requests = nil
}

func (fs *fakeServer) recorded() []recorded {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]recorded(nil), fs.requests...)
}

// sleepLog substitui a espera entre tentativas: só registra (não dorme).
type sleepLog struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (s *sleepLog) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.waits = append(s.waits, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *sleepLog) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

// newTestClient cria um cliente apontado para o servidor falso, com as esperas desligadas.
func newTestClient(t *testing.T, fs *fakeServer, responses []fakeResponse, opts ...Option) (*Client, *sleepLog) {
	t.Helper()
	fs.reset(responses...)
	return newClientAt(t, fs.URL, opts...)
}

func newClientAt(t *testing.T, baseURL string, opts ...Option) (*Client, *sleepLog) {
	t.Helper()
	c, err := NewClient("bf_live_unit", append([]Option{WithBaseURL(baseURL)}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	log := &sleepLog{}
	c.sleep = log.sleep
	return c, log
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ok: resposta 200 no envelope da API.
func ok(data any) fakeResponse {
	return fakeResponse{Status: 200, Body: mustJSON(map[string]any{
		"code": 200, "data": data, "message": "Executado com sucesso",
	})}
}

// okPage: resposta 200 paginada.
func okPage(data any, page, pageSize, total, pages int) fakeResponse {
	return fakeResponse{Status: 200, Body: mustJSON(map[string]any{
		"code": 200, "data": data, "message": "Executado com sucesso",
		"pagination": map[string]int{"page": page, "page_size": pageSize, "total": total, "pages": pages},
	})}
}

// fail: resposta de erro no envelope da API.
func fail(status int, code string, headers map[string]string) fakeResponse {
	return fakeResponse{Status: status, Headers: headers, Body: mustJSON(map[string]any{
		"code": status, "data": nil, "message": code, "error": code,
		"validation": map[string]any{}, "request_id": "req-unit",
	})}
}

// text: resposta com corpo texto (não JSON).
func text(status int, body string, headers map[string]string) fakeResponse {
	return fakeResponse{Status: status, Headers: headers, Body: mustJSON(body)}
}

// raw: resposta com o corpo JSON exatamente como escrito.
func raw(status int, body string, headers map[string]string) fakeResponse {
	return fakeResponse{Status: status, Headers: headers, Body: json.RawMessage(body)}
}

// apiErr exige que err seja um *Error e o devolve.
func apiErr(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("esperava *bfocus.Error, veio %T: %v", err, err)
	}
	return e
}
