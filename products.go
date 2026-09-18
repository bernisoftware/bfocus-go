package bfocus

import (
	"context"
	"net/http"
)

// ProductsService: catálogo de produtos — client.Products. Escopos: products:read / products:write.
type ProductsService struct{ client *Client }

func productPath(slug string) (string, error) {
	seg, err := segment("slug", slug)
	if err != nil {
		return "", err
	}
	return "/products/" + seg, nil
}

// List lista os produtos — GET /products. params pode ser nil.
func (s *ProductsService) List(ctx context.Context, params *ProductListParams) ([]Product, error) {
	var p ProductListParams
	if params != nil {
		p = *params
	}
	q := new(query).setBool("include_inactive", p.IncludeInactive)
	return callList[Product](ctx, s.client, apiRequest{method: http.MethodGet, path: "/products", query: q})
}

// Get busca o produto pelo slug — GET /products/{slug}.
func (s *ProductsService) Get(ctx context.Context, slug string) (*Product, error) {
	path, err := productPath(slug)
	if err != nil {
		return nil, err
	}
	return callObject[Product](ctx, s.client, apiRequest{method: http.MethodGet, path: path})
}

// Upsert cria ou atualiza o produto — PUT /products/{slug}. Só os campos informados mudam;
// ClearFields limpa.
func (s *ProductsService) Upsert(ctx context.Context, slug string, params *ProductUpsertParams, opts ...RequestOption) (*Product, error) {
	path, err := productPath(slug)
	if err != nil {
		return nil, err
	}
	body, err := encodePatch(params)
	if err != nil {
		return nil, err
	}
	return callObject[Product](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// Archive arquiva o produto (não apaga) — DELETE /products/{slug}. Devolve o produto com
// IsActive false.
func (s *ProductsService) Archive(ctx context.Context, slug string, opts ...RequestOption) (*Product, error) {
	path, err := productPath(slug)
	if err != nil {
		return nil, err
	}
	return callObject[Product](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}
