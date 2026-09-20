package bfocus

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// PeopleService: pessoas dos clientes (quem abre o widget/portal em nome do cliente) —
// client.People. Escopos: customers:read / customers:write.
//
// O e-mail (ou o telefone) acha a pessoa que já chegou por e-mail ou por outro sistema, e
// ela é ADOTADA — nunca duplicada. A mesma pessoa enviada com outro cliente é LIGADA a ele
// também (cadastro único em N clientes); Linked volta true.
type PeopleService struct {
	client *Client
	// Identifiers: identificadores extras das pessoas.
	Identifiers *PersonIdentifiersService
}

func newPeopleService(c *Client) *PeopleService {
	return &PeopleService{client: c, Identifiers: &PersonIdentifiersService{client: c}}
}

func personPath(customerExternalID, personExternalID string) (string, error) {
	base, err := customerPath(customerExternalID)
	if err != nil {
		return "", err
	}
	seg, err := segment("personExternalID", personExternalID)
	if err != nil {
		return "", err
	}
	return base + "/people/" + seg, nil
}

// Upsert cria a pessoa personExternalID (o id dela no SEU sistema — o mesmo user.externalId
// assinado no widget) no cliente customerExternalID, ou atualiza se já existe — PUT
// /customers/{external_id}/people/{person_external_id}, corpo {"person": {…}}. Só os
// campos informados mudam; params nil envia {"person": {}}. Status do resultado:
// "created", "updated" ou "unchanged". Access: Bool(true) devolve o acesso retirado por
// Delete.
//
// CustomFields é a exceção ao "só o que vier muda": quando a lista vai (não nil), ela
// SUBSTITUI a lista inteira de campos personalizados da pessoa — campo que ficar de fora é
// REMOVIDO. Mande o que o seu sistema tem hoje, ou deixe nil.
//
// Clear APAGA contato ("email", "phone" ou os dois) e não se confunde com ClearFields, que
// manda o campo como null — e em pessoa null quer dizer "não mexe". Campo fora da lista
// aceita: ErrValidation com Code PERSON_CLEAR_FIELD_INVALID. Clear por um identificador
// EXTRA: ErrConflict com Code PERSON_CLEAR_NOT_OWN_RECORD (só se limpa a própria ficha).
func (s *PeopleService) Upsert(ctx context.Context, customerExternalID, personExternalID string, params *PersonParams, opts ...RequestOption) (*PersonUpsertResult, error) {
	path, err := personPath(customerExternalID, personExternalID)
	if err != nil {
		return nil, err
	}
	person, err := encodePatch(params)
	if err != nil {
		return nil, err
	}
	body := append(append([]byte(`{"person":`), person...), '}')
	return callObject[PersonUpsertResult](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// List lista as pessoas do cliente — GET /customers/{external_id}/people.
func (s *PeopleService) List(ctx context.Context, customerExternalID string) ([]Person, error) {
	path, err := customerPath(customerExternalID)
	if err != nil {
		return nil, err
	}
	return callList[Person](ctx, s.client, apiRequest{method: http.MethodGet, path: path + "/people"})
}

// Delete RETIRA O ACESSO da pessoa — DELETE /customers/{external_id}/people/{person_external_id}.
// A pessoa continua no histórico (devolvida com Access false); Upsert com Access
// Bool(true) devolve o acesso. Não achou: ErrNotFound com Code PERSON_NOT_FOUND.
func (s *PeopleService) Delete(ctx context.Context, customerExternalID, personExternalID string, opts ...RequestOption) (*PersonRevokeResult, error) {
	path, err := personPath(customerExternalID, personExternalID)
	if err != nil {
		return nil, err
	}
	return callObject[PersonRevokeResult](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}

// Batch cria ou atualiza até MaxBatchSize pessoas numa chamada — POST /people/batch. Cada
// item leva o cliente (CustomerExternalID), o id da pessoa (ExternalID) e os campos de
// PersonParams.
//
// Mais de MaxBatchSize itens: erro de argumento, sem requisição (a SDK NÃO divide — divida
// em fatias de MaxBatchSize). Lista vazia devolve o resultado zerado sem requisição. Todos
// os itens são validados antes do envio. A falha de um item não desfaz os outros: confira
// Summary.Error e, em cada BatchItemResult, Error/Code.
func (s *PeopleService) Batch(ctx context.Context, items []PersonBatchItem, opts ...RequestOption) (*BatchResult, error) {
	if err := checkBatchSize("People.Batch", len(items)); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return emptyBatchResult(), nil
	}
	encoded := make([][]byte, len(items))
	for i := range items {
		it := &items[i]
		if it.CustomerExternalID == "" {
			return nil, argErr(fmt.Sprintf("o item #%d do lote (People.Batch) não tem CustomerExternalID", i))
		}
		if it.ExternalID == "" {
			return nil, argErr(fmt.Sprintf("o item #%d do lote (People.Batch) não tem ExternalID", i))
		}
		person, err := encodePatch(it)
		if err != nil {
			return nil, err
		}
		customer, err := marshalJSON(it.CustomerExternalID)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		b.WriteString(`{"customer_external_id":`)
		b.Write(customer)
		b.WriteString(`,"person":`)
		b.Write(person)
		b.WriteByte('}')
		encoded[i] = b.Bytes()
	}
	return postBatch(ctx, s.client, "/people/batch", encoded, opts)
}

// PersonIdentifiersService: identificadores extras de uma pessoa — client.People.Identifiers.
// Liga o id de OUTRO sistema seu à mesma pessoa.
type PersonIdentifiersService struct{ client *Client }

func personIdentifierPath(personExternalID, extraID string) (string, error) {
	person, err := segment("personExternalID", personExternalID)
	if err != nil {
		return "", err
	}
	extra, err := segment("extraID", extraID)
	if err != nil {
		return "", err
	}
	return "/people/" + person + "/identifiers/" + extra, nil
}

// List devolve TODOS os identificadores da pessoa — o principal (ExternalID do retorno) e os
// extras — GET /people/{person_external_id}/identifiers. Escopo customers:read. Aceita no
// caminho o principal OU qualquer um dos extras. Pessoa inexistente: ErrNotFound com Code
// PERSON_NOT_FOUND.
//
// É a fonte de verdade para RECONCILIAR: People.List mostra só o identificador principal,
// então um id que virou extra some de lá sem ter sumido do cadastro — e, sem esta leitura,
// era preciso ESCREVER (tentar um Add) para descobrir o que tinha acontecido.
func (s *PersonIdentifiersService) List(ctx context.Context, personExternalID string) (*PersonIdentifiers, error) {
	person, err := segment("personExternalID", personExternalID)
	if err != nil {
		return nil, err
	}
	return callObject[PersonIdentifiers](ctx, s.client, apiRequest{method: http.MethodGet, path: "/people/" + person + "/identifiers"})
}

// Add liga o identificador extraID à pessoa (idempotente) — PUT
// /people/{person_external_id}/identifiers/{extra_id}. params pode ser nil (sem corpo); com
// Label, o corpo é {"label": …}. extraID já é de outro cadastro: ErrConflict com Code
// IDENTIFIER_IN_USE.
func (s *PersonIdentifiersService) Add(ctx context.Context, personExternalID, extraID string, params *IdentifierParams, opts ...RequestOption) (*PersonIdentifiers, error) {
	path, err := personIdentifierPath(personExternalID, extraID)
	if err != nil {
		return nil, err
	}
	body, err := identifierBody(params)
	if err != nil {
		return nil, err
	}
	return callObject[PersonIdentifiers](ctx, s.client, writeRequest(http.MethodPut, path, body, opts))
}

// Remove desliga o identificador extraID da pessoa — DELETE
// /people/{person_external_id}/identifiers/{extra_id}. Não ligado: ErrNotFound com Code
// IDENTIFIER_NOT_FOUND.
func (s *PersonIdentifiersService) Remove(ctx context.Context, personExternalID, extraID string, opts ...RequestOption) (*PersonIdentifiers, error) {
	path, err := personIdentifierPath(personExternalID, extraID)
	if err != nil {
		return nil, err
	}
	return callObject[PersonIdentifiers](ctx, s.client, writeRequest(http.MethodDelete, path, nil, opts))
}

// identifierBody: {"label": …} só quando Label veio; senão, sem corpo.
func identifierBody(params *IdentifierParams) ([]byte, error) {
	if params == nil || params.Label == nil {
		return nil, nil
	}
	return marshalJSON(params)
}
