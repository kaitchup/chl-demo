package upayopen

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

// fakeUPay verifies the signature over the decrypted params, like UPay does,
// and answers with data encrypted to the merchant public key.
type fakeUPay struct {
	t            *testing.T
	secret       string
	platformPriv *rsa.PrivateKey
	merchantPub  *rsa.PublicKey
	gotParams    map[string]any
	gotSignParam string
	respond      func(uri string) (code int, data any)
}

func (f *fakeUPay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var plain []byte
	var env struct {
		Payload string `json:"payload"`
	}
	if json.Unmarshal(body, &env) == nil && env.Payload != "" {
		var err error
		if plain, err = Decrypt(env.Payload, f.platformPriv); err != nil {
			f.t.Fatalf("server decrypt: %v", err)
		}
	} else {
		plain = body
	}
	f.gotParams = map[string]any{}
	if len(plain) > 0 {
		d := json.NewDecoder(strings.NewReader(string(plain)))
		d.UseNumber()
		_ = d.Decode(&f.gotParams)
	}
	f.gotSignParam = SignParam(f.gotParams)
	want := Sign(f.secret, "POST|"+r.URL.Path+"|"+r.Header.Get("X-UPA-TIMESTAMP")+"|"+
		r.Header.Get("X-UPA-REQUESTID")+"|"+f.gotSignParam)
	if r.Header.Get("X-UPA-SIGN") != want {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1005, "msg": "Signature error", "success": false})
		return
	}
	code, data := f.respond(r.URL.Path)
	var out any = data
	if env.Payload != "" && code == 0 {
		b, _ := json.Marshal(data)
		enc, _ := jose.NewEncrypter(jose.A128GCM, jose.Recipient{Algorithm: jose.RSA_OAEP_256, Key: f.merchantPub}, nil)
		obj, _ := enc.Encrypt(b)
		s, _ := obj.CompactSerialize()
		out = map[string]string{"payload": s}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": "x", "success": true, "data": out})
}

func newTestClient(t *testing.T) (*Client, *fakeUPay) {
	t.Helper()
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	pubDER, _ := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	privDER, _ := x509.MarshalPKCS8PrivateKey(merchant)
	f := &fakeUPay{t: t, secret: "s3cr$t", platformPriv: platform, merchantPub: &merchant.PublicKey}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "key", f.secret,
		base64.StdEncoding.EncodeToString(pubDER), base64.StdEncoding.EncodeToString(privDER))
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func TestCreateOrderEncryptedRoundTrip(t *testing.T) {
	c, f := newTestClient(t)
	f.respond = func(string) (int, any) {
		return 0, map[string]any{"orderNo": "ORD1", "orderStatus": 0, "informationToImprove": []any{}}
	}
	res, err := c.CreateOrder(context.Background(), CreateOrderReq{
		DebitCoinID: "458884", ThirdOrderNo: "PO1", PayerNo: "P1", BankAccountNo: "B1",
		Amount: "20.00", RemitMethod: "swift", RemitUsage: "Family support", RemitNote: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OrderNo != "ORD1" {
		t.Fatalf("res = %+v", res)
	}
	// amount must stay "20.00" in both body and signature
	if !strings.Contains(f.gotSignParam, "amount=20.00&") {
		t.Fatalf("sign param = %s", f.gotSignParam)
	}
}

func TestAPIErrorAndQuoteParsing(t *testing.T) {
	c, f := newTestClient(t)
	f.respond = func(uri string) (int, any) {
		if strings.HasSuffix(uri, "/creation") {
			return 8009, nil
		}
		// numbers where strings are documented and vice versa, as UPay really returns
		return 0, map[string]any{"orderNo": "ORD1", "status": 2, "quoteList": []any{map[string]any{
			"quoteNo": "Q1", "status": 3, "debitAmount": "68.06", "destinationAmount": 20,
			"fixedFee": "10", "transactionFee": "3", "exchangeFee": "0", "validUntil": "1790318307000",
			"remitMethod": "swift",
		}}}
	}
	_, err := c.CreateOrder(context.Background(), CreateOrderReq{Amount: "2"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 8009 {
		t.Fatalf("err = %v", err)
	}
	q, err := c.QuoteInfo(context.Background(), "PO1")
	if err != nil {
		t.Fatal(err)
	}
	got := q.QuoteList[0]
	if got.DestinationAmount != "20" || got.ValidUntil.Millis() != 1790318307000 || got.RemitMethod != "swift" {
		t.Fatalf("quote = %+v", got)
	}
}

func TestFormJSONMatchesUPayShape(t *testing.T) {
	got := NewForm("person").
		Group("base", F("name", "A B"), F("empty", "")).
		MultiGroup("document", F("type", "passport")).
		Group("allEmpty", F("x", "")).
		JSON()
	want := `{"code":"person","group":[{"code":"base","fields":[{"fieldKey":"name","fieldValue":"A B"}]},` +
		`{"code":"document","fields":{"0":[{"fieldKey":"type","fieldValue":"passport"}]}}]}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestInfoFlatten(t *testing.T) {
	var in Info
	_ = json.Unmarshal([]byte(`{"person":{"base":[{"fieldKey":"name","fieldValue":"Jane"}],
		"document":{"0":[{"fieldKey":"type","fieldValue":"passport"}]}}}`), &in)
	m := in.Flatten()
	if m["base.name"] != "Jane" || m["document.type"] != "passport" {
		t.Fatalf("flatten = %v", m)
	}
}
