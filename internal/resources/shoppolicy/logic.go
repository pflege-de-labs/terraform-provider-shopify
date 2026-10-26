package shoppolicy

import (
	"fmt"
	"slices"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

var policyTypes = []client.ShopPolicyType{
	client.ShopPolicyTypeRefundPolicy,
	client.ShopPolicyTypeShippingPolicy,
	client.ShopPolicyTypePrivacyPolicy,
	client.ShopPolicyTypeTermsOfService,
	client.ShopPolicyTypeTermsOfSale,
	client.ShopPolicyTypeLegalNotice,
	client.ShopPolicyTypeSubscriptionPolicy,
	client.ShopPolicyTypeContactInformation,
}

func policyTypeNames() []string {
	out := make([]string, len(policyTypes))
	for i, t := range policyTypes {
		out[i] = string(t)
	}
	return out
}

// findPolicy matches the GID first, then the type; an empty body counts as no policy, because rows can't be deleted.
func findPolicy(ps []client.ShopPolicyFields, id string, typ client.ShopPolicyType) *client.ShopPolicyFields {
	ps = withText(ps)
	if i := slices.IndexFunc(ps, func(p client.ShopPolicyFields) bool { return id != "" && p.Id == id }); i >= 0 {
		return &ps[i]
	}
	if i := slices.IndexFunc(ps, func(p client.ShopPolicyFields) bool { return p.Type == typ }); i >= 0 {
		return &ps[i]
	}
	return nil
}

func withText(ps []client.ShopPolicyFields) []client.ShopPolicyFields {
	return slices.DeleteFunc(slices.Clone(ps), func(p client.ShopPolicyFields) bool { return p.Body == "" })
}

type createAction int

const (
	writeBody createAction = iota
	// conflict means the type already has text and the config didn't ask to take it over.
	conflict
	keepBody
)

func planCreate(existing *client.ShopPolicyFields, body string, adopt bool) createAction {
	switch {
	case existing == nil:
		return writeBody
	case !adopt:
		return conflict
	case existing.Body == body:
		return keepBody
	}
	return writeBody
}

func displayName(p *client.ShopPolicyFields) string {
	return fmt.Sprintf("%s (%s)", p.Type, p.Title)
}

func parseImportKey(s string) (client.ShopPolicyType, error) {
	t := client.ShopPolicyType(s)
	if !slices.Contains(policyTypes, t) {
		return "", fmt.Errorf("import key %q must be one of %v", s, policyTypeNames())
	}
	return t, nil
}
