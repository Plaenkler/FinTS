# FinTS

[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![Checks](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml/badge.svg)](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/plaenkler/fints.svg)](https://pkg.go.dev/github.com/plaenkler/fints)

Go library that reads accounts and transactions from German banks with [FinTS 3.0](https://www.fints.org) PIN/TAN. It uses only the standard library.

> **Status:** in development, not usable yet. The message syntax is done, the dialog and the TAN procedure follow.

## Read-only by design

The library has no functions for transfers, standing orders or any other payment. A program using it cannot move money, even though it holds the PIN of the account. Only these business transactions are planned:

| Segment | Purpose |
| ------- | ------- |
| HKIDN, HKVVB, HKSYN, HKEND | Open and close a dialog, fetch the customer system ID |
| HKTAN | Strong customer authentication with a decoupled TAN procedure such as VR SecureGo plus |
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
