package bfocus

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiPrefix     = "/api/v1/integration"
	clientHeader  = "bfocus-go/" + Version
	maxRetryAfter = 60 * time.Second
)

// apiRequest é uma chamada lógica: método, caminho (relativo ao prefixo, já codificado),
// query, corpo (nil = sem corpo) e a Idempotency-Key do usuário (opcional).
type apiRequest struct {
	method         string
	path           string
	query          *query
	body           []byte
	idempotencyKey string
}

func writeRequest(method, path string, body []byte, opts []RequestOption) apiRequest {
	return apiRequest{method: method, path: path, body: body, idempotencyKey: requestOpts(opts).idempotencyKey}
}

// envelope é a resposta de sucesso {"code", "data", "message"[, "pagination"]}.
type envelope struct {
	status     int
	requestID  string
	data       json.RawMessage
	pagination json.RawMessage
}

func (e *envelope) invalid(err error) *Error {
	return invalidResponse(e.status, e.requestID, "formato inesperado em 'data': "+err.Error(), err)
}

// send faz a chamada lógica com novas tentativas. X-Request-Id e Idempotency-Key são
// gerados UMA vez e repetidos em toda tentativa — é o que torna a repetição segura (a API
// devolve a resposta original com Idempotent-Replayed: true).
func (c *Client) send(ctx context.Context, r apiRequest) (*envelope, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := c.baseURL + apiPrefix + r.path + r.query.encode()
	requestID := newRequestID()
	idempotencyKey := ""
	if isWrite(r.method) {
		idempotencyKey = r.idempotencyKey
		if idempotencyKey == "" {
			idempotencyKey = newUUID()
		}
	}

	for attempt := 0; ; attempt++ {
		env, wait, err := c.attempt(ctx, r, endpoint, requestID, idempotencyKey)
		if err == nil {
			return env, nil
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) || attempt >= c.maxRetries || !apiErr.retryable() {
			return nil, err
		}
		if sleepErr := c.sleep(ctx, backoff(attempt, wait)); sleepErr != nil {
			return nil, sleepErr
		}
	}
}

// attempt faz UMA tentativa. Devolve o envelope (2xx) ou o erro, mais a espera pedida
// pelo Retry-After (nil quando não houver).
func (c *Client) attempt(ctx context.Context, r apiRequest, endpoint, requestID, idempotencyKey string) (*envelope, *time.Duration, error) {
	actx, cancel := ctx, context.CancelFunc(func() {})
	if c.timeout > 0 {
		actx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(actx, r.method, endpoint, body)
	if err != nil {
		return nil, nil, fmt.Errorf("bfocus: montar a requisição %s %s: %w", r.method, r.path, err)
	}
	h := req.Header
	h.Set("Authorization", "Bearer "+c.apiKey)
	h.Set("Accept", "application/json")
	h.Set("X-Bfocus-Client", clientHeader)
	h.Set("User-Agent", clientHeader)
	h.Set("X-Request-Id", requestID)
	if idempotencyKey != "" {
		h.Set("Idempotency-Key", idempotencyKey)
	}
	if r.body != nil {
		h.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, c.transportError(ctx, actx, r, requestID, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, c.transportError(ctx, actx, r, requestID, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// request_id: corpo → header X-Request-Id → o que a SDK enviou (a API ecoa o do cliente).
		fallback := requestID
		if id := strings.TrimSpace(resp.Header.Get("X-Request-Id")); id != "" {
			fallback = id
		}
		env, err := parseEnvelope(raw, resp.StatusCode, fallback)
		return env, nil, err
	}
	wait := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	return nil, wait, errorFromResponse(resp.StatusCode, raw, resp.Header, wait, requestID)
}

// transportError: cancelamento pelo chamador devolve ctx.Err() (não é erro de rede e não
// gera nova tentativa); o resto — conexão recusada, tempo esgotado da tentativa… — vira
// *Error de rede (Status 0, Code NETWORK_ERROR).
func (c *Client) transportError(ctx, attemptCtx context.Context, r apiRequest, requestID string, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var netErr net.Error
	var msg string
	switch {
	case errors.Is(attemptCtx.Err(), context.DeadlineExceeded):
		msg = fmt.Sprintf("%s: tempo esgotado após %s em %s %s", CodeNetworkError, c.timeout, r.method, r.path)
	case errors.As(cause, &netErr) && netErr.Timeout():
		msg = fmt.Sprintf("%s: tempo esgotado em %s %s: %v", CodeNetworkError, r.method, r.path, cause)
	default:
		msg = fmt.Sprintf("%s: falha de conexão em %s %s: %v", CodeNetworkError, r.method, r.path, cause)
	}
	return &Error{
		Type:      ErrorTypeNetwork,
		Code:      CodeNetworkError,
		Status:    0,
		RequestID: requestID,
		Message:   msg + " (request_id " + requestID + ")",
		Err:       cause,
	}
}

func parseEnvelope(raw []byte, status int, requestID string) (*envelope, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, invalidResponse(status, requestID, "o corpo da resposta não é um objeto JSON", err)
	}
	if root == nil {
		return nil, invalidResponse(status, requestID, "o corpo da resposta não é um objeto JSON", nil)
	}
	if id := jsonString(root["request_id"]); id != "" {
		requestID = id
	}
	data, ok := root["data"]
	if !ok || isJSONNull(data) {
		return nil, invalidResponse(status, requestID, "a resposta não trouxe 'data'", nil)
	}
	return &envelope{status: status, requestID: requestID, data: data, pagination: root["pagination"]}, nil
}

// callObject faz a chamada e decodifica `data` num objeto.
func callObject[T any](ctx context.Context, c *Client, r apiRequest) (*T, error) {
	env, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	out := new(T)
	if err := json.Unmarshal(env.data, out); err != nil {
		return nil, env.invalid(err)
	}
	return out, nil
}

// callList faz a chamada e decodifica `data` numa lista (nunca nil).
func callList[T any](ctx context.Context, c *Client, r apiRequest) ([]T, error) {
	env, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	out := []T{}
	if err := json.Unmarshal(env.data, &out); err != nil {
		return nil, env.invalid(err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// callPage faz a chamada e monta a Page com `data` + `pagination` (sem `pagination`, a
// página única com todos os itens).
func callPage[T any](ctx context.Context, c *Client, r apiRequest) (*Page[T], error) {
	env, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	items := []T{}
	if err := json.Unmarshal(env.data, &items); err != nil {
		return nil, env.invalid(err)
	}
	if items == nil {
		items = []T{}
	}
	p := &Page[T]{Items: items, Page: 1, PageSize: len(items), Total: len(items), Pages: 1}
	if len(env.pagination) > 0 {
		var pg struct {
			Page     *int `json:"page"`
			PageSize *int `json:"page_size"`
			Total    *int `json:"total"`
			Pages    *int `json:"pages"`
		}
		if json.Unmarshal(env.pagination, &pg) == nil {
			if pg.Page != nil {
				p.Page = *pg.Page
			}
			if pg.PageSize != nil {
				p.PageSize = *pg.PageSize
			}
			if pg.Total != nil {
				p.Total = *pg.Total
			}
			if pg.Pages != nil {
				p.Pages = *pg.Pages
			}
		}
	}
	return p, nil
}

func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// backoff é a espera antes da nova tentativa `attempt` (0 = a primeira): o Retry-After
// quando houver (teto de 60 s); senão min(8, 0.5 × 2^attempt) s + até 25% de variação.
func backoff(attempt int, retryAfter *time.Duration) time.Duration {
	if retryAfter != nil {
		d := *retryAfter
		if d < 0 {
			return 0
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	secs := math.Min(8, 0.5*math.Pow(2, float64(attempt)))
	secs += rand.Float64() * 0.25 * secs
	return time.Duration(secs * float64(time.Second))
}

// parseRetryAfter lê o Retry-After em segundos (inteiro ou decimal) ou data HTTP.
func parseRetryAfter(raw string, now time.Time) *time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if secs, err := strconv.ParseFloat(raw, 64); err == nil {
		if secs < 0 || math.IsNaN(secs) || math.IsInf(secs, 0) {
			return nil
		}
		d := time.Duration(math.Min(secs, 1e9) * float64(time.Second))
		return &d
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return &d
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// newRequestID: uuid4 em hex (32 caracteres).
func newRequestID() string {
	b := uuid4()
	return hex.EncodeToString(b[:])
}

// newUUID: uuid4 no formato canônico 8-4-4-4-12.
func newUUID() string {
	b := uuid4()
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func uuid4() (b [16]byte) {
	if _, err := crand.Read(b[:]); err != nil {
		for i := range b {
			b[i] = byte(rand.Intn(256))
		}
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return b
}

func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func jsonString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
