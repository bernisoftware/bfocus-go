package bfocus

import "context"

// defaultListAllPageSize é o tamanho de página padrão dos ListAll (BRIEF §10.7).
const defaultListAllPageSize = 100

// Page é uma página de uma listagem paginada.
type Page[T any] struct {
	// Items: itens desta página (nunca nil).
	Items []T `json:"items"`
	// Page: número desta página (a partir de 1).
	Page int `json:"page"`
	// PageSize: tamanho da página.
	PageSize int `json:"page_size"`
	// Total: total de itens em todas as páginas.
	Total int `json:"total"`
	// Pages: total de páginas.
	Pages int `json:"pages"`
}

// HasNext diz se há página depois desta.
func (p *Page[T]) HasNext() bool { return len(p.Items) > 0 && p.Page < p.Pages }

// Iter percorre TODOS os itens de uma listagem paginada, buscando as páginas sob demanda
// (cada página é uma chamada nova, com X-Request-Id próprio). Para na última página ou numa
// página vazia.
//
//	it := client.Customers.ListAll(&bfocus.CustomerListParams{UpdatedSince: bfocus.Time(ultimaSync)})
//	for it.Next(ctx) {
//		c := it.Current()
//		// …
//	}
//	if err := it.Err(); err != nil {
//		// erro da API, de rede ou de argumento — os itens já entregues continuam válidos
//	}
//
// Um Iter não é seguro para uso concorrente.
type Iter[T any] struct {
	fetch func(ctx context.Context, page int) (*Page[T], error)
	next  int
	items []T
	idx   int
	cur   T
	err   error
	done  bool
}

// Next avança para o próximo item, buscando a próxima página quando preciso. Devolve false
// no fim ou em erro (veja Err). O ctx vale para as requisições desta chamada.
func (it *Iter[T]) Next(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if it.idx < len(it.items) {
			it.cur = it.items[it.idx]
			it.idx++
			return true
		}
		if it.done || it.err != nil {
			return false
		}
		page := it.next
		p, err := it.fetch(ctx, page)
		if err != nil {
			it.err = err
			it.items = nil
			return false
		}
		it.items, it.idx, it.next = p.Items, 0, page+1
		if len(p.Items) == 0 || page >= p.Pages {
			it.done = true
		}
	}
}

// Current devolve o item em que o último Next parou.
func (it *Iter[T]) Current() T { return it.cur }

// Err devolve o erro que interrompeu a iteração (nil se ela terminou normalmente).
func (it *Iter[T]) Err() error { return it.err }

// newIter valida a página inicial e o tamanho (padrão 1 e 100) e devolve o iterador.
func newIter[T any](startPage, pageSize *int, fetch func(ctx context.Context, page, size int) (*Page[T], error)) *Iter[T] {
	start, size := 1, defaultListAllPageSize
	if startPage != nil {
		if *startPage < 1 {
			return errIter[T](argErr("Page precisa ser ≥ 1"))
		}
		start = *startPage
	}
	if pageSize != nil {
		if *pageSize < 1 {
			return errIter[T](argErr("PageSize precisa ser ≥ 1"))
		}
		size = *pageSize
	}
	return &Iter[T]{
		next:  start,
		fetch: func(ctx context.Context, page int) (*Page[T], error) { return fetch(ctx, page, size) },
	}
}

func errIter[T any](err error) *Iter[T] { return &Iter[T]{err: err, done: true} }
