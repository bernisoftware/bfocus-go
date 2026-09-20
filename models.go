package bfocus

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// Modelos de resposta. Campos desconhecidos são ignorados (a API ganha campos sem aviso);
// onde a spec permite campos extras (CustomField, AIAgentPreview), eles ficam em Extra e
// voltam no json.Marshal.

// Customer é um cliente.
type Customer struct {
	// ID no bFocus (UUID).
	ID string `json:"id"`
	// ExternalID: id do cliente no seu sistema.
	ExternalID   string        `json:"external_id"`
	Name         string        `json:"name"`
	Document     *string       `json:"document"`
	Email        *string       `json:"email"`
	Phone        *string       `json:"phone"`
	Website      *string       `json:"website"`
	Notes        *string       `json:"notes"`
	CustomFields []CustomField `json:"custom_fields"`
	IsActive     bool          `json:"is_active"`
	CreatedAt    *time.Time    `json:"created_at"`
	UpdatedAt    *time.Time    `json:"updated_at"`
}

// CustomField é um campo personalizado de um cliente ou de uma pessoa, como a API guardou.
type CustomField struct {
	Key   string  `json:"key"`
	Label *string `json:"label"`
	// Type: text (padrão), number, select…
	Type string `json:"type"`
	// Value: valor como veio no JSON (string, float64, bool, []any, map[string]any ou nil).
	Value any `json:"value"`
	// Visibility: quem vê o campo no bFocus (padrão "interno").
	Visibility string `json:"visibility"`
	// Extra: demais chaves que a API enviar, como vieram.
	Extra map[string]json.RawMessage `json:"-"`
	// sent: chaves conhecidas que REALMENTE vieram no JSON (nil num campo montado à mão).
	// Campo personalizado de PESSOA chega sem `type` — a API só guarda o que o seu sistema
	// mandou, mais a visibilidade. A leitura assume o padrão da spec ("text"), mas a
	// serialização não inventa de volta uma chave que a API não enviou.
	sent map[string]bool
}

var customFieldKeys = jsonKeys(reflect.TypeOf(CustomField{}))

// UnmarshalJSON preenche os campos conhecidos (com os padrões da API) e guarda o resto em
// Extra.
func (f *CustomField) UnmarshalJSON(b []byte) error {
	type plain CustomField
	p := plain{Type: "text", Visibility: "interno"}
	extra, err := decodeWithExtra(b, &p, customFieldKeys)
	if err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	sent := make(map[string]bool, len(customFieldKeys))
	for _, k := range customFieldKeys {
		if _, ok := all[k]; ok {
			sent[k] = true
		}
	}
	p.Extra = extra
	p.sent = sent
	*f = CustomField(p)
	return nil
}

// MarshalJSON serializa os campos conhecidos mais Extra. Num campo que veio da API, só as
// chaves que ela enviou (round-trip fiel); num campo montado no seu código, todas.
func (f CustomField) MarshalJSON() ([]byte, error) {
	type plain CustomField
	base, err := encodeWithExtra(plain(f), f.Extra)
	if err != nil || f.sent == nil {
		return base, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(base, &all); err != nil {
		return nil, err
	}
	for _, k := range customFieldKeys {
		if !f.sent[k] {
			delete(all, k)
		}
	}
	return json.Marshal(all)
}

// Contact é um contato de um cliente.
type Contact struct {
	// ID no bFocus (UUID).
	ID string `json:"id"`
	// ExternalID: id do contato no seu sistema.
	ExternalID *string    `json:"external_id"`
	Name       string     `json:"name"`
	Role       *string    `json:"role"`
	Email      *string    `json:"email"`
	Phone      *string    `json:"phone"`
	Notes      *string    `json:"notes"`
	IsPrimary  bool       `json:"is_primary"`
	CreatedAt  *time.Time `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

// ProductRef é a referência resumida a um produto.
type ProductRef struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

// Product é um produto do catálogo.
type Product struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
	// IsActive: false = arquivado.
	IsActive  bool `json:"is_active"`
	SortOrder int  `json:"sort_order"`
	// CurrentVersion: a versão da última release note publicada.
	CurrentVersion string `json:"current_version"`
	// AILevel: nível de IA configurado para o produto.
	AILevel   string     `json:"ai_level"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// Interaction é uma interação no histórico de um cliente.
type Interaction struct {
	ID string `json:"id"`
	// Content: conteúdo (HTML).
	Content string `json:"content"`
	// IsInternal: interna (não visível ao cliente).
	IsInternal bool `json:"is_internal"`
	// AuthorKind: tipo do autor ("human"…).
	AuthorKind string     `json:"author_kind"`
	AuthorName *string    `json:"author_name"`
	CreatedAt  *time.Time `json:"created_at"`
}

// ReleaseNote é uma release note de um produto.
type ReleaseNote struct {
	ID string `json:"id"`
	// Product: slug do produto.
	Product         string `json:"product"`
	Version         string `json:"version"`
	Title           string `json:"title"`
	DescriptionHTML string `json:"description_html"`
	// Audience: "internal", "external" ou "both".
	Audience           string     `json:"audience"`
	IsPublished        bool       `json:"is_published"`
	RequireAckInternal bool       `json:"require_ack_internal"`
	RequireAckExternal bool       `json:"require_ack_external"`
	PublishedAt        *time.Time `json:"published_at"`
	CreatedAt          *time.Time `json:"created_at"`
	UpdatedAt          *time.Time `json:"updated_at"`
}

// KBArticleSummary é um artigo da base de conhecimento SEM o corpo — como vem nas
// listagens e no resultado do lote. O artigo completo é KBArticle.
type KBArticleSummary struct {
	ID string `json:"id"`
	// ExternalID: id do artigo no seu sistema.
	ExternalID *string `json:"external_id"`
	// Product: slug do produto (nil = artigo global).
	Product *string `json:"product"`
	Title   string  `json:"title"`
	// Excerpt: resumo em texto.
	Excerpt string `json:"excerpt"`
	// Status: "draft" ou "published".
	Status string `json:"status"`
	// Origin: origem ("manual"…).
	Origin      string     `json:"origin"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

// KBArticle é o artigo completo (Get, Upsert, Publish, Unpublish): o resumo + BodyHTML.
type KBArticle struct {
	KBArticleSummary
	// BodyHTML: corpo em HTML.
	BodyHTML string `json:"body_html"`
}

// KBBatchResult é o resultado agregado de KB.Articles.BatchUpsert (todos os lotes).
type KBBatchResult struct {
	// Results: um resultado por artigo, na ordem enviada.
	Results   []KBBatchItemResult `json:"results"`
	Created   int                 `json:"created"`
	Updated   int                 `json:"updated"`
	Unchanged int                 `json:"unchanged"`
	// Failed: artigos com erro (veja KBBatchItemResult.Error).
	Failed int `json:"failed"`
}

// KBBatchItemResult é o resultado de um artigo no lote.
type KBBatchItemResult struct {
	ExternalID string `json:"external_id"`
	OK         bool   `json:"ok"`
	// Action: "created", "updated" ou "unchanged" (nil em erro).
	Action *string `json:"action"`
	// Error: código do erro (ex.: KB_ARTICLE_TITLE_REQUIRED); nil quando deu certo.
	Error *string `json:"error"`
	// Article: o artigo gravado, sem corpo (nil em erro).
	Article *KBArticleSummary `json:"article"`
}

// KBSearchHit é um resultado da busca na base de conhecimento.
type KBSearchHit struct {
	ID         string  `json:"id"`
	ExternalID *string `json:"external_id"`
	Title      string  `json:"title"`
	Excerpt    string  `json:"excerpt"`
}

// AIAgent é um agente de IA.
type AIAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Product: produto que o agente atende.
	Product   ProductRef `json:"product"`
	Active    bool       `json:"active"`
	Persona   *string    `json:"persona"`
	Scope     *string    `json:"scope"`
	AvatarURL *string    `json:"avatar_url"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// AIAgentPreview é a resposta de teste de um agente (AIAgents.Preview) e o diagnóstico.
type AIAgentPreview struct {
	// Action: o que o agente decidiu ("answer", "escalate"…).
	Action     string  `json:"action"`
	AnswerHTML *string `json:"answer_html"`
	// Escalated: encaminharia para um humano.
	Escalated bool `json:"escalated"`
	// Refused: recusou (fora do escopo).
	Refused       bool    `json:"refused"`
	HandoffReason *string `json:"handoff_reason"`
	// Confidence: confiança (0–1).
	Confidence *float64 `json:"confidence"`
	// Topic: assunto identificado na mensagem.
	Topic *string `json:"topic"`
	// Guards: guardas de segurança que dispararam.
	Guards []string `json:"guards"`
	// Citations: referências citadas na resposta, como vieram (índices em Sources).
	Citations []any `json:"citations"`
	// Sources: fontes usadas (ex.: {"type": "kb_article", "id": "…", "title": "…"}).
	Sources []map[string]any `json:"sources"`
	// Collected: dados que o agente já coletou do cliente (campo → valor).
	Collected map[string]any `json:"collected"`
	// Missing: dados que ainda faltam coletar.
	Missing []any `json:"missing"`
	// Extra: demais campos do diagnóstico que a API enviar, como vieram.
	Extra map[string]json.RawMessage `json:"-"`
}

var aiAgentPreviewKeys = jsonKeys(reflect.TypeOf(AIAgentPreview{}))

// UnmarshalJSON preenche os campos conhecidos e guarda o resto em Extra.
func (p *AIAgentPreview) UnmarshalJSON(b []byte) error {
	type plain AIAgentPreview
	var v plain
	extra, err := decodeWithExtra(b, &v, aiAgentPreviewKeys)
	if err != nil {
		return err
	}
	v.Extra = extra
	*p = AIAgentPreview(v)
	return nil
}

// MarshalJSON serializa os campos conhecidos mais Extra.
func (p AIAgentPreview) MarshalJSON() ([]byte, error) {
	type plain AIAgentPreview
	return encodeWithExtra(plain(p), p.Extra)
}

// DeleteResult é o resultado de uma exclusão.
type DeleteResult struct {
	Deleted bool `json:"deleted"`
}

// Identifier é um identificador extra de um cadastro: o id de OUTRO sistema seu ligado ao
// mesmo cliente/pessoa (o principal é o ExternalID do cadastro).
type Identifier struct {
	ExternalID string  `json:"external_id"`
	Label      *string `json:"label"`
	// Source: quem ligou ("api", "panel", "import"…).
	Source string `json:"source"`
}

// CustomerWithIdentifiers é o cliente com os identificadores extras
// (Customers.Identifiers.Add/Remove).
type CustomerWithIdentifiers struct {
	Customer
	// Identifiers: identificadores extras (o principal é ExternalID).
	Identifiers []Identifier `json:"identifiers"`
}

// Person é uma pessoa de um cliente: quem abre o widget/portal em nome dele.
type Person struct {
	// ExternalID: id da pessoa no seu sistema (nil = contato do cliente sem acesso, sem
	// identificador).
	ExternalID *string `json:"external_id"`
	Name       string  `json:"name"`
	Email      *string `json:"email"`
	Phone      *string `json:"phone"`
	Role       *string `json:"role"`
	// Access: pode abrir o widget/portal do cliente.
	Access    bool `json:"access"`
	IsPrimary bool `json:"is_primary"`
	// CustomerExternalID: external_id principal do cliente a que a pessoa pertence.
	CustomerExternalID string `json:"customer_external_id"`
	// CustomFields: campos personalizados da pessoa (Visibility vem definida no bFocus).
	CustomFields []CustomField `json:"custom_fields"`
}

// PersonUpsertResult é o retorno de People.Upsert: a pessoa + o que aconteceu.
type PersonUpsertResult struct {
	Person
	// Status: "created", "updated" ou "unchanged".
	Status string `json:"status"`
}

// PersonIdentifiers são os identificadores de uma pessoa (People.Identifiers.Add/Remove).
type PersonIdentifiers struct {
	// ExternalID: identificador principal da pessoa.
	ExternalID *string `json:"external_id"`
	// Identifiers: identificadores extras.
	Identifiers []Identifier `json:"identifiers"`
}

// BatchResult é o resultado de Customers.Batch e People.Batch: um resultado por item e os
// contadores. A falha de um item não desfaz os outros.
type BatchResult struct {
	// Results: um resultado por item; Index é a posição no lote ENVIADO.
	Results []BatchItemResult `json:"results"`
	Summary BatchSummary      `json:"summary"`
}

// BatchItemResult é o resultado de um item do lote.
type BatchItemResult struct {
	// Index: posição do item no lote enviado (0 = primeiro).
	Index int `json:"index"`
	// Status: "created", "updated", "unchanged" ou "error".
	Status string `json:"status"`
	// ExternalID: identificador do item (o principal, depois do upsert).
	ExternalID *string `json:"external_id"`
	// MergedInto: o id enviado é um identificador extra; este é o principal do cadastro —
	// atualize o id do seu lado.
	MergedInto *string `json:"merged_into"`
	// Error: código estável do erro do item (ex.: NAME_REQUIRED); nil quando deu certo.
	Error *string `json:"error"`
	// Code: status HTTP que o item teria sozinho (só em erro).
	Code *int `json:"code"`
}

// BatchSummary conta os itens do lote por status.
type BatchSummary struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Error     int `json:"error"`
}

func jsonKeys(t reflect.Type) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

func decodeWithExtra(b []byte, into any, known []string) (map[string]json.RawMessage, error) {
	if err := json.Unmarshal(b, into); err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	for _, k := range known {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

func encodeWithExtra(v any, extra map[string]json.RawMessage) ([]byte, error) {
	base, err := json.Marshal(v)
	if err != nil || len(extra) == 0 {
		return base, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(base, &all); err != nil {
		return nil, err
	}
	for k, raw := range extra {
		if _, known := all[k]; !known {
			all[k] = raw
		}
	}
	return json.Marshal(all)
}
