package webpresence

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/pflege-de-labs/terraform-provider-shopify/internal/client"
)

// key identifies a presence by its natural key: the suffix, or the domain (by host on import, by GID in config).
type key struct {
	suffix   string
	domainID string
	host     string
}

// parseImportKey tells a domain host from a suffix: hosts contain dots, suffixes are letters only.
func parseImportKey(s string) key {
	if strings.Contains(s, ".") {
		return key{host: s}
	}
	return key{suffix: s}
}

func (k key) matches(p *client.WebPresenceFields) bool {
	switch {
	case k.suffix != "":
		return p.SubfolderSuffix != nil && *p.SubfolderSuffix == k.suffix
	case k.domainID != "":
		return p.Domain != nil && p.Domain.Id == k.domainID
	case k.host != "":
		return p.Domain != nil && p.Domain.Host == k.host
	}
	return false
}

func checkTarget(suffixSet, domainSet bool) error {
	if suffixSet == domainSet {
		return errors.New("set exactly one of subfolder_suffix and domain_id")
	}
	return nil
}

// alternateLocalesJSON is nil when unmanaged, so the field is omitted; an empty managed set sends [] to clear.
func alternateLocalesJSON(locales []string, managed bool) json.RawMessage {
	if !managed {
		return nil
	}
	sorted := slices.Sorted(slices.Values(locales))
	b, _ := json.Marshal(append([]string{}, sorted...))
	return b
}

// updateInput sends the suffix only when it changes: a null or repeated one is rejected for domain and primary presences.
func updateInput(defaultLocale string, planSuffix, stateSuffix *string, alternates []string, managed bool) client.WebPresenceUpdateInput {
	in := client.WebPresenceUpdateInput{
		DefaultLocale:    &defaultLocale,
		AlternateLocales: alternateLocalesJSON(alternates, managed),
	}
	if planSuffix != nil && (stateSuffix == nil || *planSuffix != *stateSuffix) {
		in.SubfolderSuffix = planSuffix
	}
	return in
}
