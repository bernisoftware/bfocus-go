package bfocus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Corpo dos upserts ("só o que veio muda"). Nos structs de parâmetros:
//   - campo nil (ponteiro, slice, map) = OMITIDO: não vai no corpo e fica como está;
//   - campo em ClearFields = null: LIMPA o campo (se tiver valor, vale o valor);
//   - campo não-ponteiro (ex.: KBBatchArticle.ExternalID) vai sempre.
//
// ClearFields aceita o nome JSON ("phone") ou o nome Go ("Phone"). Nome desconhecido, ou
// campo que a API não aceita como null (tag `bfocus:"noclear"`), é erro de argumento antes
// de qualquer requisição.

type patchField struct {
	index     int
	goName    string
	wire      string
	nillable  bool
	clearable bool
}

type patchSchema struct {
	fields     []patchField
	byName     map[string]int
	clearIndex int
}

var patchSchemas sync.Map // reflect.Type → *patchSchema

func schemaFor(t reflect.Type) *patchSchema {
	if s, ok := patchSchemas.Load(t); ok {
		return s.(*patchSchema)
	}
	s := &patchSchema{byName: map[string]int{}, clearIndex: -1}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Name == "ClearFields" {
			s.clearIndex = i
			continue
		}
		wire, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if wire == "" || wire == "-" {
			continue
		}
		k := f.Type.Kind()
		nillable := k == reflect.Pointer || k == reflect.Slice || k == reflect.Map || k == reflect.Interface
		s.byName[wire] = len(s.fields)
		s.byName[f.Name] = len(s.fields)
		s.fields = append(s.fields, patchField{
			index:     i,
			goName:    f.Name,
			wire:      wire,
			nillable:  nillable,
			clearable: nillable && f.Tag.Get("bfocus") != "noclear",
		})
	}
	actual, _ := patchSchemas.LoadOrStore(t, s)
	return actual.(*patchSchema)
}

func (s *patchSchema) clearable() string {
	var names []string
	for _, f := range s.fields {
		if f.clearable {
			names = append(names, f.wire)
		}
	}
	return strings.Join(names, ", ")
}

// encodePatch serializa um struct de parâmetros de upsert (ponteiro nil = {}), na ordem
// dos campos do struct.
func encodePatch(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return []byte("{}"), nil
	}
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return []byte("{}"), nil
		}
		rv = rv.Elem()
	}
	typeName := rv.Type().Name()
	s := schemaFor(rv.Type())

	clear := map[string]bool{}
	if s.clearIndex >= 0 {
		names, _ := rv.Field(s.clearIndex).Interface().([]string)
		for _, name := range names {
			i, ok := s.byName[name]
			if !ok {
				return nil, argErr(fmt.Sprintf("%q não é um campo de %s (ClearFields); campos que podem ser limpos: %s", name, typeName, s.clearable()))
			}
			f := s.fields[i]
			if !f.clearable {
				return nil, argErr(fmt.Sprintf("o campo %q de %s não pode ser limpo (a API não aceita null nele)", f.wire, typeName))
			}
			clear[f.wire] = true
		}
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	n := 0
	for _, f := range s.fields {
		fv := rv.Field(f.index)
		var raw []byte
		if f.nillable && fv.IsNil() {
			if !clear[f.wire] {
				continue
			}
			raw = []byte("null")
		} else {
			var err error
			if raw, err = marshalJSON(fv.Interface()); err != nil {
				return nil, fmt.Errorf("bfocus: serializar %s.%s: %w", typeName, f.goName, err)
			}
		}
		if n > 0 {
			buf.WriteByte(',')
		}
		key, _ := marshalJSON(f.wire)
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(raw)
		n++
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalJSON é o json.Marshal sem escapar <, > e & (corpos com HTML ficam legíveis no log).
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
