package checkoutandaccountsconfiguration

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

type userError = client.UpdateCheckoutAndAccountsConfigurationCheckoutAndAccountsConfigurationUpdateCheckoutAndAccountsConfigurationUpdatePayloadUserErrorsCheckoutAndAccountsConfigurationUserError

// configKey is the natural key: a name, or the published configuration when name is empty.
type configKey struct {
	name string
}

func (k configKey) String() string {
	if k.name == "" {
		return "the published configuration"
	}
	return fmt.Sprintf("the configuration named %q", k.name)
}

func parseImportKey(id string) configKey {
	if id == "published" {
		return configKey{}
	}
	return configKey{name: id}
}

// matchConfiguration returns nil when nothing matches and an error listing the GIDs when more than one does.
func matchConfiguration(all []client.CheckoutAndAccountsConfigurationFields, key configKey) (*client.CheckoutAndAccountsConfigurationFields, error) {
	var hits []*client.CheckoutAndAccountsConfigurationFields
	for i := range all {
		c := &all[i]
		if (key.name == "" && c.IsPublished) || (key.name != "" && c.Name == key.name) {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 0:
		return nil, nil
	case 1:
		return hits[0], nil
	}
	ids := make([]string, len(hits))
	for i, c := range hits {
		ids[i] = c.Id
	}
	return nil, fmt.Errorf("%s matches %d configurations (%s); rename all but one in the checkout editor", key, len(hits), strings.Join(ids, ", "))
}

// brandingErrorDiags puts branding errors on the branding attribute, naming the JSON path and Shopify's reason.
func brandingErrorDiags(summary string, errs []userError) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, e := range errs {
		detail := fmt.Sprintf("%s (reason: %s)", e.Message, e.Reason)
		if len(e.Field) > 2 && e.Field[1] == "branding" {
			detail = fmt.Sprintf("%s at %s", detail, strings.Join(e.Field[2:], "."))
		}
		if e.Code != nil && *e.Code == client.CheckoutAndAccountsConfigurationUserErrorCodeTransient {
			detail += ". Shopify reports a temporary failure; apply again."
		}
		if len(e.Field) > 1 && e.Field[1] == "branding" {
			diags.AddAttributeError(path.Root("branding"), summary, detail)
		} else {
			diags.AddError(summary, detail)
		}
	}
	return diags
}
