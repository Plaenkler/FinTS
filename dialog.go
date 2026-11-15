package fints

import (
	"fmt"
	"strconv"
	"strings"
)

// oneStep is the security function without a TAN, banks allow it only for the synchronization.
const oneStep = "999"

// FinTS 3.0 numbers the encryption head and the encrypted data with 998 and 999.
const (
	encryptionNumber = 998
	dataNumber       = 999
)

// dialog is one conversation with the bank. The bank names it in its first answer and counts the messages.
type dialog struct {
	client   *Client
	id       string
	number   int
	function string
	params   params
}

// response is the answer of the bank with the encrypted part unwrapped.
type response struct {
	segments []segment
	messages []Message
}

func newDialog(c *Client, function string) *dialog {
	return &dialog{client: c, id: "0", number: 0, function: function, params: params{}}
}

func newSegment(kind string, version int, elements ...element) segment {
	return segment{head: head{kind: kind, number: 0, version: version, reference: 0}, elements: elements}
}

// open starts the dialog. The synchronization asks for a new customer system ID, a dialog with a TAN procedure
// sends HKTAN, which PSD2 requires for the strong customer authentication.
func (d *dialog) open(sync bool) (response, error) {
	c := d.client
	systemID := c.systemID()
	if sync {
		systemID = "0"
	}
	segments := []segment{
		newSegment("HKIDN", 2, element{text("280"), text(c.BankCode)}, element{text(c.customerID())}, element{text(systemID)}, element{text("1")}),
		newSegment("HKVVB", 3, element{text("0")}, element{text("0")}, element{text("0")}, element{text(c.ProductID)}, element{text(c.ProductVersion)}),
	}
	if d.function != oneStep {
		segments = append(segments, tanSegment("4", "HKIDN", "", c.State.TANMedium))
	}
	if sync {
		segments = append(segments, newSegment("HKSYN", 3, element{text("0")}))
	}
	r, err := d.send(segments)
	if err != nil {
		return response{}, err
	}
	d.params = readParams(r)
	if d.function == oneStep {
		return r, nil
	}
	return d.authorize(r)
}

func (d *dialog) close() error {
	_, err := d.send([]segment{newSegment("HKEND", 1, element{text(d.id)})})
	return err
}

// send wraps the segments into the signature and the formal encryption of PIN/TAN, the transport is already TLS.
func (d *dialog) send(business []segment) (response, error) {
	c := d.client
	d.number++
	now := c.now()
	date := now.Format("20060102")
	clock := now.Format("150405")
	control := strconv.FormatInt(now.UnixNano()%10000000+1, 10)
	profile := element{text("PIN"), text("2")}
	if d.function == oneStep {
		profile = element{text("PIN"), text("1")}
	}
	inner := make([]segment, 0, len(business)+2)
	inner = append(inner, newSegment("HNSHK", 4,
		profile,
		element{text(d.function)},
		element{text(control)},
		element{text("1")},
		element{text("1")},
		element{text("1"), text(""), text(c.systemID())},
		element{text("1")},
		element{text("1"), text(date), text(clock)},
		element{text("1"), text(oneStep), text("1")},
		element{text("6"), text("10"), text("16")},
		element{text("280"), text(c.BankCode), text(c.UserID), text("S"), text("0"), text("0")},
	))
	inner = append(inner, business...)
	inner = append(inner, newSegment("HNSHA", 2, element{text(control)}, element{}, element{text(c.PIN)}))
	var encrypted []byte
	for i, s := range inner {
		s.head.number = i + 2
		encoded, err := encodeSegment(s)
		if err != nil {
			return response{}, err
		}
		encrypted = append(encrypted, encoded...)
	}
	message, err := encodeMessage([]segment{
		{head: head{kind: "HNHBK", number: 1, version: 3, reference: 0}, elements: []element{{}, element{text("300")}, element{text(d.id)}, element{number(d.number)}}},
		{head: head{kind: "HNVSK", number: encryptionNumber, version: 3, reference: 0}, elements: []element{
			profile,
			element{text("998")},
			element{text("1")},
			element{text("1"), text(""), text(c.systemID())},
			element{text("1"), text(date), text(clock)},
			element{text("2"), text("2"), text("13"), binary(make([]byte, 8)), text("5"), text("1")},
			element{text("280"), text(c.BankCode), text(c.UserID), text("V"), text("0"), text("0")},
			element{text("0")},
		}},
		{head: head{kind: "HNVSD", number: dataNumber, version: 1, reference: 0}, elements: []element{{binary(encrypted)}}},
		{head: head{kind: "HNHBS", number: len(inner) + 2, version: 1, reference: 0}, elements: []element{{number(d.number)}}},
	})
	if err != nil {
		return response{}, err
	}
	answer, err := c.post(message)
	if err != nil {
		return response{}, err
	}
	r, err := readResponse(answer)
	if err != nil {
		return response{}, err
	}
	header := r.find("HNHBK")
	if len(header) > 0 && header[0].field(2, 0) != "" {
		d.id = header[0].field(2, 0)
	}
	return r, nil
}

// readResponse unwraps HNVSD and turns the return codes into an error if one of them starts with 9.
func readResponse(data []byte) (response, error) {
	outer, err := parseMessage(data)
	if err != nil {
		return response{}, fmt.Errorf("fints: %w", err)
	}
	var segments []segment
	for _, s := range outer {
		if s.head.kind != "HNVSD" {
			segments = append(segments, s)
			continue
		}
		inner, err := parseMessage([]byte(s.field(0, 0)))
		if err != nil {
			return response{}, fmt.Errorf("fints: encrypted data: %w", err)
		}
		segments = append(segments, inner...)
	}
	r := response{segments: segments, messages: nil}
	failed := false
	for _, s := range segments {
		if s.head.kind != "HIRMG" && s.head.kind != "HIRMS" {
			continue
		}
		for _, e := range s.elements {
			m := Message{Code: valueAt(e, 0), Text: valueAt(e, 2), Params: nil}
			for _, v := range e[min(len(e), 3):] {
				m.Params = append(m.Params, v.data)
			}
			r.messages = append(r.messages, m)
			failed = failed || strings.HasPrefix(m.Code, "9")
		}
	}
	if failed {
		return response{}, &Error{Messages: r.messages}
	}
	return r, nil
}

func valueAt(e element, i int) string {
	if i >= len(e) {
		return ""
	}
	return e[i].data
}

func (r response) find(kind string) []segment {
	var found []segment
	for _, s := range r.segments {
		if s.head.kind == kind {
			found = append(found, s)
		}
	}
	return found
}

func (r response) message(code string) (Message, bool) {
	for _, m := range r.messages {
		if m.Code == code {
			return m, true
		}
	}
	return Message{Code: "", Text: "", Params: nil}, false
}
