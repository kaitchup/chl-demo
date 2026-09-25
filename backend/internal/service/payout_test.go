package service

import (
	"encoding/json"
	"errors"
	"testing"
)

func validPayer() PayerInput {
	return PayerInput{
		GivenName: "Jane", FamilyName: "Doe", DateOfBirth: "1990-01-15", Nationality: "ae", Gender: "Female",
		Email: "jane@example.com", CallingCode: "+971", Phone: "501234567",
		DocType: "driving_license", DocNumber: "D123", DocIssuedDate: "2020-01-01", DocExpiryType: "dated",
		DocExpiryDate: "2030-01-01", IdentityFileID: "f1", IdentityFileName: "id.jpg",
		AddrCountry: "AE", AddrProvince: "Dubai", AddrCity: "Dubai", AddrLine1: "Al Barsha 1", AddrPostalCode: "00000",
		AddressProofFileID: "f2", EmploymentStatus: "employed",
	}
}

// fields flattens formData into group.key → value for assertions.
func fields(t *testing.T, form string) map[string]string {
	t.Helper()
	var f struct {
		Group []struct {
			Code   string          `json:"code"`
			Fields json.RawMessage `json:"fields"`
		} `json:"group"`
	}
	if err := json.Unmarshal([]byte(form), &f); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, g := range f.Group {
		var list []struct{ FieldKey, FieldValue string }
		if json.Unmarshal(g.Fields, &list) != nil {
			var multi map[string][]struct{ FieldKey, FieldValue string }
			_ = json.Unmarshal(g.Fields, &multi)
			list = multi["0"]
		}
		for _, x := range list {
			out[g.Code+"."+x.FieldKey] = x.FieldValue
		}
	}
	return out
}

func TestPayerFormMapping(t *testing.T) {
	name, form, err := PayerForm(validPayer())
	if err != nil {
		t.Fatal(err)
	}
	if name != "Jane Doe" {
		t.Fatalf("name = %q", name)
	}
	m := fields(t, form)
	want := map[string]string{
		"base.name":                             "Jane Doe",
		"base.nameLocal":                        "Jane Doe", // defaults to the latin name
		"base.dateOfBirth":                      "632361600000",
		"base.nationality":                      "AE",
		"document.issuingCountry":               "AE", // defaults to nationality
		"document.citizenship":                  "AE",
		"document.primary":                      "yes",
		"document.issuedDate":                   "1577836800000",
		"identification.materiaType":            "drivers_license",
		"identification.materiaImage":           "f1",
		"residentialAddress.proofImage":         "f2",
		"residentialAddress.fileName":           "address-proof",
		"occupationFundSource.employmentStatus": "employed",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if _, ok := m["residentialAddress.line2"]; ok {
		t.Error("empty optional field should be omitted")
	}
}

func TestPayerFormIndefiniteDropsExpiry(t *testing.T) {
	in := validPayer()
	in.DocExpiryType, in.DocExpiryDate = "indefinite", ""
	_, form, err := PayerForm(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields(t, form)["document.expiryDate"]; ok {
		t.Fatal("expiryDate must be omitted for indefinite documents")
	}
}

func TestPayerFormValidation(t *testing.T) {
	cases := map[string]func(*PayerInput){
		"date_of_birth":         func(p *PayerInput) { p.DateOfBirth = "15/01/1990" },
		"calling_code":          func(p *PayerInput) { p.CallingCode = "971" },
		"doc_expiry_date":       func(p *PayerInput) { p.DocExpiryDate = "" },
		"address_proof_file_id": func(p *PayerInput) { p.AddressProofFileID = "" },
		"gender":                func(p *PayerInput) { p.Gender = "male" },
		"given_name":            func(p *PayerInput) { p.GivenName = "张三" },
	}
	for field, mut := range cases {
		in := validPayer()
		mut(&in)
		_, _, err := PayerForm(in)
		var pe *PayoutError
		if !errors.As(err, &pe) || pe.Extra["field"] != field {
			t.Errorf("%s: err = %v", field, err)
		}
	}
}

func TestBankForm(t *testing.T) {
	m := fields(t, BankForm("Jane Doe", holderAddress{"US", "CA", "SF", "535 Mission St", "", "94105"},
		BankInput{BankCountry: "US", BankName: "BoA", SwiftCode: "BOFAUS3N", AccountNumber: "123", IBAN: "GB82WEST12345698765432"}))
	for k, v := range map[string]string{
		"bankClearingCode.type": "swift_code", "bankClearingCode.value": "BOFAUS3N",
		"default.currency": "USD", "default.transferType": "swift", "default.holder": "Jane Doe",
		"default.holderAddressCountry": "US", "default.iban": "GB82WEST12345698765432",
	} {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
}

func TestNormalizeAmount(t *testing.T) {
	for in, want := range map[string]string{"5": "5.00", "20.5": "20.50", "100000": "100000.00"} {
		if got, err := NormalizeAmount(in); err != nil || got != want {
			t.Errorf("%s → %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"4.99", "100000.01", "1.234", "-5", "abc", ""} {
		if _, err := NormalizeAmount(in); err == nil {
			t.Errorf("%s accepted", in)
		}
	}
}

func TestDecSub(t *testing.T) {
	// measured 20 USD quote: debitAmount 68.06, fixed 10, transaction 3, exchange 0
	if got := decSub("68.06", "10", "3", "0"); got != "55.06" {
		t.Fatalf("decSub = %s", got)
	}
}

func TestStage(t *testing.T) {
	for status, want := range map[int]string{0: "quoting", 2: "awaiting_confirm", 12: "awaiting_confirm",
		3: "paid", 5: "paid", 6: "processing", 7: "completed", 9: "canceled", 11: "refunded"} {
		if got := Stage(status); got != want {
			t.Errorf("Stage(%d) = %s, want %s", status, got, want)
		}
	}
}
