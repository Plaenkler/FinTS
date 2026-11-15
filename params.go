package fints

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// The field positions of one TAN procedure in HITANS version 7, each procedure has 26 fields.
const (
	methodFields       = 26
	methodFunction     = 0
	methodName         = 5
	methodMedium       = 18
	methodMaxPolls     = 21
	methodFirstWait    = 22
	methodNextWait     = 23
	methodParamsOffset = 3
)

// camtPrefix starts the names of the camt.052 formats, the account report HKCAZ returns.
const camtPrefix = "urn:iso:std:iso:20022:tech:xsd:camt.052."

// params are the bank parameters a dialog needs. The library always asks for version 0, so the bank sends them
// with every dialog and nothing has to be stored.
type params struct {
	methods  map[string]tanMethod
	allowed  []string
	tanNeed  map[string]bool
	camt     []string
	versions map[string][]int
}

// tanMethod is a TAN procedure of the bank. A decoupled procedure is approved in an app,
// the bank tells how often and how fast its status may be asked.
type tanMethod struct {
	function       string
	name           string
	mediumRequired bool
	decoupled      bool
	maxPolls       int
	firstWait      time.Duration
	nextWait       time.Duration
}

func readParams(r response) params {
	p := params{methods: map[string]tanMethod{}, allowed: nil, tanNeed: map[string]bool{}, camt: nil, versions: map[string][]int{}}
	for _, s := range r.segments {
		kind := s.head.kind
		if len(kind) == 6 && strings.HasPrefix(kind, "HI") && strings.HasSuffix(kind, "S") {
			p.versions["HK"+kind[2:5]] = append(p.versions["HK"+kind[2:5]], s.head.version)
		}
		switch {
		case kind == "HITANS" && s.head.version == 7:
			readMethods(s, p.methods)
		case kind == "HIPINS":
			readTANNeed(s, p.tanNeed)
		case kind == "HICAZS":
			p.camt = append(p.camt, readCamt(s)...)
		}
	}
	allowed, ok := r.message("3920")
	if ok {
		p.allowed = allowed.Params
	}
	slices.Sort(p.camt)
	return p
}

// readMethods walks the procedures of HITANS, the bank may leave out empty fields at the end of the last one.
func readMethods(s segment, methods map[string]tanMethod) {
	if len(s.elements) <= methodParamsOffset {
		return
	}
	values := s.elements[methodParamsOffset]
	for start := 3; start < len(values); start += methodFields {
		m := tanMethod{
			function:       valueAt(values, start+methodFunction),
			name:           valueAt(values, start+methodName),
			mediumRequired: valueAt(values, start+methodMedium) == "2",
			decoupled:      valueAt(values, start+methodMaxPolls) != "",
			maxPolls:       atoi(valueAt(values, start+methodMaxPolls)),
			firstWait:      time.Duration(atoi(valueAt(values, start+methodFirstWait))) * time.Second,
			nextWait:       time.Duration(atoi(valueAt(values, start+methodNextWait))) * time.Second,
		}
		methods[m.function] = m
	}
}

// readTANNeed reads the pairs of segment and J or N, after the PIN and ID settings of the bank.
func readTANNeed(s segment, need map[string]bool) {
	for _, e := range s.elements {
		for i := 0; i+1 < len(e); i++ {
			kind := e[i].data
			if len(kind) == 5 && strings.HasPrefix(kind, "HK") && (e[i+1].data == "J" || e[i+1].data == "N") {
				need[kind] = e[i+1].data == "J"
			}
		}
	}
}

func readCamt(s segment) []string {
	var formats []string
	for _, e := range s.elements {
		for _, v := range e {
			if strings.HasPrefix(v.data, camtPrefix) {
				formats = append(formats, v.data)
			}
		}
	}
	return formats
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// supports tells whether the bank offers the version of a business transaction the library implements.
func (p params) supports(kind string, version int) bool {
	return slices.Contains(p.versions[kind], version)
}
