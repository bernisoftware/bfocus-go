package bfocus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
