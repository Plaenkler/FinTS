# FinTS

> This project was written by Claude Opus 5.5 in Claude Code, based on the FinTS 3.0 specification. Whether that is a mark of quality or the opposite is up to you.

[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![Checks](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml/badge.svg)](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/plaenkler/fints.svg)](https://pkg.go.dev/github.com/plaenkler/fints)

Read-only Go client for [FinTS 3.0](https://www.fints.org) PIN/TAN, it has no functions for payments. It uses only the standard library.

## Installation

```text
go get github.com/plaenkler/fints
```

## Usage

```go
client := fints.New("https://fints1.atruvia.de/cgi-bin/hbciservlet", "12345678", "VR-NetKey", pin, productID)
accounts, err := client.Accounts()
if err != nil {
    log.Fatal(err)
}
log.Print(accounts[0].IBAN)
```

## Example

```go
transactions, err := client.Transactions(accounts[0], time.Now().AddDate(0, 0, -30), time.Now())
if err != nil {
    log.Fatal(err)
}
for _, t := range transactions {
    log.Print(t.Amount, t.Name, t.Purpose) // cents, negative for a debit
}
```

## Tested banks

The library follows the FinTS 3.0 standard. Here is a list of institutions that have been tested:

| Bank | Server | Accounts | Transactions | Approval in the TAN app |
| --- | --- | --- | --- | --- |
| Volksbank in Ostwestfalen eG | Atruvia | Yes | Yes | Not yet requested |
