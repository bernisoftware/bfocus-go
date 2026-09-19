package bfocus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// SignWidgetIdentity assina a identidade do usuário logado para o widget do bFocus: hex
// minúsculo de HMAC-SHA256(secret, "v1:" + userExternalID + ":" + customerExternalID)
// (UTF-8). Roda no SEU backend, com o segredo do widget — nunca envie o segredo ao
// navegador. Não precisa de chave de API nem de rede.
//
// Argumento vazio devolve um erro que satisfaz errors.Is(err, ErrInvalidArgument): um
// segredo vazio produziria uma assinatura que qualquer um forja.
func SignWidgetIdentity(secret, userExternalID, customerExternalID string) (string, error) {
	for _, arg := range [...]struct{ name, value string }{
		{"secret", secret},
		{"userExternalID", userExternalID},
		{"customerExternalID", customerExternalID},
	} {
		if arg.value == "" {
			return "", argErr(arg.name + " é obrigatório (bfocus.SignWidgetIdentity)")
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v1:" + userExternalID + ":" + customerExternalID))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// SignWidgetIdentityV2 assina a identidade do usuário logado para o widget com VALIDADE —
// o mesmo que SignWidgetIdentityV2At no instante atual. Devolve "v2.<ts>.<hex>": ts =
// segundos unix; hex = HMAC-SHA256(secret, "v2:" + ts + ":" + userExternalID + ":" +
// customerExternalID), hex minúsculo (UTF-8). Vai no userHash do widget, no mesmo lugar da
// v1 (que continua aceita).
//
// A API aceita a assinatura de 7 dias atrás até 5 minutos à frente: gere a cada
// renderização da página, nunca guarde. Roda no SEU backend; não precisa de rede.
//
// Erro de argumento (errors.Is(err, ErrInvalidArgument)): argumento vazio, ou
// userExternalID com ":" (é o separador; o id do CLIENTE pode ter ":").
func SignWidgetIdentityV2(secret, userExternalID, customerExternalID string) (string, error) {
	return signWidgetIdentityV2("bfocus.SignWidgetIdentityV2", secret, userExternalID, customerExternalID, time.Now())
}

// SignWidgetIdentityV2At é SignWidgetIdentityV2 com o instante fixo at (ts = at em
// segundos unix, arredondado para baixo). Instante antes de 1970 (inclusive o time.Time
// zero) é erro de argumento.
func SignWidgetIdentityV2At(secret, userExternalID, customerExternalID string, at time.Time) (string, error) {
	return signWidgetIdentityV2("bfocus.SignWidgetIdentityV2At", secret, userExternalID, customerExternalID, at)
}

func signWidgetIdentityV2(fn, secret, userExternalID, customerExternalID string, at time.Time) (string, error) {
	for _, arg := range [...]struct{ name, value string }{
		{"secret", secret},
		{"userExternalID", userExternalID},
		{"customerExternalID", customerExternalID},
	} {
		if arg.value == "" {
			return "", argErr(arg.name + " é obrigatório (" + fn + ")")
		}
	}
	if strings.Contains(userExternalID, ":") {
		return "", argErr("userExternalID não pode ter ':' — é o separador da assinatura; use outro (ex.: \"app-77\") (" + fn + ")")
	}
	unix := at.Unix()
	if unix < 0 {
		return "", argErr("o instante da assinatura não pode ser anterior a 1970 (" + fn + ")")
	}
	ts := strconv.FormatInt(unix, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v2:" + ts + ":" + userExternalID + ":" + customerExternalID))
	return "v2." + ts + "." + hex.EncodeToString(mac.Sum(nil)), nil
}
