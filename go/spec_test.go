package topstepx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/CurtisTechSolutions/TopstepX/go/internal/jsonx"
	"github.com/shopspring/decimal"
)

// Spec conformance tests (contract section 12.1) against
// spec/projectx-openapi.json.

const specPath = "../spec/projectx-openapi.json"

type specDoc struct {
	paths   map[string]any
	schemas map[string]any
}

func loadSpec(t *testing.T) *specDoc {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	root := decodeNumbers(t, raw).(map[string]any)
	return &specDoc{
		paths:   root["paths"].(map[string]any),
		schemas: root["components"].(map[string]any)["schemas"].(map[string]any),
	}
}

func decodeNumbers(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return v
}

// schemaGoTypes maps every object schema to its Go model.
var schemaGoTypes = map[string]reflect.Type{
	"SearchAccountResponse":               reflect.TypeOf(SearchAccountResponse{}),
	"TradingAccountModel":                 reflect.TypeOf(TradingAccount{}),
	"SearchAccountRequest":                reflect.TypeOf(SearchAccountRequest{}),
	"LoginResponse":                       reflect.TypeOf(LoginResponse{}),
	"LoginAppRequest":                     reflect.TypeOf(LoginAppRequest{}),
	"LoginApiKeyRequest":                  reflect.TypeOf(LoginAPIKeyRequest{}),
	"LogoutResponse":                      reflect.TypeOf(LogoutResponse{}),
	"ValidateResponse":                    reflect.TypeOf(ValidateResponse{}),
	"SearchContractResponse":              reflect.TypeOf(SearchContractResponse{}),
	"ContractModel":                       reflect.TypeOf(Contract{}),
	"SearchContractRequest":               reflect.TypeOf(SearchContractRequest{}),
	"SearchContractByIdResponse":          reflect.TypeOf(SearchContractByIDResponse{}),
	"SearchContractByIdRequest":           reflect.TypeOf(SearchContractByIDRequest{}),
	"ListAvailableContractResponse":       reflect.TypeOf(ListAvailableContractResponse{}),
	"ListAvailableContractRequest":        reflect.TypeOf(ListAvailableContractRequest{}),
	"RetrieveBarResponse":                 reflect.TypeOf(RetrieveBarResponse{}),
	"AggregateBarModel":                   reflect.TypeOf(AggregateBar{}),
	"RetrieveBarRequest":                  reflect.TypeOf(RetrieveBarRequest{}),
	"SearchOrderResponse":                 reflect.TypeOf(SearchOrderResponse{}),
	"OrderModel":                          reflect.TypeOf(Order{}),
	"SearchOrderRequest":                  reflect.TypeOf(SearchOrderRequest{}),
	"SearchOrderByIdResponse":             reflect.TypeOf(SearchOrderByIDResponse{}),
	"SearchOrderByIdRequest":              reflect.TypeOf(SearchOrderByIDRequest{}),
	"SearchOpenOrderRequest":              reflect.TypeOf(SearchOpenOrderRequest{}),
	"SearchOrdersQueryResponse":           reflect.TypeOf(SearchOrdersQueryResponse{}),
	"ProblemDetails":                      reflect.TypeOf(ProblemDetails{}),
	"SearchOrdersQueryRequest":            reflect.TypeOf(SearchOrdersQueryRequest{}),
	"GatewayOrderFilter":                  reflect.TypeOf(GatewayOrderFilter{}),
	"PlaceOrderResponse":                  reflect.TypeOf(PlaceOrderResponse{}),
	"PlaceOrderRequest":                   reflect.TypeOf(PlaceOrderRequest{}),
	"PlaceOrderBracket":                   reflect.TypeOf(PlaceOrderBracket{}),
	"CancelOrderResponse":                 reflect.TypeOf(CancelOrderResponse{}),
	"CancelOrderRequest":                  reflect.TypeOf(CancelOrderRequest{}),
	"ModifyOrderResponse":                 reflect.TypeOf(ModifyOrderResponse{}),
	"ModifyOrderRequest":                  reflect.TypeOf(ModifyOrderRequest{}),
	"SearchPositionResponse":              reflect.TypeOf(SearchPositionResponse{}),
	"PositionModel":                       reflect.TypeOf(Position{}),
	"SearchPositionRequest":               reflect.TypeOf(SearchPositionRequest{}),
	"ClosePositionResponse":               reflect.TypeOf(ClosePositionResponse{}),
	"CloseContractPositionRequest":        reflect.TypeOf(CloseContractPositionRequest{}),
	"PartialClosePositionResponse":        reflect.TypeOf(PartialClosePositionResponse{}),
	"PartialCloseContractPositionRequest": reflect.TypeOf(PartialCloseContractPositionRequest{}),
	"SearchHalfTradeResponse":             reflect.TypeOf(SearchHalfTradeResponse{}),
	"HalfTradeModel":                      reflect.TypeOf(HalfTrade{}),
	"SearchTradeRequest":                  reflect.TypeOf(SearchTradeRequest{}),
}

type enumConstant interface {
	String() string
	IsKnown() bool
}

func refName(ref string) string { return strings.TrimPrefix(ref, "#/components/schemas/") }

func (s *specDoc) schema(name string) map[string]any { return s.schemas[name].(map[string]any) }

// roundTrip decodes raw into a new value of typ, re-encodes it and returns
// the decoded value and the re-encoded JSON.
func roundTrip(t *testing.T, typ reflect.Type, raw []byte) (any, []byte) {
	t.Helper()
	v := reflect.New(typ)
	if err := json.Unmarshal(raw, v.Interface()); err != nil {
		t.Fatalf("decode %s from %s: %v", typ.Name(), raw, err)
	}
	out, err := json.Marshal(v.Interface())
	if err != nil {
		t.Fatalf("encode %s: %v", typ.Name(), err)
	}
	return v.Interface(), out
}

// equivalent compares JSON values: null and absent are equal, numbers compare
// by value, and strings that are both timestamps compare by instant. A number
// never equals a string, so quoted decimals fail.
func equivalent(want, got any, path string) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want object, got %T", path, got)
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		for k := range keys {
			wv, wok := w[k]
			gv, gok := g[k]
			if wv == nil && gv == nil {
				continue // null and absent are equal
			}
			if !wok {
				return fmt.Errorf("%s.%s: unexpected member %v", path, k, gv)
			}
			if !gok {
				return fmt.Errorf("%s.%s: missing member (want %v)", path, k, wv)
			}
			if err := equivalent(wv, gv, path+"."+k); err != nil {
				return err
			}
		}
		return nil
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s: want array of %d, got %v", path, len(w), got)
		}
		for i := range w {
			if err := equivalent(w[i], g[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case json.Number:
		g, ok := got.(json.Number)
		if !ok {
			return fmt.Errorf("%s: want number %s, got %T %v", path, w, got, got)
		}
		wd, err1 := decimal.NewFromString(w.String())
		gd, err2 := decimal.NewFromString(g.String())
		if err1 != nil || err2 != nil || !wd.Equal(gd) {
			return fmt.Errorf("%s: want %s, got %s", path, w, g)
		}
		return nil
	case string:
		g, ok := got.(string)
		if !ok {
			return fmt.Errorf("%s: want string %q, got %T %v", path, w, got, got)
		}
		if w == g {
			return nil
		}
		wt, err1 := jsonx.ParseTime(w)
		gt, err2 := jsonx.ParseTime(g)
		if err1 == nil && err2 == nil && wt.Equal(gt) {
			return nil
		}
		return fmt.Errorf("%s: want %q, got %q", path, w, g)
	case bool:
		if g, ok := got.(bool); !ok || g != w {
			return fmt.Errorf("%s: want %v, got %v", path, w, got)
		}
		return nil
	case nil:
		if got != nil {
			return fmt.Errorf("%s: want null, got %v", path, got)
		}
		return nil
	}
	return fmt.Errorf("%s: unsupported JSON value %T", path, want)
}

func assertEquivalentJSON(t *testing.T, want, got []byte, what string) {
	t.Helper()
	if err := equivalent(decodeNumbers(t, want), decodeNumbers(t, got), what); err != nil {
		t.Errorf("%v\nwant: %s\ngot:  %s", err, want, got)
	}
}

func TestSpecOperations(t *testing.T) {
	spec := loadSpec(t)
	writes := map[OperationID]bool{
		OpAuthLoginApp: true, OpAuthLoginKey: true, OpAuthLogout: true, OpAuthValidate: true,
		OpOrderPlaceOrder: true, OpOrderCancelOrder: true, OpOrderModifyOrder: true,
		OpPositionCloseContractPosition: true, OpPositionPartialCloseContractPosition: true,
	}
	seen := map[OperationID]bool{}
	for path, item := range spec.paths {
		for method, raw := range item.(map[string]any) {
			op := raw.(map[string]any)
			id := OperationID(op["operationId"].(string))
			seen[id] = true
			info, ok := id.Info()
			if !ok {
				t.Errorf("operation %s is not in the operation table", id)
				continue
			}
			if info.Method != strings.ToUpper(method) || info.Path != path {
				t.Errorf("%s: got %s %s, spec says %s %s", id, info.Method, info.Path, strings.ToUpper(method), path)
			}
			wantAuth := AuthBearer
			if sec, ok := op["security"].([]any); ok && len(sec) == 0 {
				wantAuth = AuthNone
			}
			if id == OpStatusPing {
				wantAuth = AuthBearerIfAvailable
			}
			if info.Auth != wantAuth {
				t.Errorf("%s: auth %s, want %s", id, info.Auth, wantAuth)
			}
			wantBucket := RateBucketGeneral
			if id == OpHistoryGetBars {
				wantBucket = RateBucketHistory
			}
			if info.RateBucket != wantBucket {
				t.Errorf("%s: rate bucket %s, want %s", id, info.RateBucket, wantBucket)
			}
			wantRetry := RetryRead
			if writes[id] {
				wantRetry = RetryWrite
			}
			if info.RetryClass != wantRetry {
				t.Errorf("%s: retry class %s, want %s", id, info.RetryClass, wantRetry)
			}
			if _, hasBody := op["requestBody"]; hasBody != info.HasBody {
				t.Errorf("%s: HasBody %v, spec requestBody present %v", id, info.HasBody, hasBody)
			}
		}
	}
	if len(seen) != 21 || len(Operations()) != 21 {
		t.Errorf("spec has %d operations and the table %d; want 21 each", len(seen), len(Operations()))
	}
}

func TestSpecEnums(t *testing.T) {
	spec := loadSpec(t)
	count := 0
	for name, raw := range spec.schemas {
		sc := raw.(map[string]any)
		values, ok := sc["enum"].([]any)
		if !ok {
			continue
		}
		count++
		names := sc["x-enumNames"].([]any)
		consts, ok := specEnumConstants[name]
		if !ok {
			t.Errorf("enum %s has no Go constants", name)
			continue
		}
		if len(consts) != len(values) || len(names) != len(values) {
			t.Errorf("enum %s: %d Go constants, %d spec values, %d names", name, len(consts), len(values), len(names))
			continue
		}
		for i, c := range consts {
			wantValue, _ := values[i].(json.Number).Int64()
			gotValue := reflect.ValueOf(c).Int()
			if gotValue != wantValue || c.String() != names[i].(string) || !c.IsKnown() {
				t.Errorf("enum %s[%d]: Go (%d, %q), spec (%d, %q)", name, i, gotValue, c.String(), wantValue, names[i])
			}
		}
		// Values the spec does not list are tolerated and named Unrecognized(n).
		typ := reflect.TypeOf(consts[0])
		for _, n := range []int64{int64(len(values)), -1, 99} {
			v := reflect.New(typ).Elem()
			v.SetInt(n)
			c := v.Interface().(enumConstant)
			if want := fmt.Sprintf("Unrecognized(%d)", n); c.String() != want || c.IsKnown() {
				t.Errorf("enum %s value %d: String %q IsKnown %v, want %q false", name, n, c.String(), c.IsKnown(), want)
			}
		}
	}
	if count != 24 || len(specEnumConstants) != 24 {
		t.Errorf("spec has %d enums and the table %d; want 24", count, len(specEnumConstants))
	}
}

// jsonFieldNames returns the JSON member names of a Go model.
func jsonFieldNames(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "-" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestSpecModelFields(t *testing.T) {
	spec := loadSpec(t)
	objects := 0
	for name, raw := range spec.schemas {
		sc := raw.(map[string]any)
		if sc["type"] != "object" {
			continue
		}
		objects++
		typ, ok := schemaGoTypes[name]
		if !ok {
			t.Errorf("object schema %s has no Go model", name)
			continue
		}
		var want []string
		for prop := range sc["properties"].(map[string]any) {
			want = append(want, prop)
		}
		sort.Strings(want)
		if got := jsonFieldNames(typ); !reflect.DeepEqual(got, want) {
			t.Errorf("%s (%s): JSON members %v, spec properties %v", name, typ.Name(), got, want)
		}
	}
	if objects != 45 || len(schemaGoTypes) != 45 {
		t.Errorf("spec has %d object schemas and the table %d; want 45", objects, len(schemaGoTypes))
	}
}

// synth generates a value for schema that holds every property and passes
// the client-side validation rules.
func (s *specDoc) synth(schema map[string]any, path string) any {
	if ref, ok := schema["$ref"].(string); ok {
		return s.synth(s.schema(refName(ref)), path)
	}
	// An object's allOf holds conditionals (if/then); every property is
	// generated anyway, which satisfies them.
	if parts, ok := schema["allOf"].([]any); ok && schema["type"] != "object" {
		var allowed []any
		for _, p := range parts {
			if e, ok := p.(map[string]any)["enum"].([]any); ok {
				allowed = e
			}
		}
		if allowed != nil {
			return allowed[len(allowed)-1]
		}
		return s.synth(parts[0].(map[string]any), path)
	}
	if alts, ok := schema["anyOf"].([]any); ok {
		for _, a := range alts {
			if alt := a.(map[string]any); alt["type"] != "null" {
				return s.synth(alt, path)
			}
		}
	}
	if values, ok := schema["enum"].([]any); ok {
		return values[len(values)-1]
	}
	typ := schema["type"]
	if list, ok := typ.([]any); ok {
		for _, x := range list {
			if x != "null" {
				typ = x
			}
		}
	}
	switch typ {
	case "object":
		obj := map[string]any{}
		for name, p := range schema["properties"].(map[string]any) {
			obj[name] = s.synth(p.(map[string]any), path+"."+name)
		}
		if _, ok := schema["additionalProperties"]; ok {
			obj["traceId"] = "00-0af7651916cd43dd8448eb211c80319c-01"
			obj["errors"] = map[string]any{"filter.accountId": []any{"must be positive"}}
		}
		return obj
	case "array":
		return []any{s.synth(schema["items"].(map[string]any), path+"[]")}
	case "integer":
		v := int64(42)
		if schema["format"] == "int64" {
			v = 9007199254740993 // not representable as float64
		}
		if maxV, ok := schema["maximum"].(json.Number); ok {
			if m, _ := maxV.Int64(); v > m {
				v = m
			}
		}
		return json.Number(strconv.FormatInt(v, 10))
	case "number":
		return json.Number("6000.123456789012345678")
	case "boolean":
		return true
	case "string":
		switch schema["format"] {
		case "date-time":
			return "2025-07-01T14:00:00.1234567Z"
		case "date":
			return "2025-07-01"
		}
		return "sample" + strings.ReplaceAll(path, ".", "_")
	}
	panic(fmt.Sprintf("synth %s: unsupported schema %v", path, schema))
}

func TestSpecSynthesizedRoundTrip(t *testing.T) {
	spec := loadSpec(t)
	for name, raw := range spec.schemas {
		sc := raw.(map[string]any)
		if sc["type"] != "object" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			typ := schemaGoTypes[name]
			if typ == nil {
				t.Fatalf("no Go model for %s", name)
			}
			in, err := json.Marshal(spec.synth(sc, name))
			if err != nil {
				t.Fatal(err)
			}
			v, out := roundTrip(t, typ, in)
			assertEquivalentJSON(t, in, out, name)
			if val, ok := v.(validator); ok {
				if err := val.Validate(); err != nil {
					t.Errorf("synthesized %s fails validation: %v", name, err)
				}
			}
		})
	}
}

// specExample is one request or HTTP 200 response example.
type specExample struct {
	op     OperationID
	path   string
	name   string
	schema string // "" for Status_Ping's string response
	value  []byte
}

func (s *specDoc) examples(t *testing.T) (requests, responses []specExample) {
	t.Helper()
	collect := func(op OperationID, path string, content map[string]any, into *[]specExample) {
		media, ok := content["application/json"].(map[string]any)
		if !ok {
			return
		}
		schemaName := ""
		if ref, ok := media["schema"].(map[string]any)["$ref"].(string); ok {
			schemaName = refName(ref)
		}
		exs, _ := media["examples"].(map[string]any)
		for exName, ex := range exs {
			value, err := json.Marshal(ex.(map[string]any)["value"])
			if err != nil {
				t.Fatal(err)
			}
			*into = append(*into, specExample{op: op, path: path, name: exName, schema: schemaName, value: value})
		}
	}
	for path, item := range s.paths {
		for _, raw := range item.(map[string]any) {
			op := raw.(map[string]any)
			id := OperationID(op["operationId"].(string))
			if rb, ok := op["requestBody"].(map[string]any); ok {
				collect(id, path, rb["content"].(map[string]any), &requests)
			}
			ok200 := op["responses"].(map[string]any)["200"].(map[string]any)
			collect(id, path, ok200["content"].(map[string]any), &responses)
		}
	}
	return requests, responses
}

func TestSpecExamplesRoundTrip(t *testing.T) {
	spec := loadSpec(t)
	requests, responses := spec.examples(t)
	for _, ex := range append(append([]specExample(nil), requests...), responses...) {
		if ex.schema == "" {
			if ex.op != OpStatusPing || pingText(ex.value) != "pong" {
				t.Errorf("%s %s: unexpected string example %s", ex.op, ex.name, ex.value)
			}
			continue
		}
		typ := schemaGoTypes[ex.schema]
		v, out := roundTrip(t, typ, ex.value)
		assertEquivalentJSON(t, ex.value, out, string(ex.op)+"/"+ex.name)
		if val, ok := v.(validator); ok {
			if err := val.Validate(); err != nil {
				t.Errorf("%s %s: example fails validation: %v", ex.op, ex.name, err)
			}
		}
	}
	report := readValidationReport(t)
	if got := len(requests) + len(responses); got != report.Examples {
		t.Errorf("round-tripped %d examples; validation-report.json lists %d", got, report.Examples)
	}
}

// TestSpecOperationsEndToEnd calls every high-level method against a mock
// server that answers with the spec's illustrative 200 example. It checks the
// HTTP method, path and body sent (against the request example) and that the
// response decodes.
func TestSpecOperationsEndToEnd(t *testing.T) {
	spec := loadSpec(t)
	requests, responses := spec.examples(t)
	reqByOp := map[OperationID][]byte{}
	for _, ex := range requests {
		if ex.name == "illustrative" {
			reqByOp[ex.op] = ex.value
		}
	}
	respByPath := map[string][]byte{}
	for _, ex := range responses {
		if ex.name == "illustrative" {
			respByPath[ex.path] = ex.value
		}
	}

	var mu sync.Mutex
	type seenRequest struct {
		method, auth string
		body         []byte
	}
	seen := map[string]seenRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen[r.URL.Path] = seenRequest{method: r.Method, auth: r.Header.Get("Authorization"), body: body}
		mu.Unlock()
		resp, ok := respByPath[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer srv.Close()

	decode := func(op OperationID, v any) {
		if err := json.Unmarshal(reqByOp[op], v); err != nil {
			t.Fatalf("%s request example: %v", op, err)
		}
	}
	calls := map[OperationID]func(context.Context, *Client) (any, error){
		OpAccountSearchAccounts: func(ctx context.Context, c *Client) (any, error) {
			var r SearchAccountRequest
			decode(OpAccountSearchAccounts, &r)
			return c.Accounts.Search(ctx, r.OnlyActiveAccounts)
		},
		OpAuthLoginApp: func(ctx context.Context, c *Client) (any, error) {
			var r LoginAppRequest
			decode(OpAuthLoginApp, &r)
			return c.Auth.LoginApp(ctx, r)
		},
		OpAuthLoginKey: func(ctx context.Context, c *Client) (any, error) {
			var r LoginAPIKeyRequest
			decode(OpAuthLoginKey, &r)
			return c.Auth.LoginKey(ctx, r.UserName, r.APIKey)
		},
		OpAuthLogout:   func(ctx context.Context, c *Client) (any, error) { return nil, c.Auth.Logout(ctx) },
		OpAuthValidate: func(ctx context.Context, c *Client) (any, error) { return c.Auth.Validate(ctx) },
		OpContractSearchContracts: func(ctx context.Context, c *Client) (any, error) {
			var r SearchContractRequest
			decode(OpContractSearchContracts, &r)
			return c.Contracts.Search(ctx, r.SearchText, r.Live)
		},
		OpContractSearchContractByID: func(ctx context.Context, c *Client) (any, error) {
			var r SearchContractByIDRequest
			decode(OpContractSearchContractByID, &r)
			return c.Contracts.SearchByID(ctx, r.ContractID)
		},
		OpContractAvailableContracts: func(ctx context.Context, c *Client) (any, error) {
			var r ListAvailableContractRequest
			decode(OpContractAvailableContracts, &r)
			return c.Contracts.Available(ctx, r.Live)
		},
		OpHistoryGetBars: func(ctx context.Context, c *Client) (any, error) {
			var r RetrieveBarRequest
			decode(OpHistoryGetBars, &r)
			return c.History.RetrieveBars(ctx, r)
		},
		OpOrderSearchOrders: func(ctx context.Context, c *Client) (any, error) {
			var r SearchOrderRequest
			decode(OpOrderSearchOrders, &r)
			return c.Orders.Search(ctx, r)
		},
		OpOrderSearchOrderByID: func(ctx context.Context, c *Client) (any, error) {
			var r SearchOrderByIDRequest
			decode(OpOrderSearchOrderByID, &r)
			return c.Orders.SearchByID(ctx, r.AccountID, r.OrderID)
		},
		OpOrderSearchOpenOrders: func(ctx context.Context, c *Client) (any, error) {
			var r SearchOpenOrderRequest
			decode(OpOrderSearchOpenOrders, &r)
			return c.Orders.SearchOpen(ctx, r.AccountID)
		},
		OpOrderSearchOrdersQuery: func(ctx context.Context, c *Client) (any, error) {
			var r SearchOrdersQueryRequest
			decode(OpOrderSearchOrdersQuery, &r)
			return c.Orders.Query(ctx, r)
		},
		OpOrderPlaceOrder: func(ctx context.Context, c *Client) (any, error) {
			var r PlaceOrderRequest
			decode(OpOrderPlaceOrder, &r)
			return c.Orders.Place(ctx, r)
		},
		OpOrderCancelOrder: func(ctx context.Context, c *Client) (any, error) {
			var r CancelOrderRequest
			decode(OpOrderCancelOrder, &r)
			return nil, c.Orders.Cancel(ctx, r.AccountID, r.OrderID)
		},
		OpOrderModifyOrder: func(ctx context.Context, c *Client) (any, error) {
			var r ModifyOrderRequest
			decode(OpOrderModifyOrder, &r)
			return nil, c.Orders.Modify(ctx, r)
		},
		OpPositionSearchOpenPositions: func(ctx context.Context, c *Client) (any, error) {
			var r SearchPositionRequest
			decode(OpPositionSearchOpenPositions, &r)
			return c.Positions.SearchOpen(ctx, r.AccountID)
		},
		OpPositionCloseContractPosition: func(ctx context.Context, c *Client) (any, error) {
			var r CloseContractPositionRequest
			decode(OpPositionCloseContractPosition, &r)
			return nil, c.Positions.Close(ctx, r.AccountID, r.ContractID)
		},
		OpPositionPartialCloseContractPosition: func(ctx context.Context, c *Client) (any, error) {
			var r PartialCloseContractPositionRequest
			decode(OpPositionPartialCloseContractPosition, &r)
			return nil, c.Positions.PartialClose(ctx, r.AccountID, r.ContractID, r.Size)
		},
		OpStatusPing: func(ctx context.Context, c *Client) (any, error) { return c.Status.Ping(ctx) },
		OpTradeSearchHalfTurnTrades: func(ctx context.Context, c *Client) (any, error) {
			var r SearchTradeRequest
			decode(OpTradeSearchHalfTurnTrades, &r)
			return c.Trades.Search(ctx, r)
		},
	}
	if len(calls) != 21 {
		t.Fatalf("calls cover %d operations, want 21", len(calls))
	}
	for _, info := range Operations() {
		t.Run(string(info.ID), func(t *testing.T) {
			c, err := NewClient(WithBaseURL(srv.URL), WithToken("EXAMPLE_TOKEN"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := calls[info.ID](context.Background(), c)
			if err != nil {
				t.Fatalf("%s: %v", info.ID, err)
			}
			if rv := reflect.ValueOf(result); result != nil && rv.Kind() == reflect.Slice && rv.Len() == 0 {
				t.Errorf("%s returned an empty list", info.ID)
			}
			mu.Lock()
			got, ok := seen[info.Path]
			mu.Unlock()
			if !ok || got.method != info.Method {
				t.Fatalf("%s: server saw %+v, want %s %s", info.ID, got, info.Method, info.Path)
			}
			if info.HasBody {
				assertEquivalentJSON(t, reqByOp[info.ID], got.body, string(info.ID)+" request")
			} else if len(got.body) != 0 {
				t.Errorf("%s: body %q, want empty", info.ID, got.body)
			}
			if wantAuth := info.Auth != AuthNone; wantAuth != (got.auth == "Bearer EXAMPLE_TOKEN") {
				t.Errorf("%s: Authorization %q", info.ID, got.auth)
			}
		})
	}
}

type validationReport struct {
	Examples      int `json:"examples"`
	NegativeCases []struct {
		Name string `json:"name"`
	} `json:"negativeCases"`
}

func readValidationReport(t *testing.T) validationReport {
	t.Helper()
	raw, err := os.ReadFile("../spec/validation-report.json")
	if err != nil {
		t.Fatal(err)
	}
	var r validationReport
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}
