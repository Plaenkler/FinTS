package fints

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxResponse limits what a bank may send, a year of camt transactions stays far below it.
const maxResponse = 64 << 20

// Client reads from one online banking access. Each call opens its own dialog and closes it again.
// It is not safe for concurrent use, the bank allows one dialog per access at a time.
type Client struct {
	// URL is the FinTS address of the bank, for the Volksbanken a server of Atruvia.
	URL string
	// BankCode is the German Bankleitzahl, UserID the login name of the online banking.
	BankCode string
	UserID   string
	// CustomerID is empty if it equals the UserID, as with most banks.
	CustomerID string
	PIN        string
	// ProductID is the registration number of the Deutsche Kreditwirtschaft, the bank rejects software without one.
	ProductID      string
	ProductVersion string
	// State changes during a call, the program saves it afterwards, so the next call skips the synchronization.
	State State
	// Approve is called when the bank waits for an approval in the TAN app, with the text of the bank.
	Approve    func(text string)
	HTTPClient *http.Client
	now        func() time.Time
	sleep      func(time.Duration)
}

// State is what a program keeps between the calls, none of it is secret.
type State struct {
	// SystemID identifies this program at the bank, it is fetched in the first call.
	SystemID string
	// TANMethod is the security function of the TAN procedure, e.g. 946 for SecureGo plus at the Volksbanken.
	TANMethod string
	// TANMedium is the device of the TAN app, only set if the bank asks for it.
	TANMedium string
}

// Error holds the messages of the bank for a failed request. The library never repeats a request after an error,
// so a wrong PIN costs exactly one attempt.
type Error struct {
	Messages []Message
}

// Message is a return code of the bank, codes starting with 0 report success, 3 a note and 9 an error.
type Message struct {
	Code   string
	Text   string
	Params []string
}

func (e *Error) Error() string {
	parts := make([]string, 0, len(e.Messages))
	for _, m := range e.Messages {
		parts = append(parts, m.Code+" "+m.Text)
	}
	return "fints: " + strings.Join(parts, "; ")
}

func New(url string, bankCode string, userID string, pin string, productID string) *Client {
	return &Client{
		URL:            url,
		BankCode:       bankCode,
		UserID:         userID,
		CustomerID:     "",
		PIN:            pin,
		ProductID:      productID,
		ProductVersion: "1.0",
		State:          State{SystemID: "", TANMethod: "", TANMedium: ""},
		Approve:        func(string) {},
		HTTPClient:     &http.Client{Timeout: time.Minute},
		now:            time.Now,
		sleep:          time.Sleep,
	}
}

func (c *Client) customerID() string {
	if c.CustomerID == "" {
		return c.UserID
	}
	return c.CustomerID
}

// post sends a message as base64, the transport FinTS defines for HTTPS.
func (c *Client) post(message []byte) ([]byte, error) {
	if c.ProductID == "" {
		return nil, errors.New("fints: the product ID of the registration is missing")
	}
	body := base64.StdEncoding.EncodeToString(message)
	response, err := c.HTTPClient.Post(c.URL, "application/octet-stream", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("fints: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse))
	if err != nil {
		return nil, fmt.Errorf("fints: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fints: the bank answered %s", response.Status)
	}
	// Some banks break the base64 into lines.
	decoded, err := base64.StdEncoding.DecodeString(string(bytes.Join(bytes.Fields(data), nil)))
	if err != nil {
		return nil, fmt.Errorf("fints: the answer is no base64: %w", err)
	}
	return decoded, nil
}

// systemID is 0 until the synchronization, FinTS uses it for a program the bank does not know yet.
func (c *Client) systemID() string {
	if c.State.SystemID == "" {
		return "0"
	}
	return c.State.SystemID
}
