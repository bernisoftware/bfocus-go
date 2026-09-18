package bfocus

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// escapeComponent faz o percent-encoding de um componente (segmento de caminho, chave ou
// valor de query): só os não reservados (A-Z a-z 0-9 - . _ ~) passam como estão — como o
// encodeURIComponent/Uri.EscapeDataString das outras SDKs (url.PathEscape deixaria ':'
// e '@' crus).
func escapeComponent(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if isUnreserved(ch) {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[ch>>4])
		b.WriteByte(hexDigits[ch&0x0f])
	}
	return b.String()
}

func isUnreserved(ch byte) bool {
	return 'A' <= ch && ch <= 'Z' || 'a' <= ch && ch <= 'z' || '0' <= ch && ch <= '9' ||
		ch == '-' || ch == '.' || ch == '_' || ch == '~'
}

// segment valida e codifica um parâmetro de caminho. Vazio, "." e ".." são recusados
// antes de qualquer requisição (o cliente HTTP resolveria "%2E%2E" como caminho).
func segment(param, value string) (string, error) {
	switch value {
	case "":
		return "", argErr(param + " não pode ser vazio")
	case ".", "..":
		return "", argErr(fmt.Sprintf("%s não pode ser %q", param, value))
	}
	return escapeComponent(value), nil
}

// kbSegment: o external_id de artigo não aceita "/" (a API recusa) — use ":" para hierarquia.
func kbSegment(param, value string) (string, error) {
	if strings.Contains(value, "/") {
		return "", argErr(fmt.Sprintf("%s de artigo não aceita '/' — use ':' para hierarquia (ex.: \"git:guia:instalacao\"): %q", param, value))
	}
	return segment(param, value)
}

// query monta a query string: omite o que não foi informado; booleanos true/false; datas
// ISO 8601 em UTC com Z.
type query struct{ pairs []string }

func (q *query) set(name, value string) *query {
	q.pairs = append(q.pairs, escapeComponent(name)+"="+escapeComponent(value))
	return q
}

func (q *query) setString(name string, v *string) *query {
	if v != nil {
		q.set(name, *v)
	}
	return q
}

func (q *query) setInt(name string, v *int) *query {
	if v != nil {
		q.set(name, strconv.Itoa(*v))
	}
	return q
}

func (q *query) setBool(name string, v *bool) *query {
	if v != nil {
		q.set(name, strconv.FormatBool(*v))
	}
	return q
}

func (q *query) setTime(name string, v *time.Time) *query {
	if v != nil {
		q.set(name, formatTime(*v))
	}
	return q
}

func (q *query) encode() string {
	if q == nil || len(q.pairs) == 0 {
		return ""
	}
	return "?" + strings.Join(q.pairs, "&")
}

// formatTime: "2026-09-01T03:00:00Z" (fração de segundo só quando houver).
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
