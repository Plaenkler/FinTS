# FinTS

[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![Checks](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml/badge.svg)](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/plaenkler/fints.svg)](https://pkg.go.dev/github.com/plaenkler/fints)

Go library that reads accounts and transactions from German banks with [FinTS 3.0](https://www.fints.org) PIN/TAN. It uses only the standard library.

> **Status:** in development. The dialog, the decoupled TAN procedure, accounts and transactions are implemented and tested against a simulated bank, not yet against a real one.

## Usage

```go
client := fints.New("https://fints1.atruvia.de/cgi-bin/hbciservlet", "12345678", "VR-NetKey", pin, productID)
client.State = saved // empty on the first call
client.Approve = func(text string) {
    log.Print(text) // e.g. "Bitte in SecureGo plus freigeben."
}
accounts, err := client.Accounts()
if err != nil {
    log.Fatal(err)
}
transactions, err := client.Transactions(accounts[0], time.Now().AddDate(0, 0, -30), time.Now())
if err != nil {
    log.Fatal(err)
}
saved = client.State // keep it for the next call
```

- Each call opens its own dialog and closes it again, the client is not safe for concurrent use.
- `State` holds the customer system ID, the TAN procedure and the TAN medium. The first call fetches them, the program saves them, so later calls skip that step. None of it is secret.
- If the bank allows several TAN procedures or several devices are active, the call fails with the list and the program sets `State.TANMethod` or `State.TANMedium`. `TANMedia` lists the devices for such a choice.
- A response with a return code starting with 9 is returned as `*fints.Error` with all messages of the bank. The library never repeats a request after an error, so a wrong PIN costs exactly one attempt.
- Waiting for the approval in the app ends at the limits the bank sets in its parameters, the library uses no `context`.

## Read-only by design

The library has no functions for transfers, standing orders or any other payment. A program using it cannot move money, even though it holds the PIN of the account. Only these business transactions are planned:

| Segment | Purpose |
| ------- | ------- |
| HKIDN, HKVVB, HKSYN, HKEND | Open and close a dialog, fetch the customer system ID |
| HKTAN | Strong customer authentication with a decoupled TAN procedure such as VR SecureGo plus |
| HKTAB | List the TAN media, so the device for the approval is chosen from the names the bank knows |
| HKSPA | List the accounts of the access |
| HKCAZ | Fetch the transactions as camt.052, structured with name and remittance information |
| HKKAZ | Fetch the transactions as MT940, only for banks without camt |

## Product registration

Banks reject FinTS software that is not registered with the Deutsche Kreditwirtschaft. The registration is free on [fints.org](https://www.fints.org). Every program using this library passes its own product ID, the library has none built in.

## Strong customer authentication

Under PSD2 the bank asks for an approval in the TAN app when the access is set up and again after 90 to 180 days, depending on the bank. The library starts the approval, reports the text of the bank and waits until it is confirmed in the app.

## Development

- The protocol is tested with recorded and constructed messages, never against a real account in the tests.
- Three wrong PINs lock the online banking access, so a first connection to a real bank should be started by hand.
