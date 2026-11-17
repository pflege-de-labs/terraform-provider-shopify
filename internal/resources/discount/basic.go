package discount

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
	"github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"
)

const whatBasic = "basic discount"

var (
	_ resource.ResourceWithConfigure   = &BasicResource{}
	_ resource.ResourceWithImportState = &BasicResource{}
	_ resource.ResourceWithIdentity    = &BasicResource{}
	_ resource.ResourceWithModifyPlan  = &BasicResource{}
)

func NewBasicResource() resource.Resource {
	return &BasicResource{discountResource[basicModel, *basicModel]{method: methodBasic, what: whatBasic}}
}

// BasicResource manages one amount-off discount on products or the order, as a code or an automatic discount.
type BasicResource struct {
	discountResource[basicModel, *basicModel]
}

type basicModel struct {
	commonModel
	CustomerGets       types.Object `tfsdk:"customer_gets"`
	MinimumRequirement types.Object `tfsdk:"minimum_requirement"`
}

func (m *basicModel) common() *commonModel { return &m.commonModel }

func (r *BasicResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_discount_basic"
	// Read re-adopts a discount recreated outside Terraform by its code or title, which yields a new GID.
	resp.ResourceBehavior.MutableIdentity = true
}

func (r *BasicResource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = patterns.IDIdentitySchema()
}

func (r *BasicResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := commonAttributes()
	attrs["customer_gets"] = customerGetsAttribute()
	attrs["minimum_requirement"] = minimumAttribute()
	resp.Schema = schema.Schema{
		MarkdownDescription: "An amount-off discount: a percentage or a fixed amount off selected products, collections or the whole order, " +
			"as a code discount (`code` set) or an automatic one (`code` unset). " +
			"**Volatile, usually a poor fit for Terraform:** promotions are created, changed and ended in the admin by marketing, and every admin edit to a managed attribute is reverted on the next apply. " +
			"Long-running rules, such as a newsletter code, fit Terraform; attributes you leave unset are not managed. " +
			"Destroying a discount deletes it; orders keep their discount applications, but a recreated discount counts its uses from zero. " +
			"Bulk codes, tags, subscription settings and app discounts are not managed. " +
			"Generated config holds product, collection, segment and market GIDs, which tie it to the store: replace them with references. " +
			"An existing discount with the same code, or an automatic one with the same title, needs `adopt_existing = true`; destroy then leaves it in place. " +
			"Import with `code/<code>` or `automatic/<title>`.",
		Attributes: attrs,
	}
}

// write: see kindModel.
func (cfg *basicModel) write(ctx context.Context, c *client.Client, plan *basicModel, live *node, diags *diag.Diagnostics) string {
	cfg.takeDefaults(&plan.commonModel)
	cfg.CustomerGets = plan.CustomerGets
	isCode := !cfg.Code.IsNull()
	summary := writeSummary(whatBasic, live)
	if live != nil {
		isCode = live.IsCode
	}
	var id *string
	var errs []client.DiscountUserErrorFields
	var err error
	if isCode {
		in, d := cfg.codeInput(ctx, live)
		if diags.Append(d...); diags.HasError() {
			return ""
		}
		if live == nil {
			var res *client.CreateDiscountCodeBasicResponse
			if res, err = client.CreateDiscountCodeBasic(ctx, c, in); err == nil && res.DiscountCodeBasicCreate != nil {
				errs = res.DiscountCodeBasicCreate.UserErrors
				if n := res.DiscountCodeBasicCreate.CodeDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountCodeBasicResponse
			if res, err = client.UpdateDiscountCodeBasic(ctx, c, live.ID, in); err == nil && res.DiscountCodeBasicUpdate != nil {
				errs = res.DiscountCodeBasicUpdate.UserErrors
				if n := res.DiscountCodeBasicUpdate.CodeDiscountNode; n != nil {
					id = &n.Id
				}
			}
		}
	} else {
		in, d := cfg.automaticInput(ctx, live)
		if diags.Append(d...); diags.HasError() {
			return ""
		}
		if live == nil {
			var res *client.CreateDiscountAutomaticBasicResponse
			if res, err = client.CreateDiscountAutomaticBasic(ctx, c, in); err == nil && res.DiscountAutomaticBasicCreate != nil {
				errs = res.DiscountAutomaticBasicCreate.UserErrors
				if n := res.DiscountAutomaticBasicCreate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		} else {
			var res *client.UpdateDiscountAutomaticBasicResponse
			if res, err = client.UpdateDiscountAutomaticBasic(ctx, c, live.ID, in); err == nil && res.DiscountAutomaticBasicUpdate != nil {
				errs = res.DiscountAutomaticBasicUpdate.UserErrors
				if n := res.DiscountAutomaticBasicUpdate.AutomaticDiscountNode; n != nil {
					id = &n.Id
				}
			}
		}
	}
	out := writeOutcome(diags, summary, err, errs, id)
	if out == "" && err == nil {
		var cg customerGetsSel
		if d := plan.CustomerGets.As(ctx, &cg, basetypes.ObjectAsOptions{}); !d.HasError() && cg.Items.empty() {
			diags.AddAttributeError(path.Root("customer_gets").AtName("items"), summary, emptySelectionHint)
		}
	}
	return out
}

// inputs maps the configured attributes against live (nil on create).
func (m *basicModel) inputs(ctx context.Context, live *node) (commonInput, *client.DiscountCustomerGetsInput, *client.DiscountMinimumRequirementInput, diag.Diagnostics) {
	common, diags := m.input(ctx, live)
	var cg customerGetsSel
	diags.Append(m.CustomerGets.As(ctx, &cg, basetypes.ObjectAsOptions{})...)
	var liveGets *client.DiscountCustomerGetsFields
	if live != nil {
		liveGets = live.CustomerGets
	}
	gets, err := customerGetsInput(cg, liveGets)
	if err != nil {
		diags.AddAttributeError(path.Root("customer_gets").AtName("items"), "Items can't be managed", fmt.Sprintf("The discount %s.", err))
	}
	var minimum *client.DiscountMinimumRequirementInput
	if !m.MinimumRequirement.IsNull() && !m.MinimumRequirement.IsUnknown() {
		var ms minimumSel
		diags.Append(m.MinimumRequirement.As(ctx, &ms, basetypes.ObjectAsOptions{})...)
		minimum = minimumInput(ms)
	}
	return common, gets, minimum, diags
}

func (m *basicModel) codeInput(ctx context.Context, live *node) (client.DiscountCodeBasicInput, diag.Diagnostics) {
	c, gets, minimum, diags := m.inputs(ctx, live)
	return client.DiscountCodeBasicInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt, Code: c.Code, UsageLimit: c.UsageLimit, AppliesOncePerCustomer: c.AppliesOncePerCustomer,
		CombinesWith: c.CombinesWith, Context: c.Context, CustomerGets: gets, MinimumRequirement: minimum,
	}, diags
}

func (m *basicModel) automaticInput(ctx context.Context, live *node) (client.DiscountAutomaticBasicInput, diag.Diagnostics) {
	c, gets, minimum, diags := m.inputs(ctx, live)
	return client.DiscountAutomaticBasicInput{
		Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt,
		CombinesWith: c.CombinesWith, Context: c.Context, CustomerGets: gets, MinimumRequirement: minimum,
	}, diags
}

// setFromAPI copies n into m; shapes the schema can't hold are errors, so import fails and list skips them.
func (m *basicModel) setFromAPI(ctx context.Context, n *node) (string, diag.Diagnostics) {
	unsupported, diags := m.commonModel.setFromAPI(ctx, n)
	m.CustomerGets, m.MinimumRequirement = types.ObjectNull(customerGetsAttrTypes()), types.ObjectNull(minimumAttrTypes())
	if n.CustomerGets == nil {
		diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q has no customer gets.", n.Title))
		return unsupported, diags
	}
	value, err1 := valueFromAPI(n.CustomerGets.Value)
	items, err2 := itemsFromAPI(n.CustomerGets.Items)
	minimum, err3 := minimumFromAPI(n.Minimum)
	for _, err := range []error{err1, err2, err3} {
		if err != nil {
			diags.AddError("Unsupported discount", fmt.Sprintf("Discount %q %s.", n.Title, err))
		}
	}
	if diags.HasError() {
		return unsupported, diags
	}
	var d diag.Diagnostics
	m.CustomerGets, d = types.ObjectValueFrom(ctx, customerGetsAttrTypes(), customerGetsSel{Value: value, Items: items})
	diags.Append(d...)
	m.MinimumRequirement, d = types.ObjectValueFrom(ctx, minimumAttrTypes(), minimum)
	diags.Append(d...)
	return unsupported, diags
}
