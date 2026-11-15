package fints

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// maxPages stops a bank that keeps sending a touchdown point, no account has that many pages in a year.
const maxPages = 1000

// Transaction is a booked entry of the account. Amount is in cents and negative for a debit, Name and IBAN belong to
// the other party: the payer of a credit, the payee of a debit.
type Transaction struct {
	Amount      int64
	Currency    string
	BookingDate string
	ValueDate   string
	Name        string
	IBAN        string
	Purpose     string
	EndToEndID  string
	Reference   string
}

// Transactions reads the booked entries between from and to with HKCAZ version 1 as camt.052.
func (c *Client) Transactions(account Account, from time.Time, to time.Time) ([]Transaction, error) {
	d, err := c.start()
	if err != nil {
		return nil, err
	}
	if !d.params.supports("HKCAZ", 1) || len(d.params.camt) == 0 {
		return nil, errors.New("fints: the bank does not offer HKCAZ version 1 with camt.052")
	}
	format := d.params.camt[len(d.params.camt)-1]
	var transactions []Transaction
	touchdown := ""
	for range maxPages {
		r, err := d.request([]segment{newSegment("HKCAZ", 1,
			accountElement(account),
			element{text(format)},
			element{text("N")},
			element{text(from.Format("20060102"))},
			element{text(to.Format("20060102"))},
			element{},
			element{text(touchdown)},
		)})
		if err != nil {
			return nil, err
		}
		for _, s := range r.find("HICAZ") {
			if len(s.elements) < 3 {
				continue
			}
			for _, report := range s.elements[2] {
				read, err := readCamt052([]byte(report.data))
				if err != nil {
					return nil, err
				}
				transactions = append(transactions, read...)
			}
		}
		more, ok := r.message("3040")
		if !ok || len(more.Params) == 0 {
			return transactions, d.close()
		}
		touchdown = more.Params[0]
	}
	return nil, errors.New("fints: the bank sent more pages than expected")
}

// camtDocument holds the fields of camt.052 the library reads, the tags match every version of the format.
type camtDocument struct {
	Reports []struct {
		Entries []camtEntry `xml:"Ntry"`
	} `xml:"BkToCstmrAcctRpt>Rpt"`
}

type camtEntry struct {
	Amount struct {
		Value    string `xml:",chardata"`
		Currency string `xml:"Ccy,attr"`
	} `xml:"Amt"`
	Indicator   string `xml:"CdtDbtInd"`
	BookingDate string `xml:"BookgDt>Dt"`
	ValueDate   string `xml:"ValDt>Dt"`
	Reference   string `xml:"AcctSvcrRef"`
	Info        string `xml:"AddtlNtryInf"`
	Details     []struct {
		EndToEndID string   `xml:"Refs>EndToEndId"`
		Purpose    []string `xml:"RmtInf>Ustrd"`
		Parties    struct {
			Debtor       camtParty `xml:"Dbtr"`
			DebtorIBAN   string    `xml:"DbtrAcct>Id>IBAN"`
			Creditor     camtParty `xml:"Cdtr"`
			CreditorIBAN string    `xml:"CdtrAcct>Id>IBAN"`
		} `xml:"RltdPties"`
	} `xml:"NtryDtls>TxDtls"`
}

// camtParty covers the name directly below the party (up to version 07) and inside Pty (from version 08).
type camtParty struct {
	Name      string `xml:"Nm"`
	PartyName string `xml:"Pty>Nm"`
}

func readCamt052(data []byte) ([]Transaction, error) {
	var document camtDocument
	err := xml.Unmarshal(data, &document)
	if err != nil {
		return nil, fmt.Errorf("fints: camt.052: %w", err)
	}
	var transactions []Transaction
	for _, report := range document.Reports {
		for _, entry := range report.Entries {
			t, err := readEntry(entry)
			if err != nil {
				return nil, err
			}
			transactions = append(transactions, t)
		}
	}
	return transactions, nil
}

func readEntry(entry camtEntry) (Transaction, error) {
	cents, err := parseCents(entry.Amount.Value)
	if err != nil {
		return Transaction{}, err
	}
	t := Transaction{
		Amount:      cents,
		Currency:    entry.Amount.Currency,
		BookingDate: entry.BookingDate,
		ValueDate:   entry.ValueDate,
		Name:        "",
		IBAN:        "",
		Purpose:     entry.Info,
		EndToEndID:  "",
		Reference:   entry.Reference,
	}
	if entry.Indicator == "DBIT" {
		t.Amount = -cents
	}
	if len(entry.Details) == 0 {
		return t, nil
	}
	details := entry.Details[0]
	t.EndToEndID = details.EndToEndID
	if len(details.Purpose) > 0 {
		t.Purpose = strings.Join(details.Purpose, "")
	}
	other, iban := details.Parties.Debtor, details.Parties.DebtorIBAN
	if entry.Indicator == "DBIT" {
		other, iban = details.Parties.Creditor, details.Parties.CreditorIBAN
	}
	t.Name = other.Name + other.PartyName
	t.IBAN = iban
	return t, nil
}

// parseCents reads a camt amount like 27.9 or 1234.56 without floating point.
func parseCents(amount string) (int64, error) {
	whole, fraction, _ := strings.Cut(strings.TrimSpace(amount), ".")
	if len(fraction) > 2 || whole == "" {
		return 0, fmt.Errorf("fints: the amount %q has no cents format", amount)
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	cents, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil || cents < 0 {
		return 0, fmt.Errorf("fints: the amount %q has no cents format", amount)
	}
	return cents, nil
}
