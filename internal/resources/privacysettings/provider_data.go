// Package privacysettings implements shopify_privacy_settings and shopify_consent_policy.
package privacysettings

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// withClient is the Configure both resources share.
type withClient struct {
	client *client.Client
}

func (w *withClient) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	w.client = c
}
