package bfocus

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrorType classifica um *Error. Os valores são os mesmos em todas as SDKs do bFocus.
type ErrorType string

const (
	// ErrorTypeAPI: qualquer status sem tipo próprio (ex.: 400) e resposta 2xx que não é o
	// envelope JSON da API (Code INVALID_RESPONSE).
	ErrorTypeAPI ErrorType = "api"
	// ErrorTypeAuthentication: 401 — chave ausente, inválida ou revogada.
	ErrorTypeAuthentication ErrorType = "authentication"
	// ErrorTypePermissionDenied: 403 — chave desligada, IP não liberado, escopo faltando
	// (RequiredScope) ou módulo não contratado (MODULE_NOT_CONTRACTED).
	ErrorTypePermissionDenied ErrorType = "permission_denied"
	// ErrorTypeNotFound: 404.
	ErrorTypeNotFound ErrorType = "not_found"
	// ErrorTypeConflict: 409 (ex.: RELEASE_NOTE_CONFLICT, KB_ARTICLE_EMPTY, AI_DISABLED).
	ErrorTypeConflict ErrorType = "conflict"
	// ErrorTypeValidation: 422 — detalhe por campo em Validation.
	ErrorTypeValidation ErrorType = "validation"
	// ErrorTypeRateLimit: 429 — espera sugerida em RetryAfter.
	ErrorTypeRateLimit ErrorType = "rate_limit"
	// ErrorTypeServer: 5xx.
	ErrorTypeServer ErrorType = "server"
	// ErrorTypeNetwork: falha de conexão ou tempo esgotado (Status 0, Code NETWORK_ERROR).
	ErrorTypeNetwork ErrorType = "network"
)

const (
	// CodeNetworkError é o Code de todo erro de rede.
	CodeNetworkError = "NETWORK_ERROR"
	// CodeInvalidResponse é o Code de uma resposta 2xx que não é o envelope JSON da API.
	CodeInvalidResponse = "INVALID_RESPONSE"
)

// Error é o erro devolvido para qualquer resposta fora de 2xx, para resposta 2xx inválida e
// para falha de rede. Na sua lógica, use Code — é estável (CUSTOMER_NOT_FOUND,
// INTEGRATION_SCOPE_MISSING…); Message é só para gente ler e pode mudar.
//
// Para o tipo, use errors.Is com os sentinelas (ErrNotFound, ErrRateLimit, ErrNetwork…) ou
// compare Type.
type Error struct {
	// Type classifica o erro pelo status (ErrorTypeNotFound…).
	Type ErrorType
	// Code: `error` do corpo, senão `message`, senão HTTP_<status> (corpo não-JSON);
	// NETWORK_ERROR em falha de rede; INVALID_RESPONSE em 2xx fora do envelope.
	Code string
	// Message: texto legível — o Code mais o contexto (status, escopo exigido, request_id…).
	Message string
	// Status HTTP (0 em erro de rede).
	Status int
	// RequestID — informe ao suporte. Vem do request_id do corpo, senão do header
	// X-Request-Id da resposta, senão é o X-Request-Id que a SDK enviou (a API ecoa o do
	// cliente, então bate com o log dela). Preenchido em todo *Error, inclusive de rede.
	RequestID string
	// Validation: campo → motivo, nos erros de validação (nil nos demais).
	Validation map[string]string
	// RetryAfter: espera pedida pela API no header Retry-After (só em 429; 0 nos demais).
	RetryAfter time.Duration
	// RequiredScope: escopo que faltou na chave, do header X-Required-Scope (só em 403 de
	// escopo).
	RequiredScope string
	// Err é a causa original (ex.: o erro de conexão), quando houver.
	Err error
}

// Error implementa a interface error.
func (e *Error) Error() string { return "bfocus: " + e.Message }

// Unwrap devolve a causa original (erro de conexão, JSON inválido…), quando houver.
func (e *Error) Unwrap() error { return e.Err }

// Is faz errors.Is(err, ErrNotFound) (e os demais sentinelas) comparar pelo Type.
func (e *Error) Is(target error) bool {
	s, ok := target.(*typeSentinel)
	return ok && s.typ == e.Type
}

func (e *Error) retryable() bool {
	switch {
	case e.Type == ErrorTypeNetwork:
		return true
	case e.Status == 429, e.Status == 502, e.Status == 503, e.Status == 504:
		return true
	}
	return false
}

type typeSentinel struct {
	typ  ErrorType
	text string
}

func (s *typeSentinel) Error() string { return s.text }

// Sentinelas para errors.Is: casam com qualquer *Error do tipo correspondente.
//
//	if errors.Is(err, bfocus.ErrNotFound) { … }
var (
	ErrAuthentication   error = &typeSentinel{ErrorTypeAuthentication, "bfocus: erro de autenticação (401)"}
	ErrPermissionDenied error = &typeSentinel{ErrorTypePermissionDenied, "bfocus: permissão negada (403)"}
	ErrNotFound         error = &typeSentinel{ErrorTypeNotFound, "bfocus: não encontrado (404)"}
	ErrConflict         error = &typeSentinel{ErrorTypeConflict, "bfocus: conflito (409)"}
	ErrValidation       error = &typeSentinel{ErrorTypeValidation, "bfocus: dados inválidos (422)"}
	ErrRateLimit        error = &typeSentinel{ErrorTypeRateLimit, "bfocus: limite de requisições (429)"}
	ErrServer           error = &typeSentinel{ErrorTypeServer, "bfocus: erro do servidor (5xx)"}
	ErrNetwork          error = &typeSentinel{ErrorTypeNetwork, "bfocus: erro de rede"}
)

// ErrInvalidArgument é satisfeito (errors.Is) pelos erros de argumento que a SDK devolve
// ANTES de qualquer requisição — parâmetro de caminho vazio, "." ou "..", "/" no
// external_id de artigo, item do lote sem ExternalID, campo desconhecido em ClearFields,
// tamanho de página < 1 — e pelos erros de NewClient (chave vazia, opção inválida) e de
// SignWidgetIdentity (argumento vazio). Não é um *Error.
var ErrInvalidArgument = errors.New("bfocus: argumento inválido")

type argumentError struct{ msg string }

func (e *argumentError) Error() string        { return "bfocus: argumento inválido: " + e.msg }
func (e *argumentError) Is(target error) bool { return target == ErrInvalidArgument }

func argErr(msg string) error { return &argumentError{msg: msg} }

func typeForStatus(status int) ErrorType {
	switch {
	case status == 401:
		return ErrorTypeAuthentication
	case status == 403:
		return ErrorTypePermissionDenied
	case status == 404:
		return ErrorTypeNotFound
	case status == 409:
		return ErrorTypeConflict
	case status == 422:
		return ErrorTypeValidation
	case status == 429:
		return ErrorTypeRateLimit
	case status >= 500 && status <= 599:
		return ErrorTypeServer
	}
	return ErrorTypeAPI
}

// errorFromResponse converte uma resposta fora de 2xx no *Error (BRIEF §4 e §10).
func errorFromResponse(status int, raw []byte, header http.Header, wait *time.Duration, sentRequestID string) *Error {
	var code, bodyMessage, requestID string
	var validation map[string]string

	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) == nil && body != nil {
		code = jsonString(body["error"])
		bodyMessage = jsonString(body["message"])
		requestID = jsonString(body["request_id"])
		var fields map[string]json.RawMessage
		if json.Unmarshal(body["validation"], &fields) == nil && len(fields) > 0 {
			validation = make(map[string]string, len(fields))
			for name, reason := range fields {
				if s := jsonString(reason); s != "" || string(reason) == `""` {
					validation[name] = s
				} else {
					validation[name] = string(reason)
				}
			}
		}
	}
	if code == "" {
		code = bodyMessage
	}
	if code == "" {
		code = "HTTP_" + strconv.Itoa(status)
	}
	// Corpo → header X-Request-Id → o que a SDK enviou (BRIEF §10.2).
	if requestID == "" {
		requestID = strings.TrimSpace(header.Get("X-Request-Id"))
	}
	if requestID == "" {
		requestID = sentRequestID
	}
	requiredScope := strings.TrimSpace(header.Get("X-Required-Scope"))
	var retryAfter time.Duration
	if status == 429 && wait != nil {
		retryAfter = *wait
	}

	msg := code
	if bodyMessage != "" && bodyMessage != code {
		msg += ": " + bodyMessage
	}
	details := []string{"HTTP " + strconv.Itoa(status)}
	if requiredScope != "" {
		details = append(details, "escopo exigido: "+requiredScope)
	}
	if module := strings.TrimSpace(header.Get("X-Required-Module")); module != "" {
		details = append(details, "módulo exigido: "+module)
	}
	names := make([]string, 0, len(validation))
	for name := range validation {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		details = append(details, name+": "+validation[name])
	}
	if retryAfter > 0 {
		details = append(details, "tente de novo em "+strconv.FormatFloat(retryAfter.Seconds(), 'f', -1, 64)+" s")
	}
	details = append(details, "request_id "+requestID)

	return &Error{
		Type:          typeForStatus(status),
		Code:          code,
		Message:       msg + " (" + strings.Join(details, "; ") + ")",
		Status:        status,
		RequestID:     requestID,
		Validation:    validation,
		RetryAfter:    retryAfter,
		RequiredScope: requiredScope,
	}
}

// invalidResponse: resposta 2xx que não é o envelope JSON da API (BRIEF §10.1).
func invalidResponse(status int, requestID, reason string, cause error) *Error {
	return &Error{
		Type:      ErrorTypeAPI,
		Code:      CodeInvalidResponse,
		Message:   CodeInvalidResponse + ": " + reason + " (HTTP " + strconv.Itoa(status) + "; request_id " + requestID + ")",
		Status:    status,
		RequestID: requestID,
		Err:       cause,
	}
}
