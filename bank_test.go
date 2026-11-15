package fints

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// bank plays the bank: it answers each message with the next reply and keeps the unwrapped segments it received.
// The replies follow the segment layouts of FinTS 3.0, the values are made up.
type bank struct {
	t        *testing.T
	replies  [][]segment
	received [][]segment
	server   *httptest.Server
}

func newBank(t *testing.T, replies ...[]segment) *bank {
	t.Helper()
	b := &bank{t: t, replies: replies, received: nil, server: nil}
	b.server = httptest.NewServer(http.HandlerFunc(b.answer))
	t.Cleanup(b.server.Close)
	return b
}

func (b *bank) answer(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		b.t.Error(err)
		return
	}
	message, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		b.t.Error(err)
		return
	}
	received, err := readResponse(message)
	if err != nil {
		b.t.Error(err)
		return
	}
	b.received = append(b.received, received.segments)
	if len(b.replies) == 0 {
		b.t.Error("the client sent more messages than the bank expected")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	reply := b.replies[0]
	b.replies = b.replies[1:]
	var inner []byte
	for _, s := range reply {
		encoded, err := encodeSegment(s)
		if err != nil {
			b.t.Error(err)
			return
		}
		inner = append(inner, encoded...)
	}
	number := strconv.Itoa(len(b.received))
	encoded, err := encodeMessage([]segment{
		{head: head{kind: "HNHBK", number: 1, version: 3, reference: 0}, elements: []element{{}, element{text("300")}, element{text("DIALOG:1")}, element{text(number)}, element{text("DIALOG:1"), text(number)}}},
		{head: head{kind: "HNVSD", number: dataNumber, version: 1, reference: 0}, elements: []element{{binary(inner)}}},
		newSegment("HNHBS", 1, element{text(number)}),
	})
	if err != nil {
		b.t.Error(err)
		return
	}
	_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(encoded)))
}

// sent returns the first segment of a kind in message i of the client.
func (b *bank) sent(i int, kind string) segment {
	b.t.Helper()
	if i >= len(b.received) {
		b.t.Fatalf("the client sent %d messages, not %d", len(b.received), i+1)
	}
	for _, s := range b.received[i] {
		if s.head.kind == kind {
			return s
		}
	}
	b.t.Fatalf("message %d has no %s", i+1, kind)
	return segment{}
}

func testClient(b *bank) (*Client, *[]time.Duration) {
	c := New(b.server.URL, "12345678", "kunde", "geheim", "PRODUCT")
	c.now = func() time.Time {
		return time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)
	}
	var waits []time.Duration
	c.sleep = func(d time.Duration) {
		waits = append(waits, d)
	}
	return c, &waits
}

func returns(kind string, codes ...element) segment {
	return newSegment(kind, 2, codes...)
}

func code(code string, message string, params ...string) element {
	e := make(element, 0, 3+len(params))
	e = append(e, text(code), text(""), text(message))
	for _, p := range params {
		e = append(e, text(p))
	}
	return e
}

// method builds the 26 fields of a procedure in HITANS version 7.
func method(function string, name string, medium string, polls string) []value {
	fields := make([]value, methodFields)
	for i := range fields {
		fields[i] = text("")
	}
	fields[methodFunction] = text(function)
	fields[1] = text("2")
	fields[methodName] = text(name)
	fields[methodMedium] = text(medium)
	fields[methodMaxPolls] = text(polls)
	fields[methodFirstWait] = text("2")
	fields[methodNextWait] = text("5")
	return fields
}

// bankParams are the bank parameters of every dialog: SecureGo plus as decoupled procedure with a required medium,
// a TAN for HKCAZ but not for HKSPA and HKTAB.
func bankParams(polls string) []segment {
	procedures := make(element, 0, 3+2*methodFields)
	procedures = append(procedures, text("N"), text("N"), text("0"))
	procedures = append(procedures, method("946", "SecureGo plus", "2", polls)...)
	procedures = append(procedures, method("942", "mobile TAN", "0", "")...)
	return []segment{
		newSegment("HITANS", 7, element{text("1")}, element{text("1")}, element{text("0")}, procedures),
		newSegment("HIPINS", 1, element{text("1")}, element{text("1")}, element{text("0")},
			element{text("5"), text("20"), text("6"), text("VR-NetKey"), text(""), text("HKSPA"), text("N"), text("HKCAZ"), text("J"), text("HKTAB"), text("N")}),
		newSegment("HISPAS", 1, element{text("1")}, element{text("1")}, element{text("0")}, element{text("J")}),
		newSegment("HICAZS", 1, element{text("1")}, element{text("1")}, element{text("0")},
			element{text("450"), text("N"), text("N"), text("urn:iso:std:iso:20022:tech:xsd:camt.052.001.02"), text("urn:iso:std:iso:20022:tech:xsd:camt.052.001.08")}),
		newSegment("HITABS", 5, element{text("1")}, element{text("1")}, element{text("0")}, element{text("N")}),
	}
}

func with(base []segment, more ...segment) []segment {
	return append(append([]segment{}, base...), more...)
}

func medium(name string, status string) element {
	e := make(element, mediumName+1)
	for i := range e {
		e[i] = text("")
	}
	e[0] = text("G")
	e[mediumStatus] = text(status)
	e[mediumName] = text(name)
	return e
}
