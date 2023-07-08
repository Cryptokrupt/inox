package core

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/inoxlang/inox/internal/utils"
	jsoniter "github.com/json-iterator/go"
)

const (
	JSON_UNTYPED_VALUE_SUFFIX = "__value"
)

var (
	ErrPatternDoesNotMatchValueToSerialize = errors.New("pattern does not match value to serialize")
	ErrPatternRequiredToSerialize          = errors.New("pattern required to serialize")
)

// this file contains the implementation of Value.HasJSONRepresentation & Value.WriteJSONRepresentation for core types.

type JSONSerializationConfig struct {
	*ReprConfig
	Pattern  Pattern //nillable
	Location string  //location of the current value being serialized
}

func GetJSONRepresentation(v Value, ctx *Context) string {
	buff := bytes.NewBuffer(nil)
	encountered := map[uintptr]int{}

	stream := jsoniter.NewStream(jsoniter.ConfigCompatibleWithStandardLibrary, nil, 10)

	err := v.WriteJSONRepresentation(ctx, stream, encountered, JSONSerializationConfig{})
	if err != nil {
		panic(fmt.Errorf("%s: %w", Stringify(v, ctx), err))
	}
	return buff.String()
}

func writeUntypedValueJSON(typeName string, fn func(w *jsoniter.Stream), w *jsoniter.Stream) {
	w.WriteObjectStart()
	w.WriteObjectField(typeName + JSON_UNTYPED_VALUE_SUFFIX)
	fn(w)
	w.WriteObjectEnd()
}

func fmtErrPatternDoesNotMatchValueToSerialize(ctx *Context, config JSONSerializationConfig) error {
	return fmt.Errorf("%w at /%s, pattern: %s", ErrPatternDoesNotMatchValueToSerialize, config.Location, Stringify(config.Pattern, ctx))
}

func (Nil NilT) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (Nil NilT) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	w.WriteNil()
	return nil
}

func (b Bool) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (b Bool) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	w.WriteBool(bool(b))
	return nil
}

func (r Rune) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (r Rune) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(RUNE_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(r))
		}, w)
		return nil
	}

	w.WriteString(string(r))
	return nil
}

func (Byte) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (b Byte) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (Int) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (i Int) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	fmt.Fprintf(w, `"%d"`, i)
	return nil
}

func (Float) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (f Float) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	w.WriteFloat64(float64(f))
	return w.Error
}

func (Str) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (s Str) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	w.WriteString(string(s))
	return nil
}

func (obj *Object) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	ptr := reflect.ValueOf(obj).Pointer()
	if _, ok := encountered[ptr]; ok {
		return false
	}
	encountered[ptr] = -1

	obj.Lock(nil)
	defer obj.Unlock(nil)
	for _, v := range obj.values {
		if !v.HasJSONRepresentation(encountered, config) {
			return false
		}
	}
	return true
}

func (obj *Object) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	//TODO: prevent modification of the Object while this function is running

	if encountered != nil && !obj.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	var entryPatterns map[string]Pattern

	objPatt, ok := config.Pattern.(*ObjectPattern)
	if ok {
		entryPatterns = objPatt.entryPatterns
	}

	_, err := w.Write([]byte{'{'})
	if err != nil {
		return err
	}

	closestState := ctx.GetClosestState()
	obj.Lock(closestState)
	defer obj.Unlock(closestState)
	keys := obj.keys
	visibility, _ := GetVisibility(obj.visibilityId)

	first := true
	for i, k := range keys {
		v := obj.values[i]

		if !config.IsPropertyVisible(k, v, visibility, ctx) {
			continue
		}

		if !first {
			w.Write([]byte{','})
		}
		first = false
		jsonStr, _ := utils.MarshalJsonNoHTMLEspace(k)
		_, err = w.Write(jsonStr)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte{':'})
		if err != nil {
			return err
		}
		err = v.WriteJSONRepresentation(ctx, w, nil, JSONSerializationConfig{
			ReprConfig: config.ReprConfig,
			Pattern:    entryPatterns[k],
		})
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte{'}'})
	if err != nil {
		return err
	}
	return nil
}

func (rec *Record) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (rec *Record) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !rec.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	var entryPatterns map[string]Pattern

	recPatt, ok := config.Pattern.(*RecordPattern)
	if ok {
		entryPatterns = recPatt.entryPatterns
	}

	_, err := w.Write([]byte{'{'})
	if err != nil {
		return err
	}

	keys := rec.keys
	first := true

	for i, k := range keys {
		v := rec.values[i]

		if !config.IsPropertyVisible(k, v, nil, ctx) {
			continue
		}

		if !first {
			w.Write([]byte{','})
		}

		first = false
		jsonStr, _ := utils.MarshalJsonNoHTMLEspace(k)
		_, err = w.Write(jsonStr)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte{':'})
		if err != nil {
			return err
		}
		err = v.WriteJSONRepresentation(ctx, w, nil, JSONSerializationConfig{
			ReprConfig: config.ReprConfig,
			Pattern:    entryPatterns[k],
		})
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte{'}'})
	if err != nil {
		return err
	}
	return nil
}

func (dict *Dictionary) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (dict *Dictionary) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (list KeyList) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (list KeyList) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation

}

func (list *List) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return list.underylingList.HasJSONRepresentation(encountered, config)
}

func (list *List) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return list.underylingList.WriteJSONRepresentation(ctx, w, encountered, config)
}

func (list *ValueList) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	ptr := reflect.ValueOf(list).Pointer()
	if _, ok := encountered[ptr]; ok {
		return false
	}
	encountered[ptr] = -1

	for _, v := range list.elements {
		if !v.HasJSONRepresentation(encountered, config) {
			return false
		}
	}
	return true
}

func (list *ValueList) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !list.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	var generalElementPattern Pattern
	listPattern, _ := config.Pattern.(*ListPattern)
	if listPattern == nil && config.Pattern != nil {
		generalElementPattern = ANYVAL_PATTERN
	}

	_, err := w.Write([]byte{'['})
	if err != nil {
		return err
	}
	first := true
	for i, v := range list.elements {

		if !first {
			_, err = w.Write([]byte{','})
			if err != nil {
				return err
			}
		}
		first = false

		elementConfig := JSONSerializationConfig{
			ReprConfig: config.ReprConfig,
		}

		if generalElementPattern != nil {
			elementConfig.Pattern = generalElementPattern
		} else if listPattern != nil {
			elementConfig.Pattern = listPattern.elementPatterns[i]
		}

		err = v.WriteJSONRepresentation(ctx, w, encountered, elementConfig)
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte{']'})
	return err
}

func (list *IntList) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (list *IntList) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !list.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	var generalElementPattern Pattern
	listPattern, _ := config.Pattern.(*ListPattern)
	if listPattern == nil && config.Pattern != nil {
		generalElementPattern = ANYVAL_PATTERN
	}

	_, err := w.Write([]byte{'['})
	if err != nil {
		return err
	}
	first := true
	for i, v := range list.Elements {
		if !first {
			_, err = w.Write([]byte{','})
			if err != nil {
				return err
			}
		}
		first = false

		elementConfig := JSONSerializationConfig{
			ReprConfig: config.ReprConfig,
		}

		if generalElementPattern != nil {
			elementConfig.Pattern = generalElementPattern
		} else if listPattern != nil {
			elementConfig.Pattern = listPattern.elementPatterns[i]
		}

		err = v.WriteJSONRepresentation(ctx, w, nil, elementConfig)
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte{']'})
	return err
}

func (tuple *BoolList) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (list *BoolList) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !list.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	return list.WriteRepresentation(ctx, w, nil, &ReprConfig{
		AllVisible: true,
	})
}

func (list *StringList) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (list *StringList) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !list.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	_, err := w.Write([]byte{'['})
	if err != nil {
		return err
	}
	first := true
	for _, v := range list.elements {
		if !first {
			_, err = w.Write([]byte{','})
			if err != nil {
				return err
			}
		}
		first = false
		err = v.WriteJSONRepresentation(ctx, w, nil, config)
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte{']'})
	return err
}

func (tuple *Tuple) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (tuple *Tuple) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !tuple.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	tuplePattern := config.Pattern.(*TuplePattern)

	_, err := w.Write([]byte{'['})
	if err != nil {
		return err
	}

	first := true
	for i, v := range tuple.elements {

		if !first {
			_, err = w.Write([]byte{','})
			if err != nil {
				return err
			}
		}
		first = false

		elementConfig := JSONSerializationConfig{
			ReprConfig: config.ReprConfig,
		}

		if tuplePattern.generalElementPattern != nil {
			elementConfig.Pattern = tuplePattern.generalElementPattern
		} else {
			elementConfig.Pattern = tuplePattern.elementPatterns[i]
		}

		err = v.WriteJSONRepresentation(ctx, w, nil, elementConfig)
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte{']'})
	return err
}

func (*RuneSlice) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (slice *RuneSlice) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (*ByteSlice) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (slice *ByteSlice) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (opt Option) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	v, ok := opt.Value.(Bool)
	return ok && v == True
}

func (opt Option) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if encountered != nil && !opt.HasJSONRepresentation(encountered, config) {
		return ErrNoRepresentation
	}

	b := make([]byte, 0, len(opt.Name)+4)

	if len(opt.Name) <= 1 {
		b = append(b, '-')
	} else {
		b = append(b, '-', '-')
	}

	b = append(b, opt.Name...)

	_, err := w.Write(utils.Must(utils.MarshalJsonNoHTMLEspace(string(b))))
	if err != nil {
		return err
	}

	// _, err = w.Write([]byte{'='})
	// if err != nil {
	// 	return err
	// }
	// if err := opt.Value.WriteJSONRepresentation(ctx, w, nil); err != nil {
	// 	return err
	// }
	return nil
}

func (Path) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (pth Path) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(PATH_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(pth))
		}, w)
		return nil
	}
	w.WriteString(string(pth))
	return nil
}

func (PathPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (patt PathPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(PATHPATTERN_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(patt))
		}, w)
		return nil
	}

	w.WriteString(string(patt))
	return nil
}

func (URL) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (u URL) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(URL_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(u))
		}, w)
		return nil
	}
	w.WriteString(string(u))
	return nil
}

func (Host) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (host Host) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(HOST_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(host))
		}, w)
		return nil
	}
	w.WriteString(string(host))
	return nil
}

func (Scheme) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (scheme Scheme) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(SCHEME_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(scheme))
		}, w)
		return nil
	}
	w.WriteString(string(scheme))
	return nil
}

func (HostPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (patt HostPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(HOSTPATTERN_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(patt))
		}, w)
		return nil
	}
	w.WriteString(string(patt))
	return nil
}

func (EmailAddress) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (addr EmailAddress) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(EMAIL_ADDR_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(addr))
		}, w)
		return nil
	}
	w.WriteString(string(addr))
	return nil
}

func (URLPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (patt URLPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(URLPATTERN_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(patt))
		}, w)
		return nil
	}
	w.WriteString(string(patt))
	return nil
}

func (Identifier) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (i Identifier) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(IDENT_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(i))
		}, w)
		return nil
	}
	w.WriteString(string(i))
	return nil
}

func (PropertyName) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (p PropertyName) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	if config.Pattern == nil {
		writeUntypedValueJSON(PROPNAME_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(string(p))
		}, w)
		return nil
	}
	w.WriteString(string(p))
	return nil
}

func (CheckedString) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (str CheckedString) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (ByteCount) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (count ByteCount) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer
	if _, err := count.Write(&buff, -1); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(BYTECOUNT_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (LineCount) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (count LineCount) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer
	if count < 0 {
		return ErrNoRepresentation
	}
	if _, err := count.write(&buff); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(BYTECOUNT_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (RuneCount) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (count RuneCount) writeJSON(w *jsoniter.Stream) (int, error) {
	var buff bytes.Buffer
	count.write(&buff)

	return w.Write(utils.Must(utils.MarshalJsonNoHTMLEspace(buff.String())))
}

func (count RuneCount) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer
	if count < 0 {
		return ErrNoRepresentation
	}
	if _, err := count.write(&buff); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(RUNECOUNT_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (ByteRate) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (rate ByteRate) writeJSON(w *jsoniter.Stream) (int, error) {
	var buff bytes.Buffer
	rate.write(&buff)

	return w.Write(utils.Must(utils.MarshalJsonNoHTMLEspace(buff.String())))
}

func (rate ByteRate) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer
	if rate < 0 {
		return ErrNoRepresentation
	}
	if _, err := rate.write(&buff); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(BYTERATE_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (SimpleRate) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (rate SimpleRate) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer
	if rate < 0 {
		return ErrNoRepresentation
	}
	if _, err := rate.write(&buff); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(SIMPLERATE_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (Duration) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (d Duration) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer
	if d < 0 {
		return ErrNoRepresentation
	}
	if _, err := d.write(&buff); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(DURATION_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (Date) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (d Date) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	var buff bytes.Buffer

	if _, err := d.write(&buff); err != nil {
		return err
	}

	if config.Pattern == nil {
		writeUntypedValueJSON(DATE_PATTERN.Name, func(w *jsoniter.Stream) {
			w.WriteString(buff.String())
		}, w)
		return nil
	}
	w.WriteString(buff.String())
	return nil
}

func (FileMode) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (m FileMode) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (RuneRange) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (r RuneRange) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	w.WriteObjectStart()

	w.WriteObjectField("start")
	w.WriteString(string(r.Start))
	w.WriteMore()

	w.WriteObjectField("end")
	w.WriteString(string(r.End))

	w.WriteObjectEnd()
	return nil
}

func (r QuantityRange) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return r.HasRepresentation(encountered, config.ReprConfig)
}

func (r QuantityRange) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNotImplementedYet
}

func (IntRange) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (r IntRange) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	w.WriteObjectStart()

	w.WriteObjectField("start")
	w.WriteString(strconv.FormatInt(int64(r.Start), 10))
	w.WriteMore()

	w.WriteObjectField("end")
	w.WriteString(strconv.FormatInt(int64(r.End), 10))

	w.WriteObjectEnd()
	return nil
}

//patterns

func (ExactValuePattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (pattern ExactValuePattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (TypePattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (pattern TypePattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (*DifferencePattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (pattern *DifferencePattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (*OptionalPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (pattern *OptionalPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (RegexPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt RegexPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (UnionPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt UnionPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (SequenceStringPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt SequenceStringPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (UnionStringPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt UnionStringPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (RuneRangeStringPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt RuneRangeStringPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (DynamicStringPatternElement) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt DynamicStringPatternElement) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (RepeatedPatternElement) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt *RepeatedPatternElement) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (NamedSegmentPathPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt *NamedSegmentPathPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNotImplementedYet
}

func (ObjectPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt ObjectPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (RecordPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt RecordPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (ListPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt ListPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (TuplePattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt TuplePattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (OptionPattern) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (patt OptionPattern) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (Mimetype) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (mt Mimetype) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (FileInfo) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (i FileInfo) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (b *Bytecode) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (b *Bytecode) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNoRepresentation
}

func (Port) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return false
}

func (port Port) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return ErrNotImplementedYet
}

func (*StringConcatenation) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (c *StringConcatenation) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	return Str(c.GetOrBuildString()).WriteJSONRepresentation(ctx, w, encountered, config)
}

func (Color) HasJSONRepresentation(encountered map[uintptr]int, config JSONSerializationConfig) bool {
	return true
}

func (c Color) WriteJSONRepresentation(ctx *Context, w *jsoniter.Stream, encountered map[uintptr]int, config JSONSerializationConfig) error {
	panic(ErrNotImplementedYet)
}
