package bfocus

import (
	"context"
	"net/http"
)

// ReleaseNotesService: release notes por produto — client.ReleaseNotes. Escopos:
// release_notes:read / release_notes:write.
type ReleaseNotesService struct{ client *Client }

func releaseNotePath(productSlug, version string) (string, error) {
	base, err := productPath(productSlug)
	if err != nil {
		return "", err
	}
	seg, err := segment("version", version)
	if err != nil {
		return "", err
	}
	return base + "/release-notes/" + seg, nil
}

// List lista as release notes do produto, uma página — GET /products/{slug}/release-notes.
func (s *ReleaseNotesService) List(ctx context.Context, productSlug string, params *ReleaseNoteListParams) (*Page[ReleaseNote], error) {
	path, err := productPath(productSlug)
	if err != nil {
		return nil, err
	}
	var p ReleaseNoteListParams
	if params != nil {
		p = *params
	}
	q := new(query).setBool("published", p.Published).setInt("page", p.Page).setInt("page_size", p.PageSize)
	return callPage[ReleaseNote](ctx, s.client, apiRequest{method: http.MethodGet, path: path + "/release-notes", query: q})
}

// ListAll percorre TODAS as release notes do produto (PageSize padrão 100).
func (s *ReleaseNotesService) ListAll(productSlug string, params *ReleaseNoteListParams) *Iter[ReleaseNote] {
	if _, err := productPath(productSlug); err != nil {
		return errIter[ReleaseNote](err)
	}
	var base ReleaseNoteListParams
	if params != nil {
		base = *params
	}
	return newIter(base.Page, base.PageSize, func(ctx context.Context, page, size int) (*Page[ReleaseNote], error) {
		p := base
		p.Page, p.PageSize = &page, &size
		return s.List(ctx, productSlug, &p)
	})
}

// Get busca a release note da versão — GET /products/{slug}/release-notes/{version}.
func (s *ReleaseNotesService) Get(ctx context.Context, productSlug, version string) (*ReleaseNote, error) {
	path, err := releaseNotePath(productSlug, version)
	if err != nil {
		return nil, err
	}
	return callObject[ReleaseNote](ctx, s.client, apiRequest{method: http.MethodGet, path: path})
}

// Upsert cria ou atualiza a release note da versão (SemVer X.Y.Z; aceita "v" na frente) —
// PUT /products/{slug}/release-notes/{version}. Com Publish: bfocus.Bool(true), grava e
// publica na mesma chamada (ideal no CI; reexecutar com a mesma versão atualiza a nota).
func (s *ReleaseNotesService) Upsert(ctx context.Context, productSlug, version string, params *ReleaseNoteUpsertParams, opts ...RequestOption) (*ReleaseNote, error) {
	path, err := releaseNotePath(productSlug, version)
	if err != nil {
		return nil, err
	}
	body, err := encodePatch(params)
	if err != nil {
		return nil, err
	}
	return callObject[ReleaseNote](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// Publish publica a release note — POST /products/{slug}/release-notes/{version}/publish.
func (s *ReleaseNotesService) Publish(ctx context.Context, productSlug, version string, opts ...RequestOption) (*ReleaseNote, error) {
	path, err := releaseNotePath(productSlug, version)
	if err != nil {
		return nil, err
	}
	return callObject[ReleaseNote](ctx, s.client, writeRequest(http.MethodPost, path+"/publish", nil, opts))
}
