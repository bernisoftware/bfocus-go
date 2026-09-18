package bfocus

import (
	"context"
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
}

func newCustomersService(c *Client) *CustomersService {
	return &CustomersService{
		client:       c,
		Contacts:     &CustomerContactsService{client: c},
		Products:     &CustomerProductsService{client: c},
		Interactions: &CustomerInteractionsService{client: c},
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
