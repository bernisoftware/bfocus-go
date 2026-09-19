package bfocus

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// MaxBatchSize é o máximo de itens por chamada de Customers.Batch e People.Batch (limite da
// API). Acima disso a SDK devolve erro de argumento ANTES de qualquer requisição — ela NÃO
// divide sozinha (diferente de KB.Articles.BatchUpsert): o Index de cada resultado é a
// posição no lote que VOCÊ enviou. Para listas maiores, divida em fatias de MaxBatchSize.
const MaxBatchSize = 500

func checkBatchSize(method string, n int) error {
	if n > MaxBatchSize {
		return argErr(fmt.Sprintf("%s aceita até %d itens por chamada (recebeu %d); divida em lotes de %d", method, MaxBatchSize, n, MaxBatchSize))
	}
	return nil
}

// emptyBatchResult é o resultado de um lote vazio (sem requisição).
func emptyBatchResult() *BatchResult {
	return &BatchResult{Results: []BatchItemResult{}}
}

// postBatch envia {"items": [...]} (itens já serializados) numa única chamada lógica.
func postBatch(ctx context.Context, c *Client, path string, items [][]byte, opts []RequestOption) (*BatchResult, error) {
	var body bytes.Buffer
	body.WriteString(`{"items":[`)
	body.Write(bytes.Join(items, []byte(",")))
	body.WriteString(`]}`)
	result, err := callObject[BatchResult](ctx, c, writeRequest(http.MethodPost, path, body.Bytes(), opts))
	if err != nil {
		return nil, err
	}
	if result.Results == nil {
		result.Results = []BatchItemResult{}
	}
	return result, nil
}
