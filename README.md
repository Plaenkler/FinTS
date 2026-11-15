# FinTS

> This project was written by Claude Opus 5.5 in Claude Code, based on the FinTS 3.0 specification. Whether that is a mark of quality or the opposite is up to you.

[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![Checks](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml/badge.svg)](https://github.com/Plaenkler/FinTS/actions/workflows/checks.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/plaenkler/fints.svg)](https://pkg.go.dev/github.com/plaenkler/fints)

Read-only Go client for [FinTS 3.0](https://www.fints.org) PIN/TAN. It lists accounts and fetches transactions, it has no functions for payments. It uses only the standard library.

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

The product ID is the registration number of the software at the [Deutsche Kreditwirtschaft](https://www.fints.org), banks reject software without one.

- Each call opens a dialog and closes it again. The first call fetches the customer system ID, the TAN procedure and the TAN medium into `State`, a program saves it for the next call.
- When the bank asks for an approval in the TAN app, `Approve` is called with its text and the client waits until it is given.
- A response with a return code starting with 9 is returned as `*fints.Error`. The client never repeats a request after an error, so a wrong PIN costs exactly one attempt.
- The client is not safe for concurrent use and works without `context`.

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
