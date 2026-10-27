package checkoutandaccountsconfiguration

import "github.com/pflege-de-labs/terraform-provider-shopify/internal/patterns"

// brandingEquality compares with brandingEqual, so the API's casing and number format never show as drift.
type brandingEquality struct{}

func (brandingEquality) Equal(a, b string) bool { return brandingEqual(a, b) }

type (
	brandingType  = patterns.JSONType[brandingEquality]
	brandingValue = patterns.JSONValue[brandingEquality]
)

var (
	brandingString = patterns.JSONString[brandingEquality]
	brandingNull   = patterns.JSONNull[brandingEquality]
)
