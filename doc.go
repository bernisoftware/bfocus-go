// Package bfocus é a SDK oficial em Go da API pública do bFocus
// (https://api.bfocus.com.br): clientes, pessoas, contatos, lotes, identificadores extras,
// produtos, release notes, base de conhecimento e agentes de IA — com novas tentativas
// idempotentes, erros tipados e a assinatura da identidade do widget (v1 e v2). Só usa a
// biblioteca padrão.
//
//	client, err := bfocus.NewClient(os.Getenv("BFOCUS_API_KEY"))
//	if err != nil {
//		log.Fatal(err) // chave vazia
//	}
//	cliente, err := client.Customers.Upsert(ctx, "ERP 1042", &bfocus.CustomerUpsertParams{
//		Name:  bfocus.String("Padaria Estrela"),
//		Email: bfocus.String("contato@padaria.example"),
//	})
//
// Crie um Client por chave e reutilize-o: é seguro para uso concorrente e não faz nenhuma
// chamada de rede na construção. Todo método recebe um context.Context: ele limita a
// chamada inteira (todas as tentativas); WithTimeout limita cada tentativa.
//
// # Omitido × null
//
// Os upserts são parciais: a API só altera os campos presentes no corpo, e null explícito
// LIMPA o campo. Nos structs de parâmetros, campo nil (ponteiro ou slice) é omitido; para
// enviar null, ponha o nome do campo em ClearFields:
//
//	client.Customers.Upsert(ctx, "ERP 1042", &bfocus.CustomerUpsertParams{
//		Name:        bfocus.String("Padaria Estrela Ltda."),
//		ClearFields: []string{"phone"}, // corpo: {"name":"Padaria Estrela Ltda.","phone":null}
//	})
//
// # Erros
//
// Toda resposta fora de 2xx vira um *Error. Use o Code (estável) na sua lógica e
// errors.Is com os sentinelas (ErrNotFound, ErrRateLimit, ErrNetwork…) para o tipo:
//
//	var e *bfocus.Error
//	if errors.As(err, &e) && e.Code == "CUSTOMER_NOT_FOUND" { /* … */ }
//	if errors.Is(err, bfocus.ErrRateLimit) { /* … */ }
//
// Argumento inválido (chave vazia, parâmetro de caminho vazio, "." ou "..", "/" no
// external_id de artigo, campo desconhecido em ClearFields, mais de MaxBatchSize itens num
// lote) devolve, antes de qualquer
// requisição, um erro que satisfaz errors.Is(err, ErrInvalidArgument) — e que não é
// *Error. Cancelar o ctx devolve ctx.Err(), sem nova tentativa.
package bfocus
