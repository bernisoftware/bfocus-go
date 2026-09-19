package bfocus

import "time"

// String devolve um ponteiro para v (para os campos opcionais dos parâmetros).
func String(v string) *string { return &v }

// Bool devolve um ponteiro para v.
func Bool(v bool) *bool { return &v }

// Int devolve um ponteiro para v.
func Int(v int) *int { return &v }

// Time devolve um ponteiro para v (ex.: UpdatedSince).
func Time(v time.Time) *time.Time { return &v }

// RequestOption é uma opção por chamada, aceita pelos métodos de escrita.
type RequestOption func(*requestOptions)

type requestOptions struct {
	idempotencyKey string
}

// WithIdempotencyKey usa a SUA Idempotency-Key em vez da gerada pela SDK — para
// deduplicar também um reenvio feito pela sua aplicação (um job reexecutado). Em
// KB.Articles.BatchUpsert, o 1º lote usa a chave como veio e os seguintes "<chave>:2",
// "<chave>:3"…
func WithIdempotencyKey(key string) RequestOption {
	return func(o *requestOptions) { o.idempotencyKey = key }
}

func requestOpts(opts []RequestOption) requestOptions {
	var o requestOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// ── clientes ─────────────────────────────────────────────────────────────────

// CustomerUpsertParams é o corpo de Customers.Upsert (PUT /customers/{external_id}).
// Parcial: campo nil é omitido (fica como está); para limpar (enviar null), ponha o nome
// em ClearFields.
type CustomerUpsertParams struct {
	// Name: nome (até 500).
	Name *string `json:"name,omitempty"`
	// Document: CPF/CNPJ ou outro documento (até 50).
	Document *string `json:"document,omitempty"`
	// Email: e-mail (até 255).
	Email *string `json:"email,omitempty"`
	// Phone: telefone (até 50).
	Phone *string `json:"phone,omitempty"`
	// Website: site (até 500).
	Website *string `json:"website,omitempty"`
	// Notes: observações.
	Notes *string `json:"notes,omitempty"`
	// CustomFields: campos personalizados. Quando enviada (não nil), a lista SUBSTITUI a
	// atual — []bfocus.CustomFieldInput{} apaga todos.
	CustomFields []CustomFieldInput `json:"custom_fields,omitempty"`
	// ClearFields: campos a LIMPAR (enviados como null), pelo nome JSON ("phone") ou Go
	// ("Phone").
	ClearFields []string `json:"-"`
}

// CustomFieldInput é um campo personalizado enviado em CustomerUpsertParams.CustomFields.
// Campos vazios não vão no corpo (a API assume label "" e type "text").
type CustomFieldInput struct {
	// Key: chave estável do campo (1–80). Obrigatória.
	Key string `json:"key"`
	// Label: rótulo exibido (até 200).
	Label string `json:"label,omitempty"`
	// Type: text (padrão), textarea, email, phone, url, number, date, datetime, bool,
	// select ou file.
	Type string `json:"type,omitempty"`
	// Value: valor (qualquer tipo JSON: string, número, bool…).
	Value any `json:"value,omitempty"`
	// Options: opções, para o tipo select.
	Options []string `json:"options,omitempty"`
}

// CustomerListParams filtra Customers.List e Customers.ListAll.
type CustomerListParams struct {
	// Q: busca por nome, documento, e-mail…
	Q *string `json:"q,omitempty"`
	// UpdatedSince: só os alterados a partir deste instante (enviado em UTC) — ideal para
	// sincronização incremental.
	UpdatedSince *time.Time `json:"updated_since,omitempty"`
	// Page: página (a partir de 1). Em ListAll, a página inicial (padrão 1).
	Page *int `json:"page,omitempty"`
	// PageSize: itens por página (1–200; padrão da API 50; em ListAll, 100).
	PageSize *int `json:"page_size,omitempty"`
}

// ContactUpsertParams é o corpo de Customers.Contacts.Upsert. Parcial: campo nil é
// omitido; para limpar, use ClearFields.
type ContactUpsertParams struct {
	// Name: nome (1–255; obrigatório ao criar).
	Name *string `json:"name,omitempty"`
	// Role: cargo/função (até 120).
	Role *string `json:"role,omitempty"`
	// Email: e-mail válido.
	Email *string `json:"email,omitempty"`
	// Phone: telefone (até 50).
	Phone *string `json:"phone,omitempty"`
	// Notes: observações.
	Notes *string `json:"notes,omitempty"`
	// IsPrimary: contato principal do cliente.
	IsPrimary *bool `json:"is_primary,omitempty"`
	// ClearFields: campos a LIMPAR (enviados como null).
	ClearFields []string `json:"-"`
}

// CustomerBatchItem é um item de Customers.Batch: os campos de CustomerUpsertParams mais o
// ExternalID do cliente (obrigatório no item). Cada item vai no corpo exatamente como o
// corpo do Upsert (só o que veio; ClearFields envia null) + "external_id".
type CustomerBatchItem struct {
	// ExternalID: id do cliente no seu sistema (obrigatório; qualquer texto — ex.:
	// "erp-1042").
	ExternalID string `json:"external_id" bfocus:"noclear"`
	// Name: nome (até 500; obrigatório ao criar).
	Name *string `json:"name,omitempty"`
	// Document: CPF/CNPJ ou outro documento (até 50).
	Document *string `json:"document,omitempty"`
	// Email: e-mail (até 255).
	Email *string `json:"email,omitempty"`
	// Phone: telefone (até 50).
	Phone *string `json:"phone,omitempty"`
	// Website: site (até 500).
	Website *string `json:"website,omitempty"`
	// Notes: observações.
	Notes *string `json:"notes,omitempty"`
	// CustomFields: campos personalizados. Quando enviada (não nil), a lista SUBSTITUI a
	// atual.
	CustomFields []CustomFieldInput `json:"custom_fields,omitempty"`
	// ClearFields: campos a LIMPAR (enviados como null).
	ClearFields []string `json:"-"`
}

// InteractionListParams pagina Customers.Interactions.List e ListAll.
type InteractionListParams struct {
	// Page: página (a partir de 1). Em ListAll, a página inicial (padrão 1).
	Page *int `json:"page,omitempty"`
	// PageSize: itens por página (1–200; em ListAll, padrão 100).
	PageSize *int `json:"page_size,omitempty"`
}

// InteractionCreateParams são os campos opcionais de Customers.Interactions.Create.
type InteractionCreateParams struct {
	// IsInternal: interna, não visível ao cliente (padrão da API: true).
	IsInternal *bool `json:"is_internal,omitempty"`
	// AuthorEmail: e-mail de um usuário do bFocus para constar como autor.
	AuthorEmail *string `json:"author_email,omitempty"`
}

// ── pessoas ──────────────────────────────────────────────────────────────────

// PersonParams é o corpo de People.Upsert (vai dentro de {"person": …}). Parcial: campo nil
// é omitido (fica como está); para enviar null, ponha o nome em ClearFields (a API trata
// null como "não altera").
type PersonParams struct {
	// Name: nome (obrigatório ao criar).
	Name *string `json:"name,omitempty"`
	// Email: e-mail. Acha a pessoa que já chegou por e-mail/outro sistema e a adota, sem
	// duplicar.
	Email *string `json:"email,omitempty"`
	// Phone: telefone (também identifica a pessoa já cadastrada).
	Phone *string `json:"phone,omitempty"`
	// Role: cargo/função no cliente (ex.: "Financeiro").
	Role *string `json:"role,omitempty"`
	// Access: acesso ao widget/portal (padrão ao criar: true). false retira o acesso;
	// true devolve.
	Access *bool `json:"access,omitempty"`
	// IsPrimary: contato principal do cliente.
	IsPrimary *bool `json:"is_primary,omitempty"`
	// ExtraEmails: e-mails adicionais (somam aos que já existem).
	ExtraEmails []string `json:"extra_emails,omitempty"`
	// ExtraPhones: telefones adicionais (somam aos que já existem).
	ExtraPhones []string `json:"extra_phones,omitempty"`
	// ClearFields: campos a enviar como null.
	ClearFields []string `json:"-"`
}

// PersonBatchItem é um item de People.Batch: o cliente, o id da pessoa e os campos de
// PersonParams. No fio vira {"customer_external_id": …, "person": {"external_id": …, …}}.
type PersonBatchItem struct {
	// CustomerExternalID: external_id do cliente a que a pessoa pertence (obrigatório).
	CustomerExternalID string `json:"customer_external_id" bfocus:"skip"`
	// ExternalID: id da pessoa no seu sistema (obrigatório) — o mesmo user.externalId
	// assinado no widget.
	ExternalID string `json:"external_id" bfocus:"noclear"`
	// Name: nome (obrigatório ao criar).
	Name *string `json:"name,omitempty"`
	// Email: e-mail (acha a pessoa já cadastrada, sem duplicar).
	Email *string `json:"email,omitempty"`
	// Phone: telefone.
	Phone *string `json:"phone,omitempty"`
	// Role: cargo/função no cliente.
	Role *string `json:"role,omitempty"`
	// Access: acesso ao widget/portal (padrão ao criar: true).
	Access *bool `json:"access,omitempty"`
	// IsPrimary: contato principal do cliente.
	IsPrimary *bool `json:"is_primary,omitempty"`
	// ExtraEmails: e-mails adicionais (somam aos que já existem).
	ExtraEmails []string `json:"extra_emails,omitempty"`
	// ExtraPhones: telefones adicionais (somam aos que já existem).
	ExtraPhones []string `json:"extra_phones,omitempty"`
	// ClearFields: campos a enviar como null.
	ClearFields []string `json:"-"`
}

// IdentifierParams são os campos opcionais de Customers.Identifiers.Add e
// People.Identifiers.Add. nil (ou Label nil) = requisição sem corpo.
type IdentifierParams struct {
	// Label: rótulo livre (até 120; ex.: o nome do sistema — "CRM").
	Label *string `json:"label,omitempty"`
}

// ── produtos ─────────────────────────────────────────────────────────────────

// ProductListParams filtra Products.List.
type ProductListParams struct {
	// IncludeInactive: inclui os arquivados.
	IncludeInactive *bool `json:"include_inactive,omitempty"`
}

// ProductUpsertParams é o corpo de Products.Upsert. Parcial: campo nil é omitido; para
// limpar, use ClearFields.
type ProductUpsertParams struct {
	// Name: nome (1–255; obrigatório ao criar).
	Name *string `json:"name,omitempty"`
	// Description: descrição.
	Description *string `json:"description,omitempty"`
	// Color: cor (ex.: "#6366F1", até 16).
	Color *string `json:"color,omitempty"`
	// Icon: ícone (até 64).
	Icon *string `json:"icon,omitempty"`
	// IsActive: ativo (false arquiva).
	IsActive *bool `json:"is_active,omitempty"`
	// SortOrder: ordem de exibição.
	SortOrder *int `json:"sort_order,omitempty"`
	// ClearFields: campos a LIMPAR (enviados como null).
	ClearFields []string `json:"-"`
}

// ── release notes ────────────────────────────────────────────────────────────

// ReleaseNoteListParams filtra ReleaseNotes.List e ListAll.
type ReleaseNoteListParams struct {
	// Published: true = só publicadas; false = só rascunhos; nil = todas.
	Published *bool `json:"published,omitempty"`
	// Page: página (a partir de 1). Em ListAll, a página inicial (padrão 1).
	Page *int `json:"page,omitempty"`
	// PageSize: itens por página (1–200; em ListAll, padrão 100).
	PageSize *int `json:"page_size,omitempty"`
}

// ReleaseNoteUpsertParams é o corpo de ReleaseNotes.Upsert. Parcial: campo nil é omitido;
// para limpar, use ClearFields.
type ReleaseNoteUpsertParams struct {
	// Title: título (1–255; obrigatório ao criar).
	Title *string `json:"title,omitempty"`
	// DescriptionHTML: descrição em HTML (use este OU DescriptionMarkdown).
	DescriptionHTML *string `json:"description_html,omitempty"`
	// DescriptionMarkdown: descrição em Markdown (a API converte para HTML).
	DescriptionMarkdown *string `json:"description_markdown,omitempty"`
	// Audience: "internal", "external" ou "both".
	Audience *string `json:"audience,omitempty"`
	// RequireAckInternal: exige ciência da equipe interna.
	RequireAckInternal *bool `json:"require_ack_internal,omitempty"`
	// RequireAckExternal: exige ciência dos clientes (widget).
	RequireAckExternal *bool `json:"require_ack_external,omitempty"`
	// Publish: true publica na mesma chamada (ideal no CI). Não pode ser limpo.
	Publish *bool `json:"publish,omitempty" bfocus:"noclear"`
	// ClearFields: campos a LIMPAR (enviados como null).
	ClearFields []string `json:"-"`
}

// ── base de conhecimento ─────────────────────────────────────────────────────

// KBArticleListParams filtra KB.Articles.List e ListAll.
type KBArticleListParams struct {
	// Product: só os artigos deste produto (slug).
	Product *string `json:"product,omitempty"`
	// Status: "draft" ou "published".
	Status *string `json:"status,omitempty"`
	// Q: busca no título e no texto.
	Q *string `json:"q,omitempty"`
	// UpdatedSince: só os alterados a partir deste instante (enviado em UTC).
	UpdatedSince *time.Time `json:"updated_since,omitempty"`
	// Page: página (a partir de 1). Em ListAll, a página inicial (padrão 1).
	Page *int `json:"page,omitempty"`
	// PageSize: itens por página (1–100; em ListAll, padrão 100).
	PageSize *int `json:"page_size,omitempty"`
}

// KBArticleUpsertParams é o corpo de KB.Articles.Upsert. Parcial: campo nil é omitido;
// para limpar, use ClearFields — em especial, ClearFields: []string{"product"} torna o
// artigo global.
type KBArticleUpsertParams struct {
	// Title: título (1–200; obrigatório ao criar).
	Title *string `json:"title,omitempty"`
	// BodyHTML: corpo em HTML (use este OU BodyMarkdown).
	BodyHTML *string `json:"body_html,omitempty"`
	// BodyMarkdown: corpo em Markdown (a API converte para HTML).
	BodyMarkdown *string `json:"body_markdown,omitempty"`
	// Product: slug do produto. Limpe (ClearFields) para tornar o artigo global.
	Product *string `json:"product,omitempty"`
	// Status: "draft" ou "published".
	Status *string `json:"status,omitempty"`
	// ClearFields: campos a LIMPAR (enviados como null).
	ClearFields []string `json:"-"`
}

// KBBatchArticle é um item de KB.Articles.BatchUpsert: os campos de KBArticleUpsertParams
// mais o ExternalID do artigo.
type KBBatchArticle struct {
	// ExternalID: id do artigo no seu sistema (obrigatório; sem "/" — use ":" para
	// hierarquia, ex.: "git:guia:instalacao").
	ExternalID string `json:"external_id" bfocus:"noclear"`
	// Title: título (1–200; obrigatório ao criar).
	Title *string `json:"title,omitempty"`
	// BodyHTML: corpo em HTML (use este OU BodyMarkdown).
	BodyHTML *string `json:"body_html,omitempty"`
	// BodyMarkdown: corpo em Markdown (a API converte para HTML).
	BodyMarkdown *string `json:"body_markdown,omitempty"`
	// Product: slug do produto. ClearFields: []string{"product"} torna o artigo global.
	Product *string `json:"product,omitempty"`
	// Status: "draft" ou "published".
	Status *string `json:"status,omitempty"`
	// ClearFields: campos a LIMPAR (enviados como null).
	ClearFields []string `json:"-"`
}

// KBSearchParams são os campos opcionais de KB.Search.
type KBSearchParams struct {
	// Product: só artigos deste produto (e os globais).
	Product *string `json:"product,omitempty"`
	// Limit: máximo de resultados (1–20; padrão da API 5).
	Limit *int `json:"limit,omitempty"`
}

// ── agentes de IA ────────────────────────────────────────────────────────────

// AIAgentPreviewParams são os campos opcionais de AIAgents.Preview.
type AIAgentPreviewParams struct {
	// History: turnos anteriores da conversa (até 20).
	History []AIAgentPreviewTurn `json:"history,omitempty"`
}

// AIAgentPreviewTurn é um turno anterior da conversa em AIAgents.Preview.
type AIAgentPreviewTurn struct {
	// Role: "customer" ou "bot".
	Role string `json:"role"`
	// Content: texto (até 4000).
	Content string `json:"content"`
}
