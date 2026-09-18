package bfocus

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// KBBatchSize é o máximo de artigos por requisição de lote (limite da API). BatchUpsert
// divide listas maiores sozinho.
const KBBatchSize = 100

// KBService: base de conhecimento — client.KB. Escopos: kb:read / kb:write. Exige o módulo
// de Atendimento: sem ele, ErrPermissionDenied com Code MODULE_NOT_CONTRACTED.
type KBService struct {
	client *Client
	// Articles: artigos.
	Articles *KBArticlesService
}

func newKBService(c *Client) *KBService {
	return &KBService{client: c, Articles: &KBArticlesService{client: c}}
}

// Search busca na base de conhecimento — GET /kb/search; é a mesma busca que os agentes de
// IA usam. q: texto buscado (1–500). params pode ser nil.
func (s *KBService) Search(ctx context.Context, q string, params *KBSearchParams) ([]KBSearchHit, error) {
	var p KBSearchParams
	if params != nil {
		p = *params
	}
	qs := new(query).set("q", q).setString("product", p.Product).setInt("limit", p.Limit)
	return callList[KBSearchHit](ctx, s.client, apiRequest{method: http.MethodGet, path: "/kb/search", query: qs})
}

// KBArticlesService: artigos da base de conhecimento — client.KB.Articles.
type KBArticlesService struct{ client *Client }

func articlePath(externalID string) (string, error) {
	seg, err := kbSegment("externalID", externalID)
	if err != nil {
		return "", err
	}
	return "/kb/articles/" + seg, nil
}

// List lista artigos, uma página — GET /kb/articles. Os itens vêm sem corpo
// (KBArticleSummary). params pode ser nil.
func (s *KBArticlesService) List(ctx context.Context, params *KBArticleListParams) (*Page[KBArticleSummary], error) {
	var p KBArticleListParams
	if params != nil {
		p = *params
	}
	q := new(query).
		setString("product", p.Product).
		setString("status", p.Status).
		setString("q", p.Q).
		setTime("updated_since", p.UpdatedSince).
		setInt("page", p.Page).
		setInt("page_size", p.PageSize)
	return callPage[KBArticleSummary](ctx, s.client, apiRequest{method: http.MethodGet, path: "/kb/articles", query: q})
}

// ListAll percorre TODOS os artigos, sem corpo (PageSize padrão 100).
func (s *KBArticlesService) ListAll(params *KBArticleListParams) *Iter[KBArticleSummary] {
	var base KBArticleListParams
	if params != nil {
		base = *params
	}
	return newIter(base.Page, base.PageSize, func(ctx context.Context, page, size int) (*Page[KBArticleSummary], error) {
		p := base
		p.Page, p.PageSize = &page, &size
		return s.List(ctx, &p)
	})
}

// Get busca o artigo completo (com BodyHTML) pelo seu external_id — GET /kb/articles/{external_id}.
func (s *KBArticlesService) Get(ctx context.Context, externalID string) (*KBArticle, error) {
	path, err := articlePath(externalID)
	if err != nil {
		return nil, err
	}
	return callObject[KBArticle](ctx, s.client, apiRequest{method: http.MethodGet, path: path})
}

// Upsert cria ou atualiza o artigo — PUT /kb/articles/{external_id} (sem "/" no id; use
// ":" para hierarquia). Só os campos informados mudam; ClearFields: []string{"product"}
// torna o artigo global. Corpo vazio: ErrConflict com Code KB_ARTICLE_EMPTY.
func (s *KBArticlesService) Upsert(ctx context.Context, externalID string, params *KBArticleUpsertParams, opts ...RequestOption) (*KBArticle, error) {
	path, err := articlePath(externalID)
	if err != nil {
		return nil, err
	}
	body, err := encodePatch(params)
	if err != nil {
		return nil, err
	}
	return callObject[KBArticle](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// BatchUpsert cria ou atualiza QUALQUER quantidade de artigos — POST /kb/articles/batch: a
// SDK divide em lotes de KBBatchSize, envia em sequência e devolve UM resultado agregado
// (Results na ordem enviada, contadores somados). Lista vazia devolve o resultado zerado
// sem requisição.
//
// Todos os itens são validados antes da primeira requisição (ExternalID obrigatório e sem
// "/"; ClearFields). A falha de um artigo não derruba o lote (veja KBBatchItemResult.Error);
// um erro HTTP interrompe os lotes seguintes e é devolvido JUNTO com o agregado dos lotes
// já gravados — rodar de novo é seguro.
//
// Com WithIdempotencyKey, o 1º lote usa a chave como veio e os seguintes "<chave>:2",
// "<chave>:3"…; sem ela, cada lote gera a sua.
func (s *KBArticlesService) BatchUpsert(ctx context.Context, articles []KBBatchArticle, opts ...RequestOption) (*KBBatchResult, error) {
	encoded := make([][]byte, len(articles))
	for i := range articles {
		a := &articles[i]
		if a.ExternalID == "" {
			return nil, argErr(fmt.Sprintf("o artigo #%d do lote não tem ExternalID", i))
		}
		if strings.Contains(a.ExternalID, "/") {
			return nil, argErr(fmt.Sprintf("o ExternalID %q (artigo #%d) não aceita '/' — use ':' para hierarquia", a.ExternalID, i))
		}
		body, err := encodePatch(a)
		if err != nil {
			return nil, err
		}
		encoded[i] = body
	}

	result := &KBBatchResult{Results: make([]KBBatchItemResult, 0, len(articles))}
	key := requestOpts(opts).idempotencyKey
	for start, chunk := 0, 1; start < len(encoded); start, chunk = start+KBBatchSize, chunk+1 {
		end := start + KBBatchSize
		if end > len(encoded) {
			end = len(encoded)
		}
		var body bytes.Buffer
		body.WriteString(`{"articles":[`)
		body.Write(bytes.Join(encoded[start:end], []byte(",")))
		body.WriteString(`]}`)

		r := apiRequest{method: http.MethodPost, path: "/kb/articles/batch", body: body.Bytes()}
		if key != "" {
			r.idempotencyKey = key
			if chunk > 1 {
				r.idempotencyKey = key + ":" + strconv.Itoa(chunk)
			}
		}
		part, err := callObject[KBBatchResult](ctx, s.client, r)
		if err != nil {
			return result, err
		}
		result.Results = append(result.Results, part.Results...)
		result.Created += part.Created
		result.Updated += part.Updated
		result.Unchanged += part.Unchanged
		result.Failed += part.Failed
	}
	return result, nil
}

// Publish publica o artigo — POST /kb/articles/{external_id}/publish.
func (s *KBArticlesService) Publish(ctx context.Context, externalID string, opts ...RequestOption) (*KBArticle, error) {
	path, err := articlePath(externalID)
	if err != nil {
		return nil, err
	}
	return callObject[KBArticle](ctx, s.client, writeRequest(http.MethodPost, path+"/publish", nil, opts))
}

// Unpublish volta o artigo para rascunho — POST /kb/articles/{external_id}/unpublish.
func (s *KBArticlesService) Unpublish(ctx context.Context, externalID string, opts ...RequestOption) (*KBArticle, error) {
	path, err := articlePath(externalID)
	if err != nil {
		return nil, err
	}
	return callObject[KBArticle](ctx, s.client, writeRequest(http.MethodPost, path+"/unpublish", nil, opts))
}

// Delete exclui o artigo — DELETE /kb/articles/{external_id}.
func (s *KBArticlesService) Delete(ctx context.Context, externalID string, opts ...RequestOption) (*DeleteResult, error) {
	path, err := articlePath(externalID)
	if err != nil {
		return nil, err
	}
	return callObject[DeleteResult](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}
