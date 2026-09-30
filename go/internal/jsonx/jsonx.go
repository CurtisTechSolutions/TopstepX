// Package jsonx is the JSON codec shared by the SDK's data models.
//
// The models keep idiomatic Go field types (time.Time, decimal.Decimal) while
// the wire format follows the cross-SDK contract:
//
//   - decimals are written as bare JSON numbers, never quoted strings, and are
//     read from numbers (or numeric strings) without losing printed digits;
//   - timestamps are read leniently (see ParseTime) and written as RFC 3339 UTC
//     with a "Z" suffix;
//   - integer fields tagged `jsonx:"lenient"` also accept integral floats such
//     as 3.0 and numeric strings.
//
// Every other field is delegated to encoding/json, so nested models use their
// own MarshalJSON and UnmarshalJSON methods.
package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

var (
	timeType    = reflect.TypeOf(time.Time{})
	decimalType = reflect.TypeOf(decimal.Decimal{})
)

type fieldKind int

const (
	kindDefault fieldKind = iota
	kindTime
	kindDecimal
	kindLenientInt
)

type fieldInfo struct {
	name      string // JSON member name
	quoted    []byte // `"name":`
	index     int
	kind      fieldKind
	pointer   bool // the Go field is a pointer to the kind
	omitEmpty bool
	bits      int // integer width for kindLenientInt
}

var fieldCache sync.Map // reflect.Type -> []fieldInfo

func fieldsOf(t reflect.Type) []fieldInfo {
	if cached, ok := fieldCache.Load(t); ok {
		return cached.([]fieldInfo)
	}
	var fields []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = sf.Name
		}
		fi := fieldInfo{name: name, index: i}
		for _, o := range strings.Split(opts, ",") {
			if o == "omitempty" {
				fi.omitEmpty = true
			}
		}
		quoted, _ := json.Marshal(name)
		fi.quoted = append(quoted, ':')

		ft := sf.Type
		if ft.Kind() == reflect.Pointer {
			fi.pointer = true
			ft = ft.Elem()
		}
		switch {
		case ft == timeType:
			fi.kind = kindTime
		case ft == decimalType:
			fi.kind = kindDecimal
		case sf.Tag.Get("jsonx") == "lenient" && (ft.Kind() == reflect.Int32 || ft.Kind() == reflect.Int64):
			fi.kind = kindLenientInt
			fi.bits = ft.Bits()
		default:
			fi.pointer = false
		}
		fields = append(fields, fi)
	}
	fieldCache.Store(t, fields)
	return fields
}

func structValue(v any) (reflect.Value, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("jsonx: Unmarshal needs a non-nil pointer to a struct, got %T", v)
	}
	return rv.Elem(), nil
}

// Unmarshal decodes the JSON object in data into the struct pointed to by v.
// Unknown members are ignored and absent members leave fields untouched. A
// JSON null leaves value fields untouched and sets pointer, slice and map
// fields to nil, as encoding/json does.
func Unmarshal(data []byte, v any) error {
	s, err := structValue(v)
	if err != nil {
		return err
	}
	data = bytes.TrimSpace(data)
	if isNull(data) {
		return nil
	}
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("cannot decode %s into %s: expected a JSON object", describe(data), s.Type().Name())
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return fmt.Errorf("cannot decode %s: %w", s.Type().Name(), err)
	}
	for _, f := range fieldsOf(s.Type()) {
		raw, ok := lookup(members, f.name)
		if !ok {
			continue
		}
		if err := decodeField(s.Field(f.index), f, raw); err != nil {
			return fmt.Errorf("%s.%s: %w", s.Type().Name(), f.name, err)
		}
	}
	return nil
}

// lookup finds a member by exact name, falling back to a case-insensitive
// match like encoding/json does.
func lookup(members map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	if raw, ok := members[name]; ok {
		return raw, true
	}
	var candidates []string
	for k := range members {
		if strings.EqualFold(k, name) {
			candidates = append(candidates, k)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	sort.Strings(candidates)
	return members[candidates[0]], true
}

func isNull(raw []byte) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func describe(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "empty input"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

func decodeField(fv reflect.Value, f fieldInfo, raw json.RawMessage) error {
	if isNull(raw) {
		switch fv.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			fv.Set(reflect.Zero(fv.Type()))
		}
		return nil
	}
	target := fv
	if f.pointer {
		target = reflect.New(fv.Type().Elem()).Elem()
	}
	switch f.kind {
	case kindTime:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("timestamp must be a JSON string, got %s", describe(raw))
		}
		t, err := ParseTime(s)
		if err != nil {
			return err
		}
		target.Set(reflect.ValueOf(t))
	case kindDecimal:
		d, err := decodeDecimal(raw)
		if err != nil {
			return err
		}
		target.Set(reflect.ValueOf(d))
	case kindLenientInt:
		n, err := LenientInt(raw, f.bits)
		if err != nil {
			return err
		}
		target.SetInt(n)
	default:
		return json.Unmarshal(raw, fv.Addr().Interface())
	}
	if f.pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().Set(target)
		fv.Set(p)
	}
	return nil
}

func decodeDecimal(raw json.RawMessage) (decimal.Decimal, error) {
	raw = bytes.TrimSpace(raw)
	s := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return decimal.Decimal{}, err
		}
		s = strings.TrimSpace(s)
	} else if len(raw) == 0 || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return decimal.Decimal{}, fmt.Errorf("decimal must be a JSON number, got %s", describe(raw))
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("invalid decimal %q", s)
	}
	return d, nil
}

// LenientInt decodes an integer that may be encoded as a JSON integer, an
// integral float such as 3.0, or a numeric string. bits is the target width
// (32 or 64) used for the range check.
func LenientInt(raw json.RawMessage, bits int) (int64, error) {
	raw = bytes.TrimSpace(raw)
	s := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
		s = strings.TrimSpace(s)
	} else if len(raw) == 0 || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return 0, fmt.Errorf("integer must be a JSON number or numeric string, got %s", describe(raw))
	}
	if n, err := strconv.ParseInt(s, 10, bits); err == nil {
		return n, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", s)
	}
	if !d.IsInteger() {
		return 0, fmt.Errorf("invalid integer %q: has a fractional part", s)
	}
	bi := d.BigInt()
	if !bi.IsInt64() {
		return 0, fmt.Errorf("integer %q out of range", s)
	}
	n := bi.Int64()
	if bits == 32 && (n < math.MinInt32 || n > math.MaxInt32) {
		return 0, fmt.Errorf("integer %q out of range for int32", s)
	}
	return n, nil
}

// Marshal encodes the struct v (or pointer to struct) as a JSON object with
// members in field order. Fields tagged omitempty are skipped when empty.
func Marshal(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return []byte("null"), nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("jsonx: Marshal needs a struct, got %T", v)
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for _, f := range fieldsOf(rv.Type()) {
		fv := rv.Field(f.index)
		if f.omitEmpty && isEmpty(fv) {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.Write(f.quoted)
		if err := encodeField(&buf, fv, f); err != nil {
			return nil, fmt.Errorf("%s.%s: %w", rv.Type().Name(), f.name, err)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func encodeField(buf *bytes.Buffer, fv reflect.Value, f fieldInfo) error {
	if f.kind == kindDefault {
		b, err := json.Marshal(fv.Interface())
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	}
	if f.pointer {
		if fv.IsNil() {
			buf.WriteString("null")
			return nil
		}
		fv = fv.Elem()
	}
	switch f.kind {
	case kindTime:
		buf.WriteByte('"')
		buf.WriteString(FormatTime(fv.Interface().(time.Time)))
		buf.WriteByte('"')
	case kindDecimal:
		buf.WriteString(fv.Interface().(decimal.Decimal).String())
	case kindLenientInt:
		buf.WriteString(strconv.FormatInt(fv.Int(), 10))
	}
	return nil
}

func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

// FormatTime formats t as RFC 3339 in UTC with a "Z" suffix and only the
// fractional digits needed.
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// ErrInvalidTimestamp is wrapped by every ParseTime error.
var ErrInvalidTimestamp = errors.New("invalid timestamp")

// ParseTime parses an RFC 3339 style timestamp leniently and returns it in UTC.
//
// It accepts a "T", "t" or space separator, 0 to 9 fractional-second digits
// (further digits are truncated), and a "Z" suffix, a numeric offset
// (+hh:mm, +hhmm or +hh) or no offset at all, which is read as UTC.
func ParseTime(s string) (time.Time, error) {
	bad := func() (time.Time, error) {
		return time.Time{}, fmt.Errorf("%w %q", ErrInvalidTimestamp, s)
	}
	if len(s) < 19 {
		return bad()
	}
	year, ok1 := digits(s[0:4])
	month, ok2 := digits(s[5:7])
	day, ok3 := digits(s[8:10])
	hour, ok4 := digits(s[11:13])
	minute, ok5 := digits(s[14:16])
	sec, ok6 := digits(s[17:19])
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) || s[4] != '-' || s[7] != '-' ||
		(s[10] != 'T' && s[10] != 't' && s[10] != ' ') || s[13] != ':' || s[16] != ':' {
		return bad()
	}
	rest := s[19:]
	nanos := 0
	if len(rest) > 0 && (rest[0] == '.' || rest[0] == ',') {
		n := 1
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		frac := rest[1:n]
		if len(frac) == 0 {
			return bad()
		}
		if len(frac) > 9 {
			frac = frac[:9]
		}
		v, _ := strconv.Atoi(frac)
		for i := len(frac); i < 9; i++ {
			v *= 10
		}
		nanos = v
		rest = rest[n:]
	}
	offset := 0
	switch {
	case rest == "" || rest == "Z" || rest == "z":
	case rest[0] == '+' || rest[0] == '-':
		zone := rest[1:]
		var hh, mm int
		var ok bool
		switch len(zone) {
		case 2:
			hh, ok = digits(zone)
		case 4:
			var okH, okM bool
			hh, okH = digits(zone[0:2])
			mm, okM = digits(zone[2:4])
			ok = okH && okM
		case 5:
			var okH, okM bool
			hh, okH = digits(zone[0:2])
			mm, okM = digits(zone[3:5])
			ok = okH && okM && zone[2] == ':'
		}
		if !ok || hh > 23 || mm > 59 {
			return bad()
		}
		offset = hh*3600 + mm*60
		if rest[0] == '-' {
			offset = -offset
		}
	default:
		return bad()
	}
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || sec > 60 {
		return bad()
	}
	t := time.Date(year, time.Month(month), day, hour, minute, sec, nanos, time.UTC)
	if t.Day() != day && sec != 60 {
		return bad() // day out of range for the month
	}
	return t.Add(-time.Duration(offset) * time.Second), nil
}

func digits(s string) (int, bool) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, len(s) > 0
}
