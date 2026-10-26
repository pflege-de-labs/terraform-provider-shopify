package checkoutandaccountsconfiguration

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
)

// writeOnly are input paths the read can't compare: images and font groups read back as other types, aspectRatio isn't readable.
var writeOnly = []string{
	"components.favicon",
	"components.header.logo.image",
	"components.merchandiseThumbnail.aspectRatio",
	"designTokens.typography.primary",
	"designTokens.typography.secondary",
	"surfaces.checkout.components.header.cartLink.image",
	"surfaces.checkout.components.header.logo.image",
	"surfaces.checkout.components.orderSummary.backgroundImage",
	"surfaces.customerAccounts.components.header.logo.image",
	"surfaces.signIn.components.header.logo.image",
	"surfaces.signIn.components.main.backgroundImage",
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// isWriteOnly reports whether p is a write-only path or lies below one.
func isWriteOnly(p []string) bool {
	s := strings.Join(p, ".")
	for _, w := range writeOnly {
		if s == w || strings.HasPrefix(s, w+".") {
			return true
		}
	}
	return false
}

// decode parses JSON into maps, float64s, strings, bools and nils; empty input is null.
func decode(raw []byte) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var v any
	err := json.Unmarshal(raw, &v)
	return v, err
}

// brandingObjectError returns why s isn't a JSON object, or nil.
func brandingObjectError(s string) error {
	v, err := decode([]byte(s))
	if err != nil {
		return err
	}
	if _, ok := v.(map[string]any); !ok {
		return errors.New("branding must be a JSON object")
	}
	return nil
}

// normalise drops nulls and objects left empty; it returns nil when nothing is left.
func normalise(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	for k, c := range m {
		if c = normalise(c); c != nil {
			out[k] = c
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// liveBranding is the live branding as import and list emit it, or "" when there is none.
func liveBranding(raw []byte) (string, error) {
	v, err := decode(raw)
	if err != nil {
		return "", err
	}
	v = normalise(v)
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// projectBranding is what Read stores: the state's paths with live values, or the state string itself when nothing drifted.
func projectBranding(state string, live []byte) (string, error) {
	s, err := decode([]byte(state))
	if err != nil {
		return "", err
	}
	l, err := decode(live)
	if err != nil {
		return "", err
	}
	projected := project(s, normalise(l), nil)
	if leavesEqual(s, projected) {
		return state, nil
	}
	b, err := json.Marshal(projected)
	return string(b), err
}

// project takes each state leaf from live (nil if absent); write-only paths keep the state value.
func project(state, live any, at []string) any {
	if isWriteOnly(at) {
		return state
	}
	sm, ok := state.(map[string]any)
	if !ok {
		return live
	}
	lm, _ := live.(map[string]any)
	out := make(map[string]any, len(sm))
	for k, v := range sm {
		out[k] = project(v, lm[k], append(at[:len(at):len(at)], k))
	}
	return out
}

// brandingEqual compares two branding strings leaf by leaf; strings that aren't JSON compare exactly.
func brandingEqual(a, b string) bool {
	av, aerr := decode([]byte(a))
	bv, berr := decode([]byte(b))
	if aerr != nil || berr != nil {
		return a == b
	}
	return leavesEqual(av, bv)
}

// leavesEqual needs the same leaf paths on both sides; empty objects have no leaves, a null is one.
func leavesEqual(a, b any) bool {
	al, bl := map[string]any{}, map[string]any{}
	flatten(a, "", al)
	flatten(b, "", bl)
	if len(al) != len(bl) {
		return false
	}
	for p, av := range al {
		bv, ok := bl[p]
		if !ok || !leafEqual(av, bv) {
			return false
		}
	}
	return true
}

func flatten(v any, at string, out map[string]any) {
	m, ok := v.(map[string]any)
	if !ok {
		out[at] = v
		return
	}
	for k, c := range m {
		flatten(c, at+"\x00"+k, out)
	}
}

// leafEqual: decoded numbers are float64, so they compare numerically; hex colours ignore case.
func leafEqual(a, b any) bool {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok && hexColor.MatchString(as) && hexColor.MatchString(bs) {
		return strings.EqualFold(as, bs)
	}
	return reflect.DeepEqual(a, b)
}
