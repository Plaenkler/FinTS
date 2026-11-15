package fints

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Without limits in HITANS the status is asked every few seconds for about five minutes.
const (
	defaultPolls = 100
	minWait      = 3 * time.Second
)

// The field positions of one medium in HITAB version 5, the account inside takes four values.
const (
	mediumStatus = 1
	mediumName   = 13
)

// TANMedium is a device registered for the TAN procedure, the bank only sends approvals to an active one.
type TANMedium struct {
	Name   string
	Active bool
}

// tanSegment builds HKTAN version 7. Process 4 starts the strong customer authentication for a segment,
// process S asks for the status of a decoupled approval.
func tanSegment(process string, kind string, reference string, medium string) segment {
	further := ""
	if process == "S" {
		further = "N"
	}
	return newSegment("HKTAN", 7,
		element{text(process)},
		element{text(kind)},
		element{},
		element{},
		element{text(reference)},
		element{text(further)},
		element{},
		element{},
		element{},
		element{},
		element{text(medium)},
	)
}

// request sends business segments and adds HKTAN if the bank wants a TAN for them.
func (d *dialog) request(business []segment) (response, error) {
	kind := business[0].head.kind
	if !d.params.tanNeed[kind] {
		return d.send(business)
	}
	r, err := d.send(append(business, tanSegment("4", kind, "", d.client.State.TANMedium)))
	if err != nil {
		return response{}, err
	}
	return d.authorize(r)
}

// authorize waits for the approval in the TAN app if the bank asks for one. The last status answer then holds
// the result of the original request.
func (d *dialog) authorize(r response) (response, error) {
	_, pending := r.message("3955")
	if !pending {
		return r, nil
	}
	answers := r.find("HITAN")
	if len(answers) == 0 {
		return response{}, errors.New("fints: the bank asks for an approval without HITAN")
	}
	reference := answers[0].field(2, 0)
	d.client.Approve(answers[0].field(3, 0))
	method := d.params.methods[d.function]
	polls := method.maxPolls
	if polls <= 0 {
		polls = defaultPolls
	}
	wait := method.firstWait
	for range polls {
		d.client.sleep(max(wait, minWait))
		next, err := d.send([]segment{tanSegment("S", "", reference, d.client.State.TANMedium)})
		if err != nil {
			return response{}, err
		}
		_, waiting := next.message("3956")
		if !waiting {
			return next, nil
		}
		wait = method.nextWait
	}
	return response{}, errors.New("fints: the approval in the TAN app was not given in time")
}

// chooseMethod takes the only allowed procedure or the only decoupled one, otherwise the program has to choose.
func chooseMethod(p params) (string, error) {
	var allowed, decoupled, names []string
	for _, function := range p.allowed {
		method, ok := p.methods[function]
		if function == oneStep || !ok {
			continue
		}
		allowed = append(allowed, function)
		names = append(names, function+" "+method.name)
		if method.decoupled {
			decoupled = append(decoupled, function)
		}
	}
	switch {
	case len(allowed) == 1:
		return allowed[0], nil
	case len(decoupled) == 1:
		return decoupled[0], nil
	case len(allowed) == 0:
		return "", errors.New("fints: the bank allows no TAN procedure for this access")
	}
	return "", fmt.Errorf("fints: set State.TANMethod to one of %s", strings.Join(names, ", "))
}

// media reads the devices with HKTAB version 5, asking for all media of all classes.
func (d *dialog) media() ([]TANMedium, error) {
	if !d.params.supports("HKTAB", 5) {
		return nil, errors.New("fints: the bank does not offer HKTAB version 5")
	}
	r, err := d.send([]segment{newSegment("HKTAB", 5, element{text("0")}, element{text("A")})})
	if err != nil {
		return nil, err
	}
	var media []TANMedium
	for _, s := range r.find("HITAB") {
		for _, e := range s.elements[min(len(s.elements), 1):] {
			media = append(media, TANMedium{Name: valueAt(e, mediumName), Active: valueAt(e, mediumStatus) == "1"})
		}
	}
	return media, nil
}

// prepare runs a dialog without TAN when the program knows no customer system ID, TAN procedure or TAN medium yet.
// The state only changes when the dialog succeeded.
func (c *Client) prepare() error {
	d := newDialog(c, oneStep)
	sync := c.State.SystemID == ""
	r, err := d.open(sync)
	if err != nil {
		return err
	}
	next := c.State
	if sync {
		synced := r.find("HISYN")
		if len(synced) == 0 || synced[0].field(0, 0) == "" {
			return errors.New("fints: the bank sent no customer system ID")
		}
		next.SystemID = synced[0].field(0, 0)
	}
	if next.TANMethod == "" {
		next.TANMethod, err = chooseMethod(d.params)
		if err != nil {
			return err
		}
	}
	var active []string
	if d.params.methods[next.TANMethod].mediumRequired && next.TANMedium == "" {
		media, err := d.media()
		if err != nil {
			return err
		}
		for _, m := range media {
			if m.Active {
				active = append(active, m.Name)
			}
		}
		if len(active) == 1 {
			next.TANMedium = active[0]
		}
	}
	err = d.close()
	if err != nil {
		return err
	}
	c.State = next
	if d.params.methods[next.TANMethod].mediumRequired && next.TANMedium == "" {
		return fmt.Errorf("fints: set State.TANMedium to one of the active devices %q", active)
	}
	return nil
}

// TANMedia lists the devices of the access, so the program can let the user choose one when several are active.
func (c *Client) TANMedia() ([]TANMedium, error) {
	d := newDialog(c, oneStep)
	sync := c.State.SystemID == ""
	r, err := d.open(sync)
	if err != nil {
		return nil, err
	}
	media, err := d.media()
	if err != nil {
		return nil, err
	}
	synced := r.find("HISYN")
	if sync && len(synced) > 0 {
		c.State.SystemID = synced[0].field(0, 0)
	}
	return media, d.close()
}

// start opens a dialog with the strong customer authentication, after a preparation if the state is incomplete.
func (c *Client) start() (*dialog, error) {
	if c.State.SystemID == "" || c.State.TANMethod == "" || c.State.TANMedium == "" {
		err := c.prepare()
		if err != nil {
			return nil, err
		}
	}
	d := newDialog(c, c.State.TANMethod)
	_, err := d.open(false)
	if err != nil {
		return nil, err
	}
	return d, nil
}
