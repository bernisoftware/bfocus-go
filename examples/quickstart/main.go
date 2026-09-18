// Command quickstart mostra a SDK Go do bFocus em uso.
//
// Rodar:
//
//	BFOCUS_API_KEY=bf_live_... go run ./examples/quickstart
//
// ou, fora deste repositório:
//
//	BFOCUS_API_KEY=bf_live_... go run github.com/bernisoftware/bfocus-go/examples/quickstart@v0.1.0
//
// Opcional: BFOCUS_BASE_URL=http://localhost:8000 para apontar para a API local e
// BFOCUS_WIDGET_SECRET para assinar a identidade do widget. A chave precisa dos escopos
// customers:write, products:read e kb:read.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	bfocus "github.com/bernisoftware/bfocus-go"
)

func main() { os.Exit(run()) }

func run() int {
	apiKey := os.Getenv("BFOCUS_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Defina BFOCUS_API_KEY (Integrações → Chaves de API no bFocus).")
		return 2
	}
	var opts []bfocus.Option
	if baseURL := os.Getenv("BFOCUS_BASE_URL"); baseURL != "" {
		opts = append(opts, bfocus.WithBaseURL(baseURL))
	}
	client, err := bfocus.NewClient(apiKey, opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// O ctx limita o quickstart inteiro; cada tentativa tem o seu próprio tempo limite (30 s).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := tour(ctx, client); err != nil {
		var e *bfocus.Error
		if errors.As(err, &e) {
			// Decida pelo Code (estável); informe o RequestID ao suporte.
			fmt.Fprintf(os.Stderr, "erro %s (HTTP %d) request_id=%s\n", e.Code, e.Status, e.RequestID)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}

	// Identidade do widget: assinada no SEU backend, sem rede e sem chave de API.
	if secret := os.Getenv("BFOCUS_WIDGET_SECRET"); secret != "" {
		signature, err := bfocus.SignWidgetIdentity(secret, "USR-1", "ERP 1042")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("assinatura do widget:", signature)
	}
	return 0
}

func tour(ctx context.Context, client *bfocus.Client) error {
	// 1) Cliente: cria ou atualiza pelo id do SEU sistema. Só o que você passa muda.
	customer, err := client.Customers.Upsert(ctx, "ERP 1042", &bfocus.CustomerUpsertParams{
		Name:         bfocus.String("Padaria Estrela"),
		Email:        bfocus.String("contato@padaria.example"),
		CustomFields: []bfocus.CustomFieldInput{{Key: "plano", Label: "Plano", Value: "ouro"}},
	})
	if err != nil {
		return err
	}
	fmt.Println("cliente:", customer.ID, customer.Name)

	// 2) Registro no histórico do cliente.
	if _, err := client.Customers.Interactions.Create(ctx, "ERP 1042", "Cliente sincronizado pelo quickstart.", nil); err != nil {
		return err
	}

	// 3) Catálogo de produtos.
	products, err := client.Products.List(ctx, nil)
	if err != nil {
		return err
	}
	for _, p := range products {
		fmt.Println("produto:", p.Slug, "-", p.Name)
	}

	// 4) Busca na base de conhecimento.
	hits, err := client.KB.Search(ctx, "como emitir nota fiscal", &bfocus.KBSearchParams{Limit: bfocus.Int(3)})
	if err != nil {
		return err
	}
	for _, hit := range hits {
		fmt.Println("artigo:", hit.Title)
	}

	// 5) Todos os clientes com "padaria" (o iterador busca as páginas sozinho).
	total := 0
	it := client.Customers.ListAll(&bfocus.CustomerListParams{Q: bfocus.String("padaria")})
	for it.Next(ctx) {
		total++
	}
	if err := it.Err(); err != nil {
		return err
	}
	fmt.Println("clientes com 'padaria':", total)
	return nil
}
