package fints

import (
	"reflect"
	"strings"
	"testing"
)

// The messages follow the rules of FinTS 3.0 Formals, the values are made up.

// TestEncodeMessage builds a dialog initialization, the size in HNHBK counts the whole message.
func TestEncodeMessage(t *testing.T) {
	segments := []segment{
		{head: head{kind: "HNHBK", number: 1, version: 3, reference: 0}, elements: []element{{text("")}, {text("300")}, {text("0")}, {number(1)}}},
		{head: head{kind: "HKIDN", number: 2, version: 2, reference: 0}, elements: []element{{text("280"), text("12345678")}, {text("9999999999")}, {text("0")}, {text("0")}}},
		{head: head{kind: "HKVVB", number: 3, version: 3, reference: 0}, elements: []element{{text("0")}, {text("0")}, {text("0")}, {text("PRODUKT")}, {text("1.0")}}},
		{head: head{kind: "HNHBS", number: 4, version: 1, reference: 0}, elements: []element{{number(1)}}},
	}
	got, err := encodeMessage(segments)
	if err != nil {
		t.Fatal(err)
	}
	want := "HNHBK:1:3+000000000109+300+0+1'HKIDN:2:2+280:12345678+9999999999+0+0'HKVVB:3:3+0+0+0+PRODUKT+1.0'HNHBS:4:1+1'"
	if string(got) != want || len(want) != 109 {
		t.Errorf("got %s", got)
	}
	if segments[0].elements[0][0].data != "" {
		t.Error("the size changed the segments of the caller")
	}
}

func TestEncodeMessageNeedsHeader(t *testing.T) {
	_, err := encodeMessage([]segment{{head: head{kind: "HKIDN", number: 1, version: 2, reference: 0}, elements: nil}})
	if err == nil {
		t.Error("a message without HNHBK was encoded")
	}
}

// TestEncodeSegment covers the release characters, ISO 8859-1, binary data and the omitted empty values at the end.
func TestEncodeSegment(t *testing.T) {
	tests := []struct {
		name string
		in   segment
		want string
	}{
		{
			name: "release characters",
			in:   segment{head: head{kind: "HKTST", number: 3, version: 1, reference: 0}, elements: []element{{text("Miete 3?+Kaution: 1'2 @home")}}},
			want: "HKTST:3:1+Miete 3???+Kaution?: 1?'2 ?@home'",
		},
		{
			name: "umlaut",
			in:   segment{head: head{kind: "HKTST", number: 3, version: 1, reference: 0}, elements: []element{{text("Müller")}}},
			want: "HKTST:3:1+M\xfcller'",
		},
		{
			name: "binary",
			in:   segment{head: head{kind: "HNVSD", number: 999, version: 1, reference: 0}, elements: []element{{binary([]byte("HNSHK:2:4+PIN:2'"))}}},
			want: "HNVSD:999:1+@16@HNSHK:2:4+PIN:2''",
		},
		{
			name: "empty values",
			in:   segment{head: head{kind: "HKTST", number: 3, version: 1, reference: 0}, elements: []element{{text("a"), text(""), text("b"), text("")}, {text("")}, {text("c")}, {text(""), text("")}, {}}},
			want: "HKTST:3:1+a::b++c'",
		},
		{
			name: "reference",
			in:   segment{head: head{kind: "HIRMS", number: 4, version: 2, reference: 3}, elements: []element{{text("0020"), text(""), text("ok")}}},
			want: "HIRMS:4:2:3+0020::ok'",
		},
	}
	for _, test := range tests {
		got, err := encodeSegment(test.in)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if string(got) != test.want {
			t.Errorf("%s: got %q; want %q", test.name, got, test.want)
		}
	}
}

// The euro sign is missing in ISO 8859-1, sending a substitute would change what the bank receives.
func TestEncodeOutsideLatin1(t *testing.T) {
	_, err := encodeSegment(segment{head: head{kind: "HKTST", number: 3, version: 1, reference: 0}, elements: []element{{text("5 €")}}})
	if err == nil || !strings.Contains(err.Error(), "ISO 8859-1") {
		t.Errorf("got %v", err)
	}
}

// TestParseResponse reads a response with a referenced segment, an escaped dialog ID and umlauts in ISO 8859-1.
func TestParseResponse(t *testing.T) {
	message := "HNHBK:1:3+000000000208+300+DIALOG?:1+2+DIALOG?:1:2'" +
		"HIRMG:2:2+0010::Nachricht entgegengenommen.'" +
		"HIRMS:3:2:3+3920::Zugelassene TAN-Verfahren f\xfcr den Benutzer:942+0020::Der Auftrag wurde ausgef\xfchrt.'" +
		"HNHBS:4:1+2'"
	segments, err := parseMessage([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 4 {
		t.Fatalf("got %d segments", len(segments))
	}
	if segments[0].field(2, 0) != "DIALOG:1" || segments[0].field(4, 0) != "DIALOG:1" || segments[0].field(4, 1) != "2" {
		t.Errorf("the header holds %v", segments[0].elements)
	}
	answer := segments[2]
	if answer.head != (head{kind: "HIRMS", number: 3, version: 2, reference: 3}) {
		t.Errorf("the head is %+v", answer.head)
	}
	if answer.field(0, 0) != "3920" || answer.field(0, 2) != "Zugelassene TAN-Verfahren für den Benutzer" || answer.field(0, 3) != "942" {
		t.Errorf("the first answer is %v", answer.elements[0])
	}
	if answer.field(1, 2) != "Der Auftrag wurde ausgeführt." || answer.field(1, 5) != "" || answer.field(9, 0) != "" {
		t.Errorf("the second answer is %v", answer.elements[1])
	}
}

// Binary data holds delimiters without release characters, the encrypted part of a message is parsed again.
func TestParseBinary(t *testing.T) {
	inner := "HNSHK:2:4+PIN:2+942'HKIDN:3:2+280:12345678+kunde?+1+0+1'"
	message := "HNVSD:999:1+@" + "56" + "@" + inner + "'"
	if len(inner) != 56 {
		t.Fatalf("the inner message has %d bytes", len(inner))
	}
	segments, err := parseMessage([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || !segments[0].elements[0][0].binary || segments[0].field(0, 0) != inner {
		t.Fatalf("got %+v", segments)
	}
	nested, err := parseMessage([]byte(segments[0].field(0, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) != 2 || nested[1].field(1, 0) != "kunde+1" {
		t.Errorf("the inner segments are %+v", nested)
	}
}

func TestRoundTrip(t *testing.T) {
	in := []segment{
		{head: head{kind: "HKTST", number: 2, version: 1, reference: 0}, elements: []element{{text("Grüße: ?'+@"), text("x")}, {binary([]byte{0, '\'', 0xff})}}},
		{head: head{kind: "HITST", number: 3, version: 1, reference: 2}, elements: []element{{text("a")}}},
	}
	var data []byte
	for _, s := range in {
		encoded, err := encodeSegment(s)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, encoded...)
	}
	out, err := parseMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("got %+v; want %+v", out, in)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "no segment end", message: "HNHBS:4:1+2", want: "without a segment end"},
		{name: "release at the end", message: "HNHBS:4:1+2?", want: "release character"},
		{name: "binary without length", message: "HNVSD:999:1+@12", want: "has no length"},
		{name: "binary length no number", message: "HNVSD:999:1+@x@ab'", want: "has the length"},
		{name: "binary too long", message: "HNVSD:999:1+@99@ab'", want: "longer than the message"},
		{name: "head without version", message: "HNHBS:4+2'", want: "kind, number and version"},
		{name: "head with text number", message: "HNHBS:x:1+2'", want: "has the number"},
	}
	for _, test := range tests {
		_, err := parseMessage([]byte(test.message))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got %v; want %q", test.name, err, test.want)
		}
	}
}
