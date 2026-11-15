// Package fints reads accounts and transactions with FinTS 3.0 PIN/TAN. It has no functions for payments,
// so a program using it cannot move money even with the PIN of the account.
package fints

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The delimiters of FinTS 3.0, a release character makes the next byte part of the value.
const (
	segmentEnd   = '\''
	elementEnd   = '+'
	groupEnd     = ':'
	release      = '?'
	binaryMarker = '@'
)

// sizeDigits is the fixed width of the message size in HNHBK, so the size can be filled in after encoding.
const sizeDigits = 12

// segment is one segment of a message. The head is the first data element group, the elements follow it.
type segment struct {
	head     head
	elements []element
}

// head names the segment. Reference is the number of the segment of the request a response segment answers,
// 0 if it answers the whole message.
type head struct {
	kind      string
	number    int
	version   int
	reference int
}

// element is a data element or a data element group, a group holds several values.
type element []value

// value is text in UTF-8 or binary data, which FinTS sends without release characters.
type value struct {
	data   string
	binary bool
}

func text(data string) value {
	return value{data: data, binary: false}
}

func binary(data []byte) value {
	return value{data: string(data), binary: true}
}

func number(n int) value {
	return text(strconv.Itoa(n))
}

// field returns the value j of element i, an empty string if the bank left it out.
func (s segment) field(i int, j int) string {
	if i >= len(s.elements) || j >= len(s.elements[i]) {
		return ""
	}
	return s.elements[i][j].data
}

// encodeSegment leaves out empty values and elements at the end, FinTS allows that and the banks send it so.
func encodeSegment(s segment) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString(s.head.kind)
	out.WriteByte(groupEnd)
	out.WriteString(strconv.Itoa(s.head.number))
	out.WriteByte(groupEnd)
	out.WriteString(strconv.Itoa(s.head.version))
	if s.head.reference > 0 {
		out.WriteByte(groupEnd)
		out.WriteString(strconv.Itoa(s.head.reference))
	}
	elements := trimElements(s.elements)
	for _, e := range elements {
		out.WriteByte(elementEnd)
		for j, v := range trimValues(e) {
			if j > 0 {
				out.WriteByte(groupEnd)
			}
			err := encodeValue(&out, v)
			if err != nil {
				return nil, fmt.Errorf("segment %s: %w", s.head.kind, err)
			}
		}
	}
	out.WriteByte(segmentEnd)
	return out.Bytes(), nil
}

// encodeValue writes text in ISO 8859-1, the character set of FinTS 3.0.
func encodeValue(out *bytes.Buffer, v value) error {
	if v.binary {
		out.WriteByte(binaryMarker)
		out.WriteString(strconv.Itoa(len(v.data)))
		out.WriteByte(binaryMarker)
		out.WriteString(v.data)
		return nil
	}
	for _, r := range v.data {
		if r < 0 || r > 0xff {
			return fmt.Errorf("the character %q is not part of ISO 8859-1", r)
		}
		switch r {
		case segmentEnd, elementEnd, groupEnd, release, binaryMarker:
			out.WriteByte(release)
		}
		out.WriteByte(byte(r))
	}
	return nil
}

func trimElements(elements []element) []element {
	end := len(elements)
	for end > 0 && len(trimValues(elements[end-1])) == 0 {
		end--
	}
	return elements[:end]
}

func trimValues(e element) element {
	end := len(e)
	for end > 0 && e[end-1].data == "" && !e[end-1].binary {
		end--
	}
	return e[:end]
}

// encodeMessage joins the segments and writes the size of the whole message into the first element of HNHBK.
func encodeMessage(segments []segment) ([]byte, error) {
	if len(segments) == 0 || segments[0].head != (head{kind: "HNHBK", number: 1, version: 3, reference: 0}) || len(segments[0].elements) == 0 {
		return nil, errors.New("a message starts with HNHBK:1:3 and its size")
	}
	// The placeholder keeps the size of the message, the copy keeps the segments of the caller unchanged.
	first := segment{head: segments[0].head, elements: append([]element{{text(strings.Repeat("0", sizeDigits))}}, segments[0].elements[1:]...)}
	var out bytes.Buffer
	for _, s := range append([]segment{first}, segments[1:]...) {
		encoded, err := encodeSegment(s)
		if err != nil {
			return nil, err
		}
		out.Write(encoded)
	}
	message := out.Bytes()
	size := fmt.Sprintf("%0*d", sizeDigits, len(message))
	start := len("HNHBK:1:3+")
	copy(message[start:start+sizeDigits], size)
	return message, nil
}

// parser reads a message byte by byte, the position makes the errors point to the broken place.
// end is the delimiter the last element stopped at.
type parser struct {
	data []byte
	pos  int
	end  byte
}

func parseMessage(data []byte) ([]segment, error) {
	p := parser{data: data, pos: 0, end: 0}
	var segments []segment
	for p.pos < len(p.data) {
		s, err := p.segment()
		if err != nil {
			return nil, err
		}
		segments = append(segments, s)
	}
	return segments, nil
}

func (p *parser) segment() (segment, error) {
	start := p.pos
	var elements []element
	for {
		e, err := p.element()
		if err != nil {
			return segment{}, err
		}
		elements = append(elements, e)
		if p.end == segmentEnd {
			break
		}
	}
	h, err := parseHead(elements[0])
	if err != nil {
		return segment{}, fmt.Errorf("segment at byte %d: %w", start, err)
	}
	return segment{head: h, elements: elements[1:]}, nil
}

// element reads the values up to the next element or segment end.
func (p *parser) element() (element, error) {
	var e element
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		e = append(e, v)
		if p.pos >= len(p.data) {
			return nil, fmt.Errorf("the message ends at byte %d without a segment end", p.pos)
		}
		p.end = p.data[p.pos]
		p.pos++
		if p.end != groupEnd {
			return e, nil
		}
	}
}

func (p *parser) value() (value, error) {
	if p.pos < len(p.data) && p.data[p.pos] == binaryMarker {
		return p.binaryValue()
	}
	var out strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch c {
		case segmentEnd, elementEnd, groupEnd:
			return text(out.String()), nil
		case release:
			p.pos++
			if p.pos >= len(p.data) {
				return value{}, errors.New("the message ends with a release character")
			}
			c = p.data[p.pos]
		}
		out.WriteRune(rune(c))
		p.pos++
	}
	return text(out.String()), nil
}

func (p *parser) binaryValue() (value, error) {
	start := p.pos
	end := bytes.IndexByte(p.data[start+1:], binaryMarker)
	if end < 0 {
		return value{}, fmt.Errorf("binary data at byte %d has no length", start)
	}
	length, err := strconv.Atoi(string(p.data[start+1 : start+1+end]))
	if err != nil || length < 0 {
		return value{}, fmt.Errorf("binary data at byte %d has the length %q", start, p.data[start+1:start+1+end])
	}
	from := start + end + 2
	if from+length > len(p.data) {
		return value{}, fmt.Errorf("binary data at byte %d is longer than the message", start)
	}
	p.pos = from + length
	return binary(p.data[from:p.pos]), nil
}

func parseHead(e element) (head, error) {
	if len(e) < 3 || e[0].data == "" {
		return head{}, errors.New("the segment head needs kind, number and version")
	}
	numbers := make([]int, 0, 3)
	for _, v := range e[1:] {
		n, err := strconv.Atoi(v.data)
		if err != nil {
			return head{}, fmt.Errorf("the segment head %s has the number %q", e[0].data, v.data)
		}
		numbers = append(numbers, n)
	}
	h := head{kind: e[0].data, number: numbers[0], version: numbers[1], reference: 0}
	if len(numbers) > 2 {
		h.reference = numbers[2]
	}
	return h, nil
}
