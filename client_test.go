package fints

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestFirstAccounts covers the first call of a program: synchronization, the only active TAN medium,
// the approval in the app with two status requests and the accounts.
func TestFirstAccounts(t *testing.T) {
	b := newBank(t,
		with(bankParams("10"), returns("HIRMG", code("0010", "Nachricht entgegengenommen.")), returns("HIRMS", code("3920", "Zugelassene TAN-Verfahren", "946")),
			newSegment("HISYN", 4, element{text("SYSTEM1")})),
		[]segment{newSegment("HITAB", 5, element{text("0")}, medium("Altes Handy", "2"), medium("Simons iPhone", "1"))},
		[]segment{returns("HIRMG", code("0100", "Dialog beendet."))},
		with(bankParams("10"), returns("HIRMS", code("3955", "Sicherheitsfreigabe erfolgt über anderen Kanal.")),
			newSegment("HITAN", 7, element{text("4")}, element{}, element{text("AUFTRAG1")}, element{text("Bitte in SecureGo plus freigeben.")})),
		[]segment{returns("HIRMS", code("3956", "Starke Kundenauthentifizierung noch ausstehend."))},
		[]segment{returns("HIRMS", code("0020", "Der Auftrag wurde ausgeführt.")), newSegment("HITAN", 7, element{text("S")}, element{}, element{text("AUFTRAG1")})},
		[]segment{newSegment("HISPA", 1,
			element{text("J"), text("DE02120300000000202051"), text("GENODEM1GTL"), text("202051"), text(""), text("280"), text("12345678")},
			element{text("J"), text("DE89370400440532013000"), text("GENODEM1GTL"), text("532013000"), text(""), text("280"), text("12345678")})},
		[]segment{returns("HIRMG", code("0100", "Dialog beendet."))},
	)
	c, waits := testClient(b)
	var approvals []string
	c.Approve = func(text string) {
		approvals = append(approvals, text)
	}
	accounts, err := c.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0] != (Account{IBAN: "DE02120300000000202051", BIC: "GENODEM1GTL", Number: "202051", SubAccount: "", BankCode: "12345678"}) {
		t.Errorf("got the accounts %+v", accounts)
	}
	if c.State != (State{SystemID: "SYSTEM1", TANMethod: "946", TANMedium: "Simons iPhone"}) {
		t.Errorf("got the state %+v", c.State)
	}
	if !reflect.DeepEqual(approvals, []string{"Bitte in SecureGo plus freigeben."}) {
		t.Errorf("the program was told %q", approvals)
	}
	if !reflect.DeepEqual(*waits, []time.Duration{3 * time.Second, 5 * time.Second}) {
		t.Errorf("the client waited %v", *waits)
	}
	sync := b.sent(0, "HKIDN")
	if sync.field(2, 0) != "0" || b.sent(0, "HNSHK").field(1, 0) != oneStep || b.sent(0, "HKSYN").field(0, 0) != "0" {
		t.Errorf("the synchronization sent %+v", b.received[0])
	}
	if b.sent(0, "HNSHA").field(2, 0) != "geheim" || b.sent(0, "HKVVB").field(3, 0) != "PRODUKT" {
		t.Error("the first message lacks the PIN or the product ID")
	}
	if b.sent(1, "HNHBK").field(2, 0) != "DIALOG:1" || b.sent(1, "HNHBK").field(3, 0) != "2" {
		t.Errorf("the second message has the header %+v", b.sent(1, "HNHBK").elements)
	}
	open := b.sent(3, "HKTAN")
	if b.sent(3, "HNSHK").field(1, 0) != "946" || open.field(0, 0) != "4" || open.field(1, 0) != "HKIDN" || open.field(10, 0) != "Simons iPhone" {
		t.Errorf("the dialog opened with %+v", b.received[3])
	}
	if b.sent(3, "HKIDN").field(2, 0) != "SYSTEM1" || b.sent(3, "HNHBK").field(2, 0) != "0" {
		t.Error("the dialog did not start with the new customer system ID")
	}
	status := b.sent(4, "HKTAN")
	if status.field(0, 0) != "S" || status.field(4, 0) != "AUFTRAG1" || status.field(5, 0) != "N" {
		t.Errorf("the status request was %+v", status.elements)
	}
	if b.sent(7, "HKEND").field(0, 0) != "DIALOG:1" {
		t.Error("the dialog was not closed")
	}
}

// TestTransactions reads two pages of camt.052, the bank waives the TAN for the opening and for HKCAZ.
func TestTransactions(t *testing.T) {
	first := `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:camt.052.001.08"><BkToCstmrAcctRpt><Rpt>
<Ntry><Amt Ccy="EUR">27.9</Amt><CdtDbtInd>CRDT</CdtDbtInd><Sts><Cd>BOOK</Cd></Sts><BookgDt><Dt>2026-11-13</Dt></BookgDt><ValDt><Dt>2026-11-13</Dt></ValDt>
<AcctSvcrRef>REF-1</AcctSvcrRef><NtryDtls><TxDtls><Refs><EndToEndId>NOTPROVIDED</EndToEndId></Refs>
<RltdPties><Dbtr><Pty><Nm>Universitätsbibliothek Greifswald</Nm></Pty></Dbtr><DbtrAcct><Id><IBAN>DE44150505000000000001</IBAN></Id></DbtrAcct></RltdPties>
<RmtInf><Ustrd>RE-152-11-2026 Bestellung </Ustrd><Ustrd>35255235</Ustrd></RmtInf></TxDtls></NtryDtls></Ntry>
</Rpt></BkToCstmrAcctRpt></Document>`
	second := `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"><BkToCstmrAcctRpt><Rpt>
<Ntry><Amt Ccy="EUR">1234.56</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-11-14</Dt></BookgDt><AddtlNtryInf>Miete</AddtlNtryInf>
<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Hausverwaltung Muster</Nm></Cdtr><CdtrAcct><Id><IBAN>DE89370400440532013000</IBAN></Id></CdtrAcct></RltdPties></TxDtls></NtryDtls></Ntry>
<Ntry><Amt Ccy="EUR">5</Amt><CdtDbtInd>CRDT</CdtDbtInd><BookgDt><Dt>2026-11-14</Dt></BookgDt><AddtlNtryInf>Bareinzahlung</AddtlNtryInf></Ntry>
</Rpt></BkToCstmrAcctRpt></Document>`
	account := Account{IBAN: "DE02120300000000202051", BIC: "GENODEM1GTL", Number: "202051", SubAccount: "", BankCode: "12345678"}
	b := newBank(t,
		with(bankParams("10"), returns("HIRMS", code("3076", "Starke Kundenauthentifizierung nicht notwendig."))),
		[]segment{returns("HIRMS", code("3076", "Keine starke Authentifizierung erforderlich."), code("3040", "Es liegen weitere Informationen vor.", "SEITE2")),
			newSegment("HICAZ", 1, accountElement(account), element{text("urn:iso:std:iso:20022:tech:xsd:camt.052.001.08")}, element{binary([]byte(first))})},
		[]segment{returns("HIRMS", code("0020", "Der Auftrag wurde ausgeführt.")),
			newSegment("HICAZ", 1, accountElement(account), element{text("urn:iso:std:iso:20022:tech:xsd:camt.052.001.02")}, element{binary([]byte(second))})},
		[]segment{returns("HIRMG", code("0100", "Dialog beendet."))},
	)
	c, waits := testClient(b)
	c.State = State{SystemID: "SYSTEM1", TANMethod: "946", TANMedium: "Simons iPhone"}
	transactions, err := c.Transactions(account, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := []Transaction{
		{Amount: 2790, Currency: "EUR", BookingDate: "2026-11-13", ValueDate: "2026-11-13", Name: "Universitätsbibliothek Greifswald", IBAN: "DE44150505000000000001",
			Purpose: "RE-152-11-2026 Bestellung 35255235", EndToEndID: "NOTPROVIDED", Reference: "REF-1"},
		{Amount: -123456, Currency: "EUR", BookingDate: "2026-11-14", ValueDate: "", Name: "Hausverwaltung Muster", IBAN: "DE89370400440532013000",
			Purpose: "Miete", EndToEndID: "", Reference: ""},
		{Amount: 500, Currency: "EUR", BookingDate: "2026-11-14", ValueDate: "", Name: "", IBAN: "", Purpose: "Bareinzahlung", EndToEndID: "", Reference: ""},
	}
	if !reflect.DeepEqual(transactions, want) {
		t.Errorf("got %+v", transactions)
	}
	if len(*waits) != 0 {
		t.Errorf("the client waited %v without an approval", *waits)
	}
	request := b.sent(1, "HKCAZ")
	if request.field(0, 1) != account.IBAN || request.field(1, 0) != "urn:iso:std:iso:20022:tech:xsd:camt.052.001.08" ||
		request.field(3, 0) != "20261101" || request.field(4, 0) != "20261115" || request.field(6, 0) != "" {
		t.Errorf("the first request was %+v", request.elements)
	}
	if b.sent(1, "HKTAN").field(1, 0) != "HKCAZ" || b.sent(2, "HKCAZ").field(6, 0) != "SEITE2" {
		t.Error("HKCAZ went without HKTAN or the second page without the touchdown point")
	}
}

// A wrong PIN must not be tried again, every attempt counts towards the lock of the access.
func TestWrongPIN(t *testing.T) {
	b := newBank(t, []segment{returns("HIRMG", code("9050", "Die Nachricht enthält Fehler.")), returns("HIRMS", code("9942", "PIN falsch."))})
	c, _ := testClient(b)
	_, err := c.Accounts()
	var bankError *Error
	if !errors.As(err, &bankError) || !strings.Contains(err.Error(), "9942 PIN falsch.") {
		t.Fatalf("got %v", err)
	}
	if len(b.received) != 1 || c.State != (State{SystemID: "", TANMethod: "", TANMedium: ""}) {
		t.Errorf("the client sent %d messages and kept %+v", len(b.received), c.State)
	}
}

func TestApprovalTimeout(t *testing.T) {
	pending := []segment{returns("HIRMS", code("3956", "Starke Kundenauthentifizierung noch ausstehend."))}
	b := newBank(t,
		with(bankParams("2"), returns("HIRMS", code("3955", "Sicherheitsfreigabe erfolgt über anderen Kanal.")),
			newSegment("HITAN", 7, element{text("4")}, element{}, element{text("AUFTRAG1")}, element{text("Bitte freigeben.")})),
		pending,
		pending,
	)
	c, _ := testClient(b)
	c.State = State{SystemID: "SYSTEM1", TANMethod: "946", TANMedium: "Simons iPhone"}
	_, err := c.Accounts()
	if err == nil || !strings.Contains(err.Error(), "not given in time") || len(b.received) != 3 {
		t.Errorf("got %v after %d messages", err, len(b.received))
	}
}

// With several active devices the program has to choose, the customer system ID is kept anyway.
func TestSeveralMedia(t *testing.T) {
	b := newBank(t,
		with(bankParams("10"), returns("HIRMS", code("3920", "Zugelassene TAN-Verfahren", "946")), newSegment("HISYN", 4, element{text("SYSTEM1")})),
		[]segment{newSegment("HITAB", 5, element{text("0")}, medium("iPhone", "1"), medium("iPad", "1"))},
		[]segment{returns("HIRMG", code("0100", "Dialog beendet."))},
	)
	c, _ := testClient(b)
	_, err := c.Accounts()
	if err == nil || !strings.Contains(err.Error(), `["iPhone" "iPad"]`) {
		t.Fatalf("got %v", err)
	}
	if c.State != (State{SystemID: "SYSTEM1", TANMethod: "946", TANMedium: ""}) {
		t.Errorf("got the state %+v", c.State)
	}
}

func TestTANMedia(t *testing.T) {
	b := newBank(t,
		with(bankParams("10"), newSegment("HISYN", 4, element{text("SYSTEM1")})),
		[]segment{newSegment("HITAB", 5, element{text("0")}, medium("iPhone", "1"), medium("Altes Handy", "2"))},
		[]segment{returns("HIRMG", code("0100", "Dialog beendet."))},
	)
	c, _ := testClient(b)
	media, err := c.TANMedia()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(media, []TANMedium{{Name: "iPhone", Active: true}, {Name: "Altes Handy", Active: false}}) || c.State.SystemID != "SYSTEM1" {
		t.Errorf("got %+v and %+v", media, c.State)
	}
}

func TestChooseMethod(t *testing.T) {
	methods := map[string]tanMethod{
		"946": {function: "946", name: "SecureGo plus", mediumRequired: true, decoupled: true, maxPolls: 10, firstWait: 0, nextWait: 0},
		"942": {function: "942", name: "mobile TAN", mediumRequired: false, decoupled: false, maxPolls: 0, firstWait: 0, nextWait: 0},
		"944": {function: "944", name: "SecureGo", mediumRequired: false, decoupled: true, maxPolls: 10, firstWait: 0, nextWait: 0},
	}
	tests := []struct {
		allowed []string
		want    string
		err     string
	}{
		{allowed: []string{"999", "942"}, want: "942", err: ""},
		{allowed: []string{"942", "946"}, want: "946", err: ""},
		{allowed: []string{"946", "944"}, want: "", err: "946 SecureGo plus, 944 SecureGo"},
		{allowed: []string{"999", "123"}, want: "", err: "no TAN procedure"},
	}
	for _, test := range tests {
		got, err := chooseMethod(params{methods: methods, allowed: test.allowed, tanNeed: nil, camt: nil, versions: nil})
		if got != test.want || (test.err == "" && err != nil) || (test.err != "" && (err == nil || !strings.Contains(err.Error(), test.err))) {
			t.Errorf("%v: got %q, %v", test.allowed, got, err)
		}
	}
}

func TestParseCents(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{{in: "27.9", want: 2790}, {in: "1234.56", want: 123456}, {in: "5", want: 500}, {in: "\n0.01 ", want: 1}}
	for _, test := range tests {
		got, err := parseCents(test.in)
		if err != nil || got != test.want {
			t.Errorf("%q: got %d, %v", test.in, got, err)
		}
	}
	for _, in := range []string{"1.234", "", "-5", "1,50", ".5"} {
		_, err := parseCents(in)
		if err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}

func TestTransport(t *testing.T) {
	c := New("http://127.0.0.1:1", "12345678", "kunde", "geheim", "")
	_, err := c.Accounts()
	if err == nil || !strings.Contains(err.Error(), "product ID") {
		t.Errorf("got %v without product ID", err)
	}
	tests := []struct {
		status int
		body   string
		want   string
	}{
		{status: http.StatusInternalServerError, body: "", want: "500"},
		{status: http.StatusOK, body: "kein base64!", want: "no base64"},
		{status: http.StatusOK, body: base64.StdEncoding.EncodeToString([]byte("HIRMG:2:2+0010")), want: "without a segment end"},
	}
	for _, test := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte(test.body))
		}))
		c = New(server.URL, "12345678", "kunde", "geheim", "PRODUKT")
		_, err = c.Accounts()
		server.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("got %v; want %q", err, test.want)
		}
	}
}
