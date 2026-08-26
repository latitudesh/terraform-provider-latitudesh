package sdkcoverage

import "encoding/json"

// jsonReport is the machine-readable coverage document. It is the contract the
// sdk-watch workflow reads: `jq '.pending | length'` gates whether there is
// anything to scaffold, and each pending entry carries the suggested Terraform
// type name so the tracking issue and the scaffold job need no naming logic of
// their own. Keep the field names stable — automation depends on them.
type jsonReport struct {
	SDKModule  string          `json:"sdk_module"`
	SDKVersion string          `json:"sdk_version,omitempty"`
	OK         bool            `json:"ok"`
	Counts     jsonCounts      `json:"counts"`
	Violations []jsonViolation `json:"violations"`

	// Pending is the generation queue: groups with no manifest entry whose shape
	// supports scaffolding. This is the array automation acts on.
	Pending []jsonGroup `json:"pending"`

	// Unshaped and Revisit are informational — surfaced so the tracking issue can
	// report them, never acted on automatically.
	Unshaped []jsonGroup `json:"unshaped"`
	Revisit  []jsonGroup `json:"revisit"`
}

type jsonCounts struct {
	Total      int `json:"total"`
	Covered    int `json:"covered"`
	Pending    int `json:"pending"`
	Excluded   int `json:"excluded"`
	Unshaped   int `json:"unshaped"`
	Revisit    int `json:"revisit"`
	Violations int `json:"violations"`
}

type jsonViolation struct {
	// Group is empty for violations that concern a Terraform type with no owning
	// group.
	Group   string `json:"group,omitempty"`
	Message string `json:"message"`
}

type jsonGroup struct {
	Group             string   `json:"group"`
	CRUD              string   `json:"crud"`
	Generates         []string `json:"generates,omitempty"`
	Methods           []string `json:"methods"`
	SuggestedTypeName string   `json:"suggested_type_name"`
	// Ceiling is set only on Revisit rows, where the recorded cap is what the SDK
	// has now outgrown.
	Ceiling string `json:"ceiling,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// JSON renders the report as an indented JSON document. providerTypeName is the
// prefix every suggested type name carries.
func (r Report) JSON(sdkVersion, providerTypeName string) ([]byte, error) {
	doc := jsonReport{
		SDKModule:  SDKModulePath,
		SDKVersion: sdkVersion,
		OK:         r.OK(),
		Counts: jsonCounts{
			Total:      r.Total(),
			Covered:    len(r.Covered),
			Pending:    len(r.Pending),
			Excluded:   len(r.Excluded),
			Unshaped:   len(r.Unshaped),
			Revisit:    len(r.Revisit),
			Violations: len(r.Violations),
		},
		Violations: toJSONViolations(r.Violations),
		Pending:    toJSONGroups(r.Pending, providerTypeName),
		Unshaped:   toJSONGroups(r.Unshaped, providerTypeName),
		Revisit:    toJSONGroups(r.Revisit, providerTypeName),
	}
	return json.MarshalIndent(doc, "", "  ")
}

func toJSONViolations(violations []Violation) []jsonViolation {
	// A non-nil empty slice so the JSON is a stable [] rather than null, which
	// keeps jq filters simple on the consuming side.
	out := make([]jsonViolation, 0, len(violations))
	for _, v := range violations {
		out = append(out, jsonViolation{Group: v.Group, Message: v.Message})
	}
	return out
}

func toJSONGroups(rows []GroupReport, providerTypeName string) []jsonGroup {
	out := make([]jsonGroup, 0, len(rows))
	for _, g := range rows {
		// A methodless group has nil Methods; normalize so "arrays are never
		// null" holds for every array in the document, not just the top-level ones.
		methods := g.Methods
		if methods == nil {
			methods = []string{}
		}
		out = append(out, jsonGroup{
			Group:             g.Name,
			CRUD:              g.Capabilities.Summary(),
			Generates:         g.Generates(),
			Methods:           methods,
			SuggestedTypeName: SuggestTypeName(g.Name, providerTypeName),
			Ceiling:           string(g.Ceiling),
			Notes:             g.Notes,
		})
	}
	return out
}
