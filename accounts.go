package fints

import (
	"errors"
)

// Account is a SEPA account of the access as HISPA lists it.
type Account struct {
	IBAN       string
	BIC        string
	Number     string
	SubAccount string
	BankCode   string
}

// Accounts lists the SEPA accounts with HKSPA version 1.
func (c *Client) Accounts() ([]Account, error) {
	d, err := c.start()
	if err != nil {
		return nil, err
	}
	if !d.params.supports("HKSPA", 1) {
		return nil, errors.New("fints: the bank does not offer HKSPA version 1")
	}
	r, err := d.request([]segment{newSegment("HKSPA", 1)})
	if err != nil {
		return nil, err
	}
	var accounts []Account
	for _, s := range r.find("HISPA") {
		for _, e := range s.elements {
			accounts = append(accounts, readAccount(e))
		}
	}
	return accounts, d.close()
}

// readAccount reads the international account connection: SEPA flag, IBAN, BIC, number, sub account, country, bank code.
func readAccount(e element) Account {
	return Account{IBAN: valueAt(e, 1), BIC: valueAt(e, 2), Number: valueAt(e, 3), SubAccount: valueAt(e, 4), BankCode: valueAt(e, 6)}
}

// accountElement writes the account connection of the orders, which has no SEPA flag unlike HISPA.
func accountElement(a Account) element {
	return element{text(a.IBAN), text(a.BIC), text(a.Number), text(a.SubAccount), text("280"), text(a.BankCode)}
}
