package sdkcoverage

import "strings"

// SuggestTypeName proposes the Terraform type name for an SDK service group, e.g.
// "APIKeys" -> "latitudesh_api_key". It is a starting point for the scaffolding
// agent and the tracking issue, never the final word: the mapping is genuinely
// many-to-many (PrivateNetworks backs two types, TeamMembers ships as
// latitudesh_member), so a human editing the generated PR is where the real name
// gets decided.
//
// The rule is mechanical and predictable: split the dotted path into segments,
// split each segment into words (aware of acronym runs like API/IP/SSH),
// singularize the last word of each segment, lowercase everything and join with
// underscores, then prefix the provider name.
func SuggestTypeName(groupName, providerTypeName string) string {
	var words []string
	for _, segment := range strings.Split(groupName, ".") {
		segWords := splitWords(segment)
		if len(segWords) > 0 {
			last := len(segWords) - 1
			segWords[last] = singularize(segWords[last])
		}
		words = append(words, segWords...)
	}

	for i, w := range words {
		words[i] = strings.ToLower(w)
	}

	snake := strings.Join(words, "_")
	if snake == "" {
		return providerTypeName
	}
	return providerTypeName + "_" + snake
}

// splitWords breaks a CamelCase/PascalCase identifier into words, keeping runs of
// uppercase letters together as a single acronym: "APIKeys" -> ["API", "Keys"],
// "IPAddresses" -> ["IP", "Addresses"], "VpnSessions" -> ["Vpn", "Sessions"].
func splitWords(s string) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}

	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		boundary := false
		switch {
		// A lowercase (or digit) followed by an uppercase starts a new word:
		// the "I" in "elasticIps".
		case !isUpper(prev) && isUpper(cur):
			boundary = true
		// The end of an acronym run: UPPER UPPER lower splits before the second
		// uppercase, so "APIKeys" breaks into "API" and "Keys" rather than
		// "APIK" and "eys".
		case isUpper(prev) && isUpper(cur) && next != 0 && isLower(next):
			boundary = true
		}

		if boundary {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	return append(words, string(runes[start:]))
}

// singularize turns the plural head noun of a group name into its singular form.
// It handles just the patterns the SDK's group names actually use — this is a
// naming hint, not a linguistics engine, so an odd result is corrected by the
// reviewer, not worked around here.
func singularize(word string) string {
	lower := strings.ToLower(word)

	switch {
	case len(lower) > 3 && strings.HasSuffix(lower, "ies"):
		// Policies -> Policy
		return word[:len(word)-3] + "y"
	case hasAnySuffix(lower, "sses", "shes", "ches", "xes", "zes", "ses"):
		// Addresses -> Address, Boxes -> Box
		return word[:len(word)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss"):
		// Keys -> Key, Firewalls -> Firewall; "ss" endings (none today) stay put.
		return word[:len(word)-1]
	default:
		// Data, Storage, Traffic, VM: no plural to strip.
		return word
	}
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
