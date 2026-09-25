package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"chldemo/internal/repo"
	"chldemo/internal/upayopen"

	"golang.org/x/crypto/bcrypt"
)

// PayoutError is a client-facing error: HTTP status + stable code + details.
type PayoutError struct {
	HTTP  int
	Code  string
	Msg   string
	Extra map[string]any
}

func (e *PayoutError) Error() string { return e.Code + ": " + e.Msg }

func perr(status int, code, msg string) *PayoutError {
	return &PayoutError{HTTP: status, Code: code, Msg: msg}
}

func invalid(field, msg string) *PayoutError {
	return &PayoutError{HTTP: http.StatusUnprocessableEntity, Code: "VALIDATION", Msg: field + ": " + msg,
		Extra: map[string]any{"field": field}}
}

// upstream wraps a UPay failure so the caller sees UPay's code and message.
func upstream(err error) error {
	var api *upayopen.APIError
	if errors.As(err, &api) {
		return &PayoutError{HTTP: http.StatusBadGateway, Code: "UPSTREAM", Msg: api.Msg,
			Extra: map[string]any{"upay_code": api.Code, "request_id": api.RequestID}}
	}
	return &PayoutError{HTTP: http.StatusBadGateway, Code: "UPSTREAM", Msg: err.Error()}
}

const (
	MinPayoutAmount     = "5"      // measured: 8009 "allowed range: 5 ~ 100000"
	MaxPayoutAmount     = "100000" //
	payoutCurrency      = "USD"    // bank account currency options are missing in OpenAPI; see issue list #4
	remitMethod         = "swift"
	tradePasswordFails  = 5
	tradePasswordLock   = 30 * time.Minute
	quoteSafetyMargin   = 5 * time.Second
	countriesCacheTTL   = 24 * time.Hour
	maxTextLen          = 140
	requoteExpiredAfter = 0 // requote only once the quote has passed validUntil
)

type PayoutConfig struct {
	DebitSymbol    string // e.g. USDT; resolved to a numeric coinId via debit/coin/list
	UploadFileType int    // 10 = payout material
}

// PayoutService owns every payout business rule; handlers, the sync job and
// the webhook all go through it.
type PayoutService struct {
	c    *upayopen.Client
	repo *repo.Repo
	cfg  PayoutConfig

	mu          sync.Mutex
	debitCoinID string
	countries   []upayopen.Area
	countriesAt time.Time
}

func NewPayout(c *upayopen.Client, r *repo.Repo, cfg PayoutConfig) *PayoutService {
	if cfg.DebitSymbol == "" {
		cfg.DebitSymbol = "USDT"
	}
	if cfg.UploadFileType == 0 {
		cfg.UploadFileType = 10
	}
	return &PayoutService{c: c, repo: r, cfg: cfg}
}

func (s *PayoutService) DebitSymbol() string { return s.cfg.DebitSymbol }

// DebitCoinID resolves the configured symbol to UPay's coinId (e.g. USDT → "458884").
func (s *PayoutService) DebitCoinID(ctx context.Context) (string, error) {
	s.mu.Lock()
	id := s.debitCoinID
	s.mu.Unlock()
	if id != "" {
		return id, nil
	}
	coins, err := s.c.DebitCoins(ctx)
	if err != nil {
		return "", upstream(err)
	}
	for _, c := range coins {
		if strings.EqualFold(c.Symbol, s.cfg.DebitSymbol) {
			s.mu.Lock()
			s.debitCoinID = c.CoinID
			s.mu.Unlock()
			return c.CoinID, nil
		}
	}
	return "", perr(http.StatusServiceUnavailable, "DEBIT_COIN_UNAVAILABLE", s.cfg.DebitSymbol+" is not a supported debit coin")
}

// Countries returns UPay's country list (cached).
func (s *PayoutService) Countries(ctx context.Context) ([]upayopen.Area, error) {
	s.mu.Lock()
	if s.countries != nil && time.Since(s.countriesAt) < countriesCacheTTL {
		out := s.countries
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	list, err := s.c.Countries(ctx)
	if err != nil {
		return nil, upstream(err)
	}
	s.mu.Lock()
	s.countries, s.countriesAt = list, time.Now()
	s.mu.Unlock()
	return list, nil
}

// ---- validation helpers ----

var (
	reCountry     = regexp.MustCompile(`^[A-Z]{2}$`)
	reCallingCode = regexp.MustCompile(`^\+[0-9]{1,4}$`)
	rePhone       = regexp.MustCompile(`^[0-9]([0-9-]{0,18}[0-9])?$`)
	rePostal      = regexp.MustCompile(`^[A-Za-z0-9 -]{1,20}$`)
	reSwift       = regexp.MustCompile(`^[A-Za-z0-9]{8,11}$`)
	reAccount     = regexp.MustCompile(`^[A-Za-z0-9]{1,34}$`)
	reIBAN        = regexp.MustCompile(`^[A-Za-z]{2}[0-9]{2}[A-Za-z0-9]{11,30}$`)
	reEmail       = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	reAmount      = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)
	reTradePwd    = regexp.MustCompile(`^[0-9]{6}$`)
	reLatinName   = regexp.MustCompile(`^[A-Za-z][A-Za-z ,.'-]*[A-Za-z.]$|^[A-Za-z]$`)
)

type validator struct{ err *PayoutError }

func (v *validator) check(ok bool, field, msg string) {
	if v.err == nil && !ok {
		v.err = invalid(field, msg)
	}
}

func (v *validator) required(field, val string, max int) {
	v.check(strings.TrimSpace(val) != "", field, "required")
	v.check(len([]rune(val)) <= max, field, fmt.Sprintf("at most %d characters", max))
}

func (v *validator) optional(field, val string, max int) {
	v.check(len([]rune(val)) <= max, field, fmt.Sprintf("at most %d characters", max))
}

func (v *validator) match(field, val string, re *regexp.Regexp, msg string) {
	v.check(re.MatchString(val), field, msg)
}

func (v *validator) oneOf(field, val string, allowed ...string) {
	for _, a := range allowed {
		if val == a {
			return
		}
	}
	v.check(false, field, "must be one of "+strings.Join(allowed, ", "))
}

// dateMillis converts yyyy-MM-dd to the UTC-midnight millisecond string UPay
// requires (verified: yyyy-MM-dd is rejected with 8002).
func dateMillis(v *validator, field, val string, required bool) string {
	if val == "" {
		v.check(!required, field, "required")
		return ""
	}
	t, err := time.Parse("2006-01-02", val)
	v.check(err == nil, field, "must be yyyy-MM-dd")
	if err != nil {
		return ""
	}
	return strconv.FormatInt(t.UTC().UnixMilli(), 10)
}

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ---- enums (values from the archived dynamic forms) ----

var (
	Genders          = []string{"Male", "Female"}
	DocTypes         = []string{"passport", "national_id", "driving_license", "residence_permit"}
	ExpiryTypes      = []string{"dated", "indefinite"}
	EmploymentStatus = []string{"employed", "self_employed", "unemployed", "student", "retired", "homemaker", "other"}
	SourcesOfIncome  = []string{"Salary", "Pension", "Investment", "Property", "FriendsAndFamily", "Benefits"}
)

// identification.materiaType has its own vocabulary.
var materiaTypeByDoc = map[string]string{
	"passport": "passport", "national_id": "national_id", "driving_license": "drivers_license", "residence_permit": "other",
}

// ---- files ----

var uploadTypes = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".pdf": "application/pdf"}

const MaxUploadBytes = 10 << 20 // fileType=10 allows 10 MB

func (s *PayoutService) UploadFile(ctx context.Context, r io.Reader, filename string) (string, error) {
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	ct, ok := uploadTypes["."+ext]
	if !ok || !strings.Contains(filename, ".") {
		return "", invalid("file", "only jpg, jpeg, png or pdf")
	}
	id, err := s.c.Upload(ctx, r, filename, ct, s.cfg.UploadFileType)
	if err != nil {
		return "", upstream(err)
	}
	return id, nil
}

// ---- payer (KYC) ----

type PayerInput struct {
	GivenName   string `json:"given_name"`
	FamilyName  string `json:"family_name"`
	NameLocal   string `json:"name_local"`
	DateOfBirth string `json:"date_of_birth"` // yyyy-MM-dd
	Nationality string `json:"nationality"`
	Gender      string `json:"gender"`

	Email       string `json:"email"`
	CallingCode string `json:"calling_code"`
	Phone       string `json:"phone"`

	DocType           string `json:"doc_type"`
	DocNumber         string `json:"doc_number"`
	DocIssuingCountry string `json:"doc_issuing_country"`
	DocIssuedDate     string `json:"doc_issued_date"`
	DocExpiryType     string `json:"doc_expiry_type"`
	DocExpiryDate     string `json:"doc_expiry_date"`

	IdentityFileID   string `json:"identity_file_id"`
	IdentityFileName string `json:"identity_file_name"`

	AddrCountry          string `json:"addr_country"`
	AddrProvince         string `json:"addr_province"`
	AddrCity             string `json:"addr_city"`
	AddrLine1            string `json:"addr_line1"`
	AddrLine2            string `json:"addr_line2"`
	AddrPostalCode       string `json:"addr_postal_code"`
	AddressProofFileID   string `json:"address_proof_file_id"`
	AddressProofFileName string `json:"address_proof_file_name"`

	EmploymentStatus string `json:"employment_status"`
	SourceOfIncome   string `json:"source_of_income"`
	Occupation       string `json:"occupation"`
}

// PayerForm validates the input and builds the UPay formData (field mapping:
// TECH-DESIGN §7.2).
func PayerForm(in PayerInput) (name string, form string, err error) {
	v := &validator{}
	in.Nationality, in.DocIssuingCountry, in.AddrCountry = upper(in.Nationality), upper(in.DocIssuingCountry), upper(in.AddrCountry)
	v.required("given_name", in.GivenName, 70)
	v.match("given_name", in.GivenName, reLatinName, "latin letters only")
	v.required("family_name", in.FamilyName, 70)
	v.match("family_name", in.FamilyName, reLatinName, "latin letters only")
	name = strings.TrimSpace(in.GivenName) + " " + strings.TrimSpace(in.FamilyName)
	if strings.TrimSpace(in.NameLocal) == "" {
		in.NameLocal = name
	}
	v.optional("name_local", in.NameLocal, 140)
	dob := dateMillis(v, "date_of_birth", in.DateOfBirth, true)
	v.match("nationality", in.Nationality, reCountry, "ISO 3166-1 alpha-2")
	v.oneOf("gender", in.Gender, Genders...)

	v.required("email", in.Email, 64)
	v.match("email", in.Email, reEmail, "invalid email")
	v.match("calling_code", in.CallingCode, reCallingCode, "like +971")
	v.match("phone", in.Phone, rePhone, "digits and hyphens only")

	v.oneOf("doc_type", in.DocType, DocTypes...)
	v.required("doc_number", in.DocNumber, 100)
	if in.DocIssuingCountry == "" {
		in.DocIssuingCountry = in.Nationality
	}
	v.match("doc_issuing_country", in.DocIssuingCountry, reCountry, "ISO 3166-1 alpha-2")
	issued := dateMillis(v, "doc_issued_date", in.DocIssuedDate, true)
	v.oneOf("doc_expiry_type", in.DocExpiryType, ExpiryTypes...)
	expiry := dateMillis(v, "doc_expiry_date", in.DocExpiryDate, in.DocExpiryType == "dated")
	if in.DocExpiryType == "indefinite" {
		expiry = ""
	}

	v.required("identity_file_id", in.IdentityFileID, 100)
	v.match("addr_country", in.AddrCountry, reCountry, "ISO 3166-1 alpha-2")
	v.required("addr_province", in.AddrProvince, 100)
	v.required("addr_city", in.AddrCity, 100)
	v.required("addr_line1", in.AddrLine1, 100)
	v.optional("addr_line2", in.AddrLine2, 100)
	v.match("addr_postal_code", in.AddrPostalCode, rePostal, "letters, digits, spaces and hyphens, max 20")
	v.required("address_proof_file_id", in.AddressProofFileID, 100)

	if in.EmploymentStatus != "" {
		v.oneOf("employment_status", in.EmploymentStatus, EmploymentStatus...)
	}
	if in.SourceOfIncome != "" {
		v.oneOf("source_of_income", in.SourceOfIncome, SourcesOfIncome...)
	}
	v.optional("occupation", in.Occupation, 100)
	if v.err != nil {
		return "", "", v.err
	}

	fileName := func(n, def string) string {
		if n = strings.TrimSpace(n); n == "" {
			return def
		}
		if len([]rune(n)) > 100 {
			return string([]rune(n)[:100])
		}
		return n
	}
	f := upayopen.NewForm("person").
		Group("base",
			upayopen.F("name", name), upayopen.F("nameLocal", in.NameLocal), upayopen.F("dateOfBirth", dob),
			upayopen.F("nationality", in.Nationality), upayopen.F("gender", in.Gender)).
		Group("contact",
			upayopen.F("email", in.Email), upayopen.F("callingCode", in.CallingCode), upayopen.F("phoneNumber", in.Phone)).
		MultiGroup("document",
			upayopen.F("type", in.DocType), upayopen.F("number", in.DocNumber),
			upayopen.F("issuingCountry", in.DocIssuingCountry), upayopen.F("issuedDate", issued),
			upayopen.F("citizenship", in.Nationality), upayopen.F("primary", "yes"),
			upayopen.F("expiryType", in.DocExpiryType), upayopen.F("expiryDate", expiry)).
		Group("residentialAddress",
			upayopen.F("country", in.AddrCountry), upayopen.F("province", in.AddrProvince),
			upayopen.F("city", in.AddrCity), upayopen.F("line1", in.AddrLine1), upayopen.F("line2", in.AddrLine2),
			upayopen.F("postalCode", in.AddrPostalCode),
			upayopen.F("fileName", fileName(in.AddressProofFileName, "address-proof")),
			upayopen.F("describe", "proof of address"), upayopen.F("proofImage", in.AddressProofFileID)).
		Group("identification",
			upayopen.F("materiaType", materiaTypeByDoc[in.DocType]),
			upayopen.F("fileName", fileName(in.IdentityFileName, "identity")),
			upayopen.F("materiaImage", in.IdentityFileID),
			upayopen.F("materiaDescribe", in.DocType+" photo")).
		Group("occupationFundSource",
			upayopen.F("employmentStatus", in.EmploymentStatus), upayopen.F("sourceOfIncome", in.SourceOfIncome),
			upayopen.F("occupation", in.Occupation))
	return name, f.JSON(), nil
}

func (s *PayoutService) CreatePayer(ctx context.Context, userID int64, in PayerInput) (repo.PayoutPayer, error) {
	if p, err := s.repo.GetPayoutPayer(ctx, userID); err == nil {
		return p, perr(http.StatusConflict, "PAYER_EXISTS", "payer already created")
	}
	name, form, err := PayerForm(in)
	if err != nil {
		return repo.PayoutPayer{}, err
	}
	payerNo, err := s.c.AddPayer(ctx, "person", form)
	if err != nil {
		var api *upayopen.APIError
		if errors.As(err, &api) && (api.Code == 10019 || api.Code == 10020) {
			return repo.PayoutPayer{}, perr(http.StatusUnprocessableEntity, "FILE_EXPIRED", "uploaded file expired, please upload again")
		}
		return repo.PayoutPayer{}, upstream(err)
	}
	if err := s.repo.CreatePayoutPayer(ctx, userID, payerNo, name); err != nil {
		// UPay already has the payer; keep its number in the log for manual repair.
		log.Printf("payout: user=%d payer %s created at UPay but not saved: %v", userID, payerNo, err)
		return repo.PayoutPayer{}, err
	}
	return s.repo.GetPayoutPayer(ctx, userID)
}

// ---- recipients (beneficiary + bank account) ----

type BankInput struct {
	BankCountry   string `json:"bank_country"`
	BankName      string `json:"bank_name"`
	SwiftCode     string `json:"swift_code"`
	AccountNumber string `json:"account_number"`
	IBAN          string `json:"iban"`
}

type RecipientInput struct {
	GivenName   string `json:"given_name"`
	FamilyName  string `json:"family_name"`
	DateOfBirth string `json:"date_of_birth"`
	Nationality string `json:"nationality"`
	Email       string `json:"email"`
	CallingCode string `json:"calling_code"`
	Phone       string `json:"phone"`

	AddrCountry    string `json:"addr_country"`
	AddrState      string `json:"addr_state"`
	AddrCity       string `json:"addr_city"`
	AddrLine1      string `json:"addr_line1"`
	AddrLine2      string `json:"addr_line2"`
	AddrPostalCode string `json:"addr_postal_code"`

	Bank BankInput `json:"bank"`
}

type holderAddress struct{ Country, State, City, Line1, Line2, PostalCode string }

func validateBank(v *validator, b *BankInput) {
	b.BankCountry, b.SwiftCode, b.IBAN = upper(b.BankCountry), upper(b.SwiftCode), upper(strings.ReplaceAll(b.IBAN, " ", ""))
	b.AccountNumber = strings.ReplaceAll(b.AccountNumber, " ", "")
	v.match("bank.bank_country", b.BankCountry, reCountry, "ISO 3166-1 alpha-2")
	v.required("bank.bank_name", b.BankName, 140)
	v.match("bank.swift_code", b.SwiftCode, reSwift, "8 or 11 letters/digits")
	v.match("bank.account_number", b.AccountNumber, reAccount, "letters and digits, max 34")
	// UPay rejects bank accounts without IBAN (8002), even for non-IBAN countries.
	v.match("bank.iban", b.IBAN, reIBAN, "15-34 chars, 2 letters + 2 digits + alphanumerics")
}

// BankForm builds bank/account/add formData (TECH-DESIGN §7.3).
func BankForm(holder string, addr holderAddress, b BankInput) string {
	return upayopen.NewForm("bankAccount").
		Group("bankClearingCode", upayopen.F("type", "swift_code"), upayopen.F("value", b.SwiftCode)).
		Group("default",
			upayopen.F("country", b.BankCountry), upayopen.F("currency", payoutCurrency),
			upayopen.F("holder", holder), upayopen.F("bank", b.BankName),
			upayopen.F("transferType", remitMethod), upayopen.F("accountNumber", b.AccountNumber),
			upayopen.F("iban", b.IBAN),
			upayopen.F("holderAddressCountry", addr.Country), upayopen.F("holderAddressState", addr.State),
			upayopen.F("holderAddressCity", addr.City), upayopen.F("holderAddressLine1", addr.Line1),
			upayopen.F("holderAddressLine2", addr.Line2), upayopen.F("holderAddressPostalCode", addr.PostalCode)).
		JSON()
}

func last4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}

func (s *PayoutService) CreateRecipient(ctx context.Context, userID int64, in RecipientInput) (repo.PayoutRecipient, error) {
	v := &validator{}
	in.Nationality, in.AddrCountry = upper(in.Nationality), upper(in.AddrCountry)
	v.required("given_name", in.GivenName, 70)
	v.match("given_name", in.GivenName, reLatinName, "latin letters only")
	v.required("family_name", in.FamilyName, 70)
	v.match("family_name", in.FamilyName, reLatinName, "latin letters only")
	dob := dateMillis(v, "date_of_birth", in.DateOfBirth, true)
	v.match("nationality", in.Nationality, reCountry, "ISO 3166-1 alpha-2")
	v.required("email", in.Email, 64)
	v.match("email", in.Email, reEmail, "invalid email")
	v.match("calling_code", in.CallingCode, reCallingCode, "like +1")
	v.match("phone", in.Phone, rePhone, "digits and hyphens only")
	// Bank holder address fields are mandatory, so the shared address is too.
	v.match("addr_country", in.AddrCountry, reCountry, "ISO 3166-1 alpha-2")
	v.required("addr_state", in.AddrState, 100)
	v.required("addr_city", in.AddrCity, 100)
	v.required("addr_line1", in.AddrLine1, 200)
	v.optional("addr_line2", in.AddrLine2, 200)
	v.match("addr_postal_code", in.AddrPostalCode, rePostal, "letters, digits, spaces and hyphens, max 20")
	validateBank(v, &in.Bank)
	if v.err != nil {
		return repo.PayoutRecipient{}, v.err
	}

	name := strings.TrimSpace(in.GivenName) + " " + strings.TrimSpace(in.FamilyName)
	rec := repo.PayoutRecipient{
		UserID: userID, GivenName: strings.TrimSpace(in.GivenName), FamilyName: strings.TrimSpace(in.FamilyName),
		BankName: in.Bank.BankName, SwiftCode: in.Bank.SwiftCode, AccountLast4: last4(in.Bank.AccountNumber),
		Currency: payoutCurrency, BankCountry: in.Bank.BankCountry,
	}
	id, err := s.repo.CreatePayoutRecipient(ctx, rec)
	if err != nil {
		return rec, err
	}
	rec.ID = id

	benForm := upayopen.NewForm("person").
		Group("base", upayopen.F("name", name), upayopen.F("dateOfBirth", dob), upayopen.F("nationality", in.Nationality)).
		Group("contact", upayopen.F("email", in.Email), upayopen.F("callingCode", in.CallingCode), upayopen.F("number", in.Phone)).
		Group("residentialAddress",
			upayopen.F("country", in.AddrCountry), upayopen.F("state", in.AddrState), upayopen.F("city", in.AddrCity),
			upayopen.F("line1", in.AddrLine1), upayopen.F("line2", in.AddrLine2), upayopen.F("postalCode", in.AddrPostalCode)).
		JSON()
	benNo, err := s.c.AddBeneficiary(ctx, "person", benForm)
	if err != nil {
		_ = s.repo.SetRecipientError(ctx, id, err.Error())
		return rec, upstream(err)
	}
	if err := s.repo.SetRecipientBeneficiary(ctx, id, benNo); err != nil {
		return rec, err
	}
	rec.BeneficiaryNo = &benNo

	addr := holderAddress{in.AddrCountry, in.AddrState, in.AddrCity, in.AddrLine1, in.AddrLine2, in.AddrPostalCode}
	return s.addBank(ctx, rec, name, addr, in.Bank)
}

func (s *PayoutService) addBank(ctx context.Context, rec repo.PayoutRecipient, holder string, addr holderAddress, b BankInput) (repo.PayoutRecipient, error) {
	bankNo, err := s.c.AddBankAccount(ctx, *rec.BeneficiaryNo, BankForm(holder, addr, b))
	if err != nil {
		_ = s.repo.SetRecipientError(ctx, rec.ID, err.Error())
		e := upstream(err).(*PayoutError)
		if e.Extra == nil {
			e.Extra = map[string]any{}
		}
		e.Code, e.Extra["recipient_id"] = "RECIPIENT_BANK_FAILED", rec.ID
		return rec, e
	}
	rec.BankAccountNo = &bankNo
	rec.BankName, rec.SwiftCode, rec.AccountLast4, rec.BankCountry = b.BankName, b.SwiftCode, last4(b.AccountNumber), b.BankCountry
	if err := s.repo.SetRecipientBank(ctx, rec); err != nil {
		return rec, err
	}
	return rec, nil
}

// RetryRecipientBank re-runs step 2. The full account number / IBAN are not
// stored locally, so the bank section is resubmitted; the holder address is
// read back from the beneficiary at UPay.
func (s *PayoutService) RetryRecipientBank(ctx context.Context, userID, id int64, b BankInput) (repo.PayoutRecipient, error) {
	rec, err := s.repo.GetPayoutRecipient(ctx, userID, id)
	if err != nil {
		return rec, err
	}
	if rec.BeneficiaryNo == nil {
		return rec, perr(http.StatusConflict, "RECIPIENT_INCOMPLETE", "recipient was not created, please add it again")
	}
	if rec.BankAccountNo != nil {
		return rec, nil
	}
	v := &validator{}
	validateBank(v, &b)
	if v.err != nil {
		return rec, v.err
	}
	info, err := s.c.BeneficiaryInfo(ctx, *rec.BeneficiaryNo)
	if err != nil {
		return rec, upstream(err)
	}
	m := info.Info.Flatten()
	addr := holderAddress{m["residentialAddress.country"], m["residentialAddress.state"], m["residentialAddress.city"],
		m["residentialAddress.line1"], m["residentialAddress.line2"], m["residentialAddress.postalCode"]}
	return s.addBank(ctx, rec, rec.Name(), addr, b)
}

// ---- orders ----

// Stage maps a UPay order status to the UI stage (TECH-DESIGN §6.2).
func Stage(status int) string {
	switch status {
	case upayopen.StatusCreated, upayopen.StatusQuoting:
		return "quoting"
	case upayopen.StatusAwaitingConfirm, upayopen.StatusAwaitingNewQuote:
		return "awaiting_confirm"
	case upayopen.StatusQuoteFailed:
		return "quote_failed"
	case upayopen.StatusQuoteConfirmed, upayopen.StatusReviewing:
		return "paid"
	case upayopen.StatusProcessing:
		return "processing"
	case upayopen.StatusSucceeded:
		return "completed"
	case upayopen.StatusFailed:
		return "failed"
	case upayopen.StatusCanceled:
		return "canceled"
	case upayopen.StatusRefunding:
		return "refunding"
	case upayopen.StatusRefunded:
		return "refunded"
	}
	return "unknown"
}

func newThirdOrderNo() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "PO" + strings.ToUpper(strconv.FormatInt(time.Now().UnixMilli(), 36)) + strings.ToUpper(hex.EncodeToString(b))
}

// NormalizeAmount validates a fiat amount and renders it with 2 decimals.
func NormalizeAmount(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !reAmount.MatchString(s) {
		return "", invalid("amount", "up to 2 decimals")
	}
	r, _ := new(big.Rat).SetString(s)
	lo, _ := new(big.Rat).SetString(MinPayoutAmount)
	hi, _ := new(big.Rat).SetString(MaxPayoutAmount)
	if r.Cmp(lo) < 0 || r.Cmp(hi) > 0 {
		return "", &PayoutError{HTTP: http.StatusUnprocessableEntity, Code: "AMOUNT_OUT_OF_RANGE",
			Msg:   "amount must be between " + MinPayoutAmount + " and " + MaxPayoutAmount,
			Extra: map[string]any{"min": MinPayoutAmount, "max": MaxPayoutAmount}}
	}
	return r.FloatString(2), nil
}

type CreatedOrder struct {
	Order   repo.PayoutOrder
	Improve any // informationToImprove, when UPay reports missing data
}

func (s *PayoutService) CreateOrder(ctx context.Context, userID, recipientID int64, amount, usage, note string) (CreatedOrder, error) {
	var out CreatedOrder
	payer, err := s.repo.GetPayoutPayer(ctx, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return out, perr(http.StatusConflict, "PAYER_REQUIRED", "complete verification first")
	} else if err != nil {
		return out, err
	}
	rec, err := s.repo.GetPayoutRecipient(ctx, userID, recipientID)
	if errors.Is(err, repo.ErrNotFound) {
		return out, perr(http.StatusNotFound, "NOT_FOUND", "recipient not found")
	} else if err != nil {
		return out, err
	}
	if !rec.Complete() {
		return out, perr(http.StatusConflict, "RECIPIENT_INCOMPLETE", "recipient bank account is not set up")
	}
	amt, err := NormalizeAmount(amount)
	if err != nil {
		return out, err
	}
	v := &validator{}
	usage, note = strings.TrimSpace(usage), strings.TrimSpace(note)
	v.required("usage", usage, maxTextLen)
	v.optional("note", note, maxTextLen)
	if v.err != nil {
		return out, v.err
	}
	if note == "" {
		note = usage // remitNote is mandatory at UPay
	}
	coinID, err := s.DebitCoinID(ctx)
	if err != nil {
		return out, err
	}
	o := repo.PayoutOrder{UserID: userID, RecipientID: rec.ID, ThirdOrderNo: newThirdOrderNo(), Amount: amt,
		Currency: rec.Currency, DebitCoin: s.cfg.DebitSymbol, Usage: usage, Note: note}
	if o.ID, err = s.repo.CreatePayoutOrder(ctx, o); err != nil {
		return out, err
	}
	res, err := s.c.CreateOrder(ctx, upayopen.CreateOrderReq{
		DebitCoinID: coinID, ThirdOrderNo: o.ThirdOrderNo, PayerNo: payer.PayerNo, BankAccountNo: *rec.BankAccountNo,
		Amount: amt, RemitMethod: remitMethod, RemitUsage: usage, RemitNote: note,
	})
	if err != nil {
		_ = s.repo.SetPayoutOrderError(ctx, o.ID, err.Error())
		_, _ = s.repo.ApplyPayoutOrderStatus(ctx, o.ID, upayopen.StatusQuoteFailed, time.Now(), "api")
		return out, upstream(err)
	}
	if err := s.repo.SetPayoutOrderCreated(ctx, o.ID, res.OrderNo); err != nil {
		return out, err
	}
	_, _ = s.repo.ApplyPayoutOrderStatus(ctx, o.ID, res.OrderStatus, time.Now(), "api")
	o.OrderNo, o.Status = &res.OrderNo, res.OrderStatus
	out.Order = o
	if len(res.InformationToImprove) > 0 {
		out.Improve = res.InformationToImprove
	}
	return out, nil
}

// QuoteView is the confirm-page payload; also stored as the confirmation snapshot.
type QuoteView struct {
	QuoteNo             string `json:"quote_no"`
	DebitCoin           string `json:"debit_coin"`
	DebitTotal          string `json:"debit_total"` // debitAmount: total incl. fees
	PayAmount           string `json:"pay_amount"`  // debitTotal − fees
	FixedFee            string `json:"fixed_fee"`
	TransactionFee      string `json:"transaction_fee"`
	ExchangeFee         string `json:"exchange_fee"`
	FeeCurrency         string `json:"fee_currency"`
	DestinationAmount   string `json:"destination_amount"`
	DestinationCurrency string `json:"destination_currency"`
	ValidUntil          int64  `json:"valid_until"` // ms
	KycURL              string `json:"kyc_url,omitempty"`
}

type QuoteState struct {
	Status    int        `json:"status"`
	Stage     string     `json:"stage"`
	Quote     *QuoteView `json:"quote"`
	ExpiresIn int64      `json:"expires_in"` // seconds, 0 when expired / no quote
}

// decSub returns a − Σbs as a decimal string with the widest input precision.
func decSub(a string, bs ...string) string {
	prec := 0
	sum, ok := new(big.Rat).SetString(orZero(a))
	if !ok {
		return ""
	}
	for _, s := range append([]string{a}, bs...) {
		if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > prec {
			prec = len(s) - i - 1
		}
	}
	for _, b := range bs {
		r, ok := new(big.Rat).SetString(orZero(b))
		if !ok {
			return ""
		}
		sum.Sub(sum, r)
	}
	return sum.FloatString(prec)
}

func orZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

func toQuoteView(q upayopen.Quote) *QuoteView {
	return &QuoteView{
		QuoteNo: q.QuoteNo, DebitCoin: q.DebitCoin, DebitTotal: q.DebitAmount.String(),
		PayAmount: decSub(q.DebitAmount.String(), q.FixedFee.String(), q.TransactionFee.String(), q.ExchangeFee.String()),
		FixedFee:  orZero(q.FixedFee.String()), TransactionFee: orZero(q.TransactionFee.String()),
		ExchangeFee: orZero(q.ExchangeFee.String()), FeeCurrency: q.FeeCurrency,
		DestinationAmount: q.DestinationAmount.String(), DestinationCurrency: q.DestinationCurrency,
		ValidUntil: q.ValidUntil.Millis(), KycURL: q.KycURL,
	}
}

func (s *PayoutService) order(ctx context.Context, userID, id int64) (repo.PayoutOrder, error) {
	o, err := s.repo.GetPayoutOrder(ctx, userID, id)
	if errors.Is(err, repo.ErrNotFound) {
		return o, perr(http.StatusNotFound, "NOT_FOUND", "order not found")
	}
	if err == nil && o.OrderNo == nil {
		return o, perr(http.StatusConflict, "ORDER_NOT_CREATED", "order was not accepted by UPay")
	}
	return o, err
}

// currentQuote fetches the latest quote state and records any status change.
func (s *PayoutService) currentQuote(ctx context.Context, o repo.PayoutOrder) (QuoteState, *upayopen.Quote, error) {
	qi, err := s.c.QuoteInfo(ctx, o.ThirdOrderNo)
	if err != nil {
		return QuoteState{}, nil, upstream(err)
	}
	_, _ = s.repo.ApplyPayoutOrderStatus(ctx, o.ID, qi.Status, time.Now(), "api")
	st := QuoteState{Status: qi.Status, Stage: Stage(qi.Status)}
	if qi.Status != upayopen.StatusAwaitingConfirm && qi.Status != upayopen.StatusAwaitingNewQuote {
		return st, nil, nil
	}
	for i := range qi.QuoteList {
		q := qi.QuoteList[i]
		if q.Status != upayopen.QuoteAvailable {
			continue
		}
		st.Quote = toQuoteView(q)
		// UPay never marks an unconfirmed quote as expired; validUntil is the only signal.
		if left := time.Until(time.UnixMilli(st.Quote.ValidUntil)); left > 0 {
			st.ExpiresIn = int64(left.Seconds())
		}
		return st, &q, nil
	}
	return st, nil, nil
}

func (s *PayoutService) Quote(ctx context.Context, userID, id int64) (QuoteState, error) {
	o, err := s.order(ctx, userID, id)
	if err != nil {
		return QuoteState{}, err
	}
	if upayopen.IsTerminal(o.Status) || o.ConfirmedAt != nil {
		return QuoteState{Status: o.Status, Stage: Stage(o.Status)}, nil
	}
	st, _, err := s.currentQuote(ctx, o)
	return st, err
}

// ---- trade password ----

func (s *PayoutService) HasTradePassword(ctx context.Context, userID int64) (bool, error) {
	tp, err := s.repo.GetTradePassword(ctx, userID)
	return tp.Hash != nil, err
}

func (s *PayoutService) SetTradePassword(ctx context.Context, userID int64, pwd string) error {
	if !reTradePwd.MatchString(pwd) {
		return invalid("password", "6 digits")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	ok, err := s.repo.SetTradePasswordIfUnset(ctx, userID, string(hash))
	if err != nil {
		return err
	}
	if !ok {
		return perr(http.StatusConflict, "TRADE_PASSWORD_EXISTS", "trade password already set")
	}
	return nil
}

func (s *PayoutService) CheckTradePassword(ctx context.Context, userID int64, pwd string) error {
	tp, err := s.repo.GetTradePassword(ctx, userID)
	if err != nil {
		return err
	}
	if tp.Hash == nil {
		return perr(http.StatusPreconditionRequired, "TRADE_PASSWORD_NOT_SET", "set a trade password first")
	}
	if tp.LockedUntil != nil && tp.LockedUntil.After(time.Now()) {
		return &PayoutError{HTTP: http.StatusLocked, Code: "TRADE_PASSWORD_LOCKED", Msg: "too many attempts",
			Extra: map[string]any{"retry_after": int64(time.Until(*tp.LockedUntil).Seconds())}}
	}
	if bcrypt.CompareHashAndPassword([]byte(*tp.Hash), []byte(pwd)) == nil {
		return s.repo.ResetTradePasswordFailures(ctx, userID)
	}
	n, err := s.repo.RecordTradePasswordFailure(ctx, userID, tradePasswordFails, time.Now().Add(tradePasswordLock))
	if err != nil {
		return err
	}
	if n == 0 {
		return &PayoutError{HTTP: http.StatusLocked, Code: "TRADE_PASSWORD_LOCKED", Msg: "too many attempts",
			Extra: map[string]any{"retry_after": int64(tradePasswordLock.Seconds())}}
	}
	// 422, not 401: the frontend treats 401 as "session expired" and logs out.
	return &PayoutError{HTTP: http.StatusUnprocessableEntity, Code: "TRADE_PASSWORD_INVALID", Msg: "wrong trade password",
		Extra: map[string]any{"remaining_attempts": tradePasswordFails - n}}
}

// ---- confirm / cancel / requote ----

func (s *PayoutService) Confirm(ctx context.Context, userID, id int64, quoteNo, pwd string) (int, error) {
	o, err := s.order(ctx, userID, id)
	if err != nil {
		return 0, err
	}
	// Only orders we hold in 2/12 are confirmable — UPay itself does not check
	// (a canceled order can be revived; see issue list #5).
	if o.Status != upayopen.StatusAwaitingConfirm && o.Status != upayopen.StatusAwaitingNewQuote {
		return o.Status, perr(http.StatusConflict, "ORDER_NOT_CONFIRMABLE", "order is not awaiting confirmation")
	}
	st, q, err := s.currentQuote(ctx, o)
	if err != nil {
		return 0, err
	}
	if q == nil || q.QuoteNo != quoteNo {
		return st.Status, perr(http.StatusConflict, "QUOTE_CHANGED", "quote is no longer available, refresh")
	}
	if time.Until(time.UnixMilli(q.ValidUntil.Millis())) < quoteSafetyMargin {
		return st.Status, perr(http.StatusConflict, "QUOTE_EXPIRED", "quote expired")
	}
	if err := s.CheckTradePassword(ctx, userID, pwd); err != nil {
		return st.Status, err
	}
	res, err := s.c.ConfirmOrder(ctx, o.ThirdOrderNo, quoteNo)
	if err != nil {
		return st.Status, upstream(err)
	}
	snap, _ := json.Marshal(toQuoteView(*q))
	if err := s.repo.SetPayoutOrderConfirmed(ctx, o.ID, snap); err != nil {
		return 0, err
	}
	status := res.OrderStatus
	if status == 0 {
		status = upayopen.StatusQuoteConfirmed // confirm may return empty data
	}
	_, _ = s.repo.ApplyPayoutOrderStatus(ctx, o.ID, status, time.Now(), "api")
	// The synchronous 3 is provisional (UPay re-checks the quote asynchronously).
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		time.Sleep(2 * time.Second)
		if o2, err := s.repo.GetPayoutOrder(c, userID, id); err == nil {
			_ = s.SyncOrder(c, o2, time.Now(), "api")
		}
	}()
	return status, nil
}

func (s *PayoutService) Cancel(ctx context.Context, userID, id int64) (int, error) {
	o, err := s.order(ctx, userID, id)
	if err != nil {
		return 0, err
	}
	switch o.Status {
	case upayopen.StatusCreated, upayopen.StatusQuoting, upayopen.StatusAwaitingConfirm, upayopen.StatusAwaitingNewQuote:
	default:
		return o.Status, perr(http.StatusConflict, "ORDER_NOT_CANCELABLE", "order can no longer be canceled")
	}
	status, err := s.c.CancelOrder(ctx, o.ThirdOrderNo)
	if err != nil {
		return o.Status, upstream(err)
	}
	if status == 0 {
		status = upayopen.StatusCanceled
	}
	_, _ = s.repo.ApplyPayoutOrderStatus(ctx, o.ID, status, time.Now(), "api")
	return status, nil
}

// Requote replaces an order whose quote expired. UPay has no requote endpoint
// (issue list #2); triggering it via confirm risks a real debit when clocks
// disagree, so we cancel and create a fresh order with the same parameters.
func (s *PayoutService) Requote(ctx context.Context, userID, id int64) (CreatedOrder, error) {
	o, err := s.order(ctx, userID, id)
	if err != nil {
		return CreatedOrder{}, err
	}
	if o.Status != upayopen.StatusAwaitingConfirm && o.Status != upayopen.StatusAwaitingNewQuote &&
		o.Status != upayopen.StatusQuoteFailed {
		return CreatedOrder{}, perr(http.StatusConflict, "ORDER_NOT_REQUOTABLE", "order cannot be requoted")
	}
	if o.Status != upayopen.StatusQuoteFailed {
		if st, _, err := s.currentQuote(ctx, o); err != nil {
			return CreatedOrder{}, err
		} else if st.ExpiresIn > requoteExpiredAfter {
			return CreatedOrder{}, perr(http.StatusConflict, "QUOTE_STILL_VALID", "current quote is still valid")
		}
		if _, err := s.Cancel(ctx, userID, id); err != nil {
			return CreatedOrder{}, err
		}
	}
	return s.CreateOrder(ctx, userID, o.RecipientID, o.Amount, o.Usage, o.Note)
}

// ---- status sync (job, webhook, confirm) ----

func (s *PayoutService) SyncOrder(ctx context.Context, o repo.PayoutOrder, observedAt time.Time, source string) error {
	info, raw, err := s.c.OrderInfo(ctx, o.ThirdOrderNo)
	if err != nil {
		return err
	}
	if err := s.repo.SetPayoutOrderInfo(ctx, o.ID, raw); err != nil {
		return err
	}
	changed, err := s.repo.ApplyPayoutOrderStatus(ctx, o.ID, info.Status, observedAt, source)
	if changed {
		log.Printf("payout: order %s status %d → %d (%s)", o.ThirdOrderNo, o.Status, info.Status, source)
	}
	return err
}

// SyncByOrderNo syncs the order a webhook refers to. Unknown orders are ignored.
func (s *PayoutService) SyncByOrderNo(ctx context.Context, orderNo string, observedAt time.Time, source string) error {
	o, err := s.repo.GetPayoutOrderByOrderNo(ctx, orderNo)
	if errors.Is(err, repo.ErrNotFound) {
		log.Printf("payout: webhook for unknown order %s ignored", orderNo)
		return nil
	}
	if err != nil {
		return err
	}
	return s.SyncOrder(ctx, o, observedAt, source)
}
