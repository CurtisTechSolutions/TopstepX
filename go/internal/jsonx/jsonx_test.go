package jsonx

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestParseTime(t *testing.T) {
	want := time.Date(2025, 7, 1, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2025-07-01T14:00:00Z", want},
		{"2025-07-01T14:00:00z", want},
		{"2025-07-01t14:00:00Z", want},
		{"2025-07-01 14:00:00Z", want},
		{"2025-07-01T14:00:00", want}, // missing offset is UTC
		{"2025-07-01T16:00:00+02:00", want},
		{"2025-07-01T16:00:00+0200", want},
		{"2025-07-01T16:00:00+02", want},
		{"2025-07-01T09:30:00-04:30", want},
		{"2025-07-01T14:00:00.1Z", want.Add(100 * time.Millisecond)},
		{"2025-07-01T14:00:00.1234567Z", want.Add(123456700)},                   // .NET precision
		{"2025-07-01T14:00:00.1234567", want.Add(123456700)},                    // .NET, no offset
		{"2025-07-01T14:00:00.123456789+00:00", want.Add(123456789)},            // nine digits
		{"2025-07-01T14:00:00.1234567891Z", want.Add(123456789)},                // extra digits truncated
		{"2025-07-01T14:00:00.000000000Z", want},                                // zero fraction
		{"2024-02-29T14:00:00Z", time.Date(2024, 2, 29, 14, 0, 0, 0, time.UTC)}, // leap day
	}
	for _, c := range cases {
		got, err := ParseTime(c.in)
		if err != nil {
			t.Errorf("ParseTime(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) || got.Location() != time.UTC {
			t.Errorf("ParseTime(%q) = %v, want %v in UTC", c.in, got, c.want)
		}
	}
}

func TestParseTimeRejects(t *testing.T) {
	for _, in := range []string{
		"", "2025-07-01", "2025-07-01T14:00", "2025-13-01T14:00:00Z", "2025-02-30T14:00:00Z",
		"2025-07-01T24:00:00Z", "2025-07-01T14:60:00Z", "2025-07-01T14:00:00.Z", "2025-07-01T14:00:00+2",
		"2025-07-01T14:00:00+24:00", "2025-07-01T14:00:00 UTC", "not a timestamp", "2025/07/01T14:00:00Z",
	} {
		if _, err := ParseTime(in); !errors.Is(err, ErrInvalidTimestamp) {
			t.Errorf("ParseTime(%q) error = %v, want ErrInvalidTimestamp", in, err)
		}
	}
}

func TestFormatTime(t *testing.T) {
	loc := time.FixedZone("x", 2*3600)
	cases := map[time.Time]string{
		time.Date(2025, 7, 1, 16, 0, 0, 0, loc):              "2025-07-01T14:00:00Z",
		time.Date(2025, 7, 1, 14, 0, 0, 123456700, time.UTC): "2025-07-01T14:00:00.1234567Z",
	}
	for in, want := range cases {
		if got := FormatTime(in); got != want {
			t.Errorf("FormatTime(%v) = %q, want %q", in, got, want)
		}
	}
}

type sample struct {
	Name     string           `json:"name"`
	Price    decimal.Decimal  `json:"price"`
	Opt      *decimal.Decimal `json:"opt,omitempty"`
	When     time.Time        `json:"when"`
	Maybe    *time.Time       `json:"maybe,omitempty"`
	Count    int32            `json:"count"`
	Volume   *int64           `json:"volume,omitempty" jsonx:"lenient"`
	Small    int32            `json:"small" jsonx:"lenient"`
	Tags     []string         `json:"tags,omitempty"`
	Nullable *decimal.Decimal `json:"nullable"`
	Ignored  string           `json:"-"`
	hidden   int
}

func (s sample) MarshalJSON() ([]byte, error)  { return Marshal(s) }
func (s *sample) UnmarshalJSON(b []byte) error { return Unmarshal(b, s) }

func TestDecimalsAreExactAndBare(t *testing.T) {
	in := `{"name":"x","price":6000.25,"opt":0.1,"when":"2025-07-01T14:00:00.5","count":3,"small":"7","nullable":null}`
	var s sample
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	if s.Price.String() != "6000.25" || s.Opt.String() != "0.1" {
		t.Fatalf("decimals decoded inexactly: %s %s", s.Price, s.Opt)
	}
	if s.Nullable != nil || s.Small != 7 {
		t.Fatalf("unexpected decode: %+v", s)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"x","price":6000.25,"opt":0.1,"when":"2025-07-01T14:00:00.5Z","count":3,"small":7,"nullable":null}`
	if string(out) != want {
		t.Fatalf("Marshal = %s\nwant      %s", out, want)
	}
	// Decimals given as strings are accepted too.
	if err := json.Unmarshal([]byte(`{"price":"12.50"}`), &s); err != nil || s.Price.String() != "12.5" {
		t.Fatalf("string decimal: %v %s", err, s.Price)
	}
}

func TestLenientInt(t *testing.T) {
	good := map[string]int64{`3`: 3, `3.0`: 3, `"3"`: 3, `"3.0"`: 3, `-2`: -2, `1e3`: 1000, `"  42 "`: 42, `9007199254740993`: 9007199254740993}
	for in, want := range good {
		got, err := LenientInt(json.RawMessage(in), 64)
		if err != nil || got != want {
			t.Errorf("LenientInt(%s) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{`3.5`, `"abc"`, `true`, `{}`, `"3.5"`, `99999999999999999999`} {
		if _, err := LenientInt(json.RawMessage(in), 64); err == nil {
			t.Errorf("LenientInt(%s) succeeded, want error", in)
		}
	}
	if _, err := LenientInt(json.RawMessage(`3000000000`), 32); err == nil {
		t.Error("LenientInt out of int32 range succeeded")
	}
}

func TestUnmarshalLeniency(t *testing.T) {
	var s sample
	// Unknown members are ignored, keys match case-insensitively, volumes are lenient.
	in := `{"NAME":"n","unknown":{"deep":[1,2]},"volume":"12.0","when":"2025-07-01T14:00:00+02:00","price":1}`
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	if s.Name != "n" || s.Volume == nil || *s.Volume != 12 || s.When.Hour() != 12 {
		t.Fatalf("unexpected: %+v", s)
	}
	for _, bad := range []string{`[]`, `"x"`, `{"when":"yesterday"}`, `{"when":5}`, `{"price":"abc"}`, `{"price":true}`, `{"count":1.5}`} {
		var v sample
		if err := json.Unmarshal([]byte(bad), &v); err == nil {
			t.Errorf("Unmarshal(%s) succeeded, want error", bad)
		}
	}
	var v sample
	if err := json.Unmarshal([]byte(`null`), &v); err != nil {
		t.Errorf("null: %v", err)
	}
	err := json.Unmarshal([]byte(`{"when":"soon"}`), &v)
	if err == nil || !strings.Contains(err.Error(), "sample.when") {
		t.Errorf("error should name the field: %v", err)
	}
}

func TestMarshalOmitsEmptyOptionals(t *testing.T) {
	out, err := Marshal(sample{Name: "a", Price: decimal.RequireFromString("-0.5"), When: time.Unix(0, 0), Ignored: "skip", hidden: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"a","price":-0.5,"when":"1970-01-01T00:00:00Z","count":0,"small":0,"nullable":null}`
	if string(out) != want {
		t.Fatalf("Marshal = %s\nwant      %s", out, want)
	}
	if _, err := Marshal(42); err == nil {
		t.Error("Marshal(non-struct) succeeded")
	}
	if b, _ := Marshal((*sample)(nil)); string(b) != "null" {
		t.Errorf("Marshal(nil) = %s", b)
	}
	if err := Unmarshal([]byte(`{}`), sample{}); err == nil {
		t.Error("Unmarshal into non-pointer succeeded")
	}
}
