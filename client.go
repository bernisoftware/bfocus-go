package bfocus

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL é a API de produção, usada quando WithBaseURL não é informado.
	DefaultBaseURL = "https://api.bfocus.com.br"
	// DefaultTimeout é o tempo limite de cada tentativa.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxRetries é o número padrão de novas tentativas além da primeira.
	DefaultMaxRetries = 2
)

// Client é o cliente da API pública do bFocus. Crie um por chave de API com NewClient e
// reutilize-o: é seguro para uso concorrente por várias goroutines.
type Client struct {
	// Customers: clientes (e seus contatos, produtos vinculados e interações).
	Customers *CustomersService
	// People: pessoas dos clientes (acesso ao widget/portal) e seus identificadores.
	People *PeopleService
	// Products: catálogo de produtos.
	Products *ProductsService
	// ReleaseNotes: release notes por produto.
	ReleaseNotes *ReleaseNotesService
	// KB: base de conhecimento (artigos e busca).
	KB *KBService
	// AIAgents: agentes de IA.
	AIAgents *AIAgentsService

	apiKey     string
	baseURL    string
	timeout    time.Duration
	maxRetries int
	httpClient *http.Client

	// sleep espera entre tentativas. Os testes do pacote trocam por uma que só registra a
	// espera — a suíte de conformidade não pode dormir de verdade.
	sleep func(ctx context.Context, d time.Duration) error
}

// Option configura o Client em NewClient.
type Option func(*Client)

// WithBaseURL troca a URL da API (padrão: DefaultBaseURL). Em dev: "http://localhost:8000".
// A barra final é removida.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/") }
}

// WithTimeout define o tempo limite de CADA tentativa (padrão: DefaultTimeout). 0 desliga
// o limite por tentativa (vale só o ctx da chamada). Negativo faz NewClient devolver erro.
// Para um limite da chamada inteira (todas as tentativas), use o ctx.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithMaxRetries define quantas novas tentativas fazer além da primeira (padrão:
// DefaultMaxRetries). 0 desliga. Negativo faz NewClient devolver erro.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithHTTPClient usa o seu *http.Client (proxy, transporte próprio, instrumentação…). O
// tempo limite por tentativa de WithTimeout continua valendo, somado ao Timeout do seu
// cliente, se houver.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient cria o cliente para a chave de API (crie em Integrações → Chaves de API). Não
// faz chamada de rede.
//
// Chave vazia (ou só espaços), URL base inválida ou opção negativa devolvem, na hora, um
// erro que satisfaz errors.Is(err, ErrInvalidArgument) — nunca um *Error. É erro, e não
// pânico, porque a chave costuma vir do ambiente (os.Getenv), e variável ausente é
// configuração, não bug.
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, argErr("a chave de API do bFocus não pode ser vazia (bfocus.NewClient)")
	}
	c := &Client{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		timeout:    DefaultTimeout,
		maxRetries: DefaultMaxRetries,
		sleep:      sleepContext,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if u, err := url.Parse(c.baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, argErr(fmt.Sprintf("URL base inválida: %q — use uma URL absoluta http(s), ex.: %s (bfocus.WithBaseURL)", c.baseURL, DefaultBaseURL))
	}
	if c.timeout < 0 {
		return nil, argErr("o tempo limite não pode ser negativo (bfocus.WithTimeout)")
	}
	if c.maxRetries < 0 {
		return nil, argErr("o número de novas tentativas não pode ser negativo (bfocus.WithMaxRetries)")
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}

	c.Customers = newCustomersService(c)
	c.People = newPeopleService(c)
	c.Products = &ProductsService{client: c}
	c.ReleaseNotes = &ReleaseNotesService{client: c}
	c.KB = newKBService(c)
	c.AIAgents = &AIAgentsService{client: c}
	return c, nil
}

// String descreve o cliente SEM a chave de API — seguro para log (fmt.Print(client),
// %v, %+v).
func (c *Client) String() string {
	if c == nil {
		return "bfocus.Client(nil)"
	}
	return fmt.Sprintf("bfocus.Client{baseURL: %q, timeout: %s, maxRetries: %d}", c.baseURL, c.timeout, c.maxRetries)
}

// GoString é o %#v — também sem a chave de API.
func (c *Client) GoString() string { return c.String() }
