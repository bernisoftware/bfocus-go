package bfocus

import (
	"context"
	"fmt"
	"net/http"
)

// CustomersService: clientes — client.Customers. Escopos: customers:read / customers:write.
type CustomersService struct {
	client *Client
	// Contacts: contatos dos clientes.
	Contacts *CustomerContactsService
	// Products: produtos vinculados aos clientes.
	Products *CustomerProductsService
	// Interactions: histórico de interações dos clientes.
	Interactions *CustomerInteractionsService
	// Identifiers: identificadores extras dos clientes.
	Identifiers *CustomerIdentifiersService
}

func newCustomersService(c *Client) *CustomersService {
	return &CustomersService{
		client:       c,
		Contacts:     &CustomerContactsService{client: c},
		Products:     &CustomerProductsService{client: c},
		Interactions: &CustomerInteractionsService{client: c},
		Identifiers:  &CustomerIdentifiersService{client: c},
	}
}

func customerPath(externalID string) (string, error) {
	seg, err := segment("externalID", externalID)
	if err != nil {
		return "", err
	}
	return "/customers/" + seg, nil
}

// Upsert cria o cliente externalID (o id dele no SEU sistema; qualquer texto, codificado no
// caminho) ou atualiza se já existe — PUT /customers/{external_id}. Só os campos
// informados mudam; ClearFields limpa. params nil envia {}.
func (s *CustomersService) Upsert(ctx context.Context, externalID string, params *CustomerUpsertParams, opts ...RequestOption) (*Customer, error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	body, err := encodePatch(params)
	if err != nil {
		return nil, err
	}
	return callObject[Customer](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// Get busca o cliente pelo seu external_id — GET /customers/{external_id}. Não achou:
// ErrNotFound com Code CUSTOMER_NOT_FOUND.
func (s *CustomersService) Get(ctx context.Context, externalID string) (*Customer, error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	return callObject[Customer](ctx, s.client, apiRequest{method: http.MethodGet, path: path})
}

// List lista clientes, uma página — GET /customers. params pode ser nil.
func (s *CustomersService) List(ctx context.Context, params *CustomerListParams) (*Page[Customer], error) {
	var p CustomerListParams
	if params != nil {
		p = *params
	}
	q := new(query).setString("q", p.Q).setTime("updated_since", p.UpdatedSince).setInt("page", p.Page).setInt("page_size", p.PageSize)
	return callPage[Customer](ctx, s.client, apiRequest{method: http.MethodGet, path: "/customers", query: q})
}

// ListAll percorre TODOS os clientes, página a página (PageSize padrão 100; Page, se
// informado, é a página inicial). Nada é buscado até o primeiro Next.
func (s *CustomersService) ListAll(params *CustomerListParams) *Iter[Customer] {
	var base CustomerListParams
	if params != nil {
		base = *params
	}
	return newIter(base.Page, base.PageSize, func(ctx context.Context, page, size int) (*Page[Customer], error) {
		p := base
		p.Page, p.PageSize = &page, &size
		return s.List(ctx, &p)
	})
}

// Delete exclui o cliente — DELETE /customers/{external_id}.
func (s *CustomersService) Delete(ctx context.Context, externalID string, opts ...RequestOption) (*DeleteResult, error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	return callObject[DeleteResult](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}

// Batch cria ou atualiza até MaxBatchSize clientes numa chamada — POST /customers/batch.
// Cada item é o corpo do Upsert (só o que veio; ClearFields envia null) mais o ExternalID.
//
// Mais de MaxBatchSize itens: erro de argumento, sem requisição (a SDK NÃO divide — divida
// em fatias de MaxBatchSize; o Index de cada resultado é a posição no lote enviado). Lista
// vazia devolve o resultado zerado sem requisição. Todos os itens são validados antes do
// envio. A falha de um item não desfaz os outros: confira Summary.Error e, em cada
// BatchItemResult, Error/Code; MergedInto preenchido = atualize o id do seu lado.
func (s *CustomersService) Batch(ctx context.Context, items []CustomerBatchItem, opts ...RequestOption) (*BatchResult, error) {
	if err := checkBatchSize("Customers.Batch", len(items)); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return emptyBatchResult(), nil
	}
	encoded := make([][]byte, len(items))
	for i := range items {
		if items[i].ExternalID == "" {
			return nil, argErr(fmt.Sprintf("o item #%d do lote (Customers.Batch) não tem ExternalID", i))
		}
		body, err := encodePatch(&items[i])
		if err != nil {
			return nil, err
		}
		encoded[i] = body
	}
	return postBatch(ctx, s.client, "/customers/batch", encoded, opts)
}

// CustomerContactsService: contatos de um cliente — client.Customers.Contacts.
type CustomerContactsService struct{ client *Client }

func contactPath(externalID, contactExternalID string) (string, error) {
	base, err := customerPath(externalID)
	if err != nil {
		return "", err
	}
	seg, err := segment("contactExternalID", contactExternalID)
	if err != nil {
		return "", err
	}
	return base + "/contacts/" + seg, nil
}

// List lista os contatos do cliente — GET /customers/{external_id}/contacts.
func (s *CustomerContactsService) List(ctx context.Context, externalID string) ([]Contact, error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	return callList[Contact](ctx, s.client, apiRequest{method: http.MethodGet, path: path + "/contacts"})
}

// Upsert cria ou atualiza o contato — PUT /customers/{external_id}/contacts/{contact_external_id}.
// Só os campos informados mudam; ClearFields limpa.
func (s *CustomerContactsService) Upsert(ctx context.Context, externalID, contactExternalID string, params *ContactUpsertParams, opts ...RequestOption) (*Contact, error) {
	path, err := contactPath(externalID, contactExternalID)
	if err != nil {
		return nil, err
	}
	body, err := encodePatch(params)
	if err != nil {
		return nil, err
	}
	return callObject[Contact](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// Delete exclui o contato — DELETE /customers/{external_id}/contacts/{contact_external_id}.
func (s *CustomerContactsService) Delete(ctx context.Context, externalID, contactExternalID string, opts ...RequestOption) (*DeleteResult, error) {
	path, err := contactPath(externalID, contactExternalID)
	if err != nil {
		return nil, err
	}
	return callObject[DeleteResult](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}

// CustomerProductsService: produtos vinculados a um cliente — client.Customers.Products.
type CustomerProductsService struct{ client *Client }

func customerProductPath(externalID, productSlug string) (string, error) {
	base, err := customerPath(externalID)
	if err != nil {
		return "", err
	}
	seg, err := segment("productSlug", productSlug)
	if err != nil {
		return "", err
	}
	return base + "/products/" + seg, nil
}

// List lista os produtos do cliente — GET /customers/{external_id}/products.
func (s *CustomerProductsService) List(ctx context.Context, externalID string) ([]ProductRef, error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	return callList[ProductRef](ctx, s.client, apiRequest{method: http.MethodGet, path: path + "/products"})
}

// Attach vincula o produto ao cliente (idempotente) — PUT /customers/{external_id}/products/{slug}.
func (s *CustomerProductsService) Attach(ctx context.Context, externalID, productSlug string, opts ...RequestOption) (*ProductRef, error) {
	path, err := customerProductPath(externalID, productSlug)
	if err != nil {
		return nil, err
	}
	return callObject[ProductRef](ctx, s.client, writeRequest(http.MethodPut, path, nil, opts))
}

// Detach desvincula o produto do cliente — DELETE /customers/{external_id}/products/{slug}.
// Não vinculado: ErrNotFound com Code PRODUCT_NOT_LINKED.
func (s *CustomerProductsService) Detach(ctx context.Context, externalID, productSlug string, opts ...RequestOption) (*DeleteResult, error) {
	path, err := customerProductPath(externalID, productSlug)
	if err != nil {
		return nil, err
	}
	return callObject[DeleteResult](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}

// CustomerInteractionsService: histórico de interações de um cliente — client.Customers.Interactions.
type CustomerInteractionsService struct{ client *Client }

// List lista as interações do cliente, uma página — GET /customers/{external_id}/interactions.
func (s *CustomerInteractionsService) List(ctx context.Context, externalID string, params *InteractionListParams) (*Page[Interaction], error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	var p InteractionListParams
	if params != nil {
		p = *params
	}
	q := new(query).setInt("page", p.Page).setInt("page_size", p.PageSize)
	return callPage[Interaction](ctx, s.client, apiRequest{method: http.MethodGet, path: path + "/interactions", query: q})
}

// ListAll percorre TODAS as interações do cliente (PageSize padrão 100).
func (s *CustomerInteractionsService) ListAll(externalID string, params *InteractionListParams) *Iter[Interaction] {
	if _, err := customerPath(externalID); err != nil {
		return errIter[Interaction](err)
	}
	var base InteractionListParams
	if params != nil {
		base = *params
	}
	return newIter(base.Page, base.PageSize, func(ctx context.Context, page, size int) (*Page[Interaction], error) {
		p := base
		p.Page, p.PageSize = &page, &size
		return s.List(ctx, externalID, &p)
	})
}

// Create registra uma interação no histórico do cliente — POST /customers/{external_id}/interactions.
// content: texto ou HTML (1–50000). params pode ser nil.
func (s *CustomerInteractionsService) Create(ctx context.Context, externalID, content string, params *InteractionCreateParams, opts ...RequestOption) (*Interaction, error) {
	path, err := customerPath(externalID)
	if err != nil {
		return nil, err
	}
	var p InteractionCreateParams
	if params != nil {
		p = *params
	}
	body, err := marshalJSON(struct {
		Content     string  `json:"content"`
		IsInternal  *bool   `json:"is_internal,omitempty"`
		AuthorEmail *string `json:"author_email,omitempty"`
	}{content, p.IsInternal, p.AuthorEmail})
	if err != nil {
		return nil, err
	}
	return callObject[Interaction](ctx, s.client, writeRequest(http.MethodPost, path+"/interactions", body, opts))
}

// CustomerIdentifiersService: identificadores extras de um cliente —
// client.Customers.Identifiers. Liga o id de OUTRO sistema seu ao mesmo cadastro.
type CustomerIdentifiersService struct{ client *Client }

func customerIdentifierPath(externalID, extraID string) (string, error) {
	base, err := customerPath(externalID)
	if err != nil {
		return "", err
	}
	seg, err := segment("extraID", extraID)
	if err != nil {
		return "", err
	}
	return base + "/identifiers/" + seg, nil
}

// Add liga o identificador extraID ao cliente (idempotente) — PUT
// /customers/{external_id}/identifiers/{extra_id}. params pode ser nil (sem corpo); com
// Label, o corpo é {"label": …}. extraID já é de outro cadastro: ErrConflict com Code
// IDENTIFIER_IN_USE.
func (s *CustomerIdentifiersService) Add(ctx context.Context, externalID, extraID string, params *IdentifierParams, opts ...RequestOption) (*CustomerWithIdentifiers, error) {
	path, err := customerIdentifierPath(externalID, extraID)
	if err != nil {
		return nil, err
	}
	body, err := identifierBody(params)
	if err != nil {
		return nil, err
	}
	return callObject[CustomerWithIdentifiers](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// Remove desliga o identificador extraID do cliente — DELETE
// /customers/{external_id}/identifiers/{extra_id}. Não ligado: ErrNotFound com Code
// IDENTIFIER_NOT_FOUND.
func (s *CustomerIdentifiersService) Remove(ctx context.Context, externalID, extraID string, opts ...RequestOption) (*CustomerWithIdentifiers, error) {
	path, err := customerIdentifierPath(externalID, extraID)
	if err != nil {
		return nil, err
	}
	return callObject[CustomerWithIdentifiers](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}
