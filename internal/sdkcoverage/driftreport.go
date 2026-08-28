package sdkcoverage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// jsonDriftReport is the machine-readable drift document. It is a separate
// document from jsonReport on purpose: the coverage report's field names are a
// stable contract with the sdk-watch workflow, and drift gets its own contract
// rather than a risky extension of that one. Keep these field names stable too —
// the workflow reads counts.breaking and counts.informational.
type jsonDriftReport struct {
	SDKModule string `json:"sdk_module"`

	// LockSDKVersion labels the tree the lock was generated from, SDKVersion the
	// tree being compared. Both informational; the diff runs on shapes.
	LockSDKVersion string `json:"lock_sdk_version,omitempty"`
	SDKVersion     string `json:"sdk_version,omitempty"`

	// LockMissing means there was no lock to compare against, so the document is
	// an inert no-op rather than a claim that nothing drifted.
	LockMissing bool `json:"lock_missing"`

	// OK means no breaking drift.
	OK bool `json:"ok"`

	Counts jsonDriftCounts `json:"counts"`
	Drift  []jsonDrift     `json:"drift"`
}

type jsonDriftCounts struct {
	Total         int `json:"total"`
	Breaking      int `json:"breaking"`
	Informational int `json:"informational"`
}

type jsonDrift struct {
	Group              string   `json:"group"`
	Kind               string   `json:"kind"`
	Breaking           bool     `json:"breaking"`
	Model              string   `json:"model,omitempty"`
	Field              string   `json:"field,omitempty"`
	Detail             string   `json:"detail"`
	SuggestedAttribute string   `json:"suggested_attribute,omitempty"`
	ImplementedBy      []string `json:"implemented_by"`
}

// DriftJSON renders a drift set as an indented JSON document. lockVersion and
// sdkVersion label the two sides; lockMissing renders the inert no-op form.
func DriftJSON(drift []Drift, lockVersion, sdkVersion string, lockMissing bool) ([]byte, error) {
	doc := jsonDriftReport{
		SDKModule:      SDKModulePath,
		LockSDKVersion: lockVersion,
		SDKVersion:     sdkVersion,
		LockMissing:    lockMissing,
		OK:             countBreaking(drift) == 0,
		Counts: jsonDriftCounts{
			Total:         len(drift),
			Breaking:      countBreaking(drift),
			Informational: len(drift) - countBreaking(drift),
		},
		Drift: make([]jsonDrift, 0, len(drift)),
	}
	for _, d := range drift {
		implementedBy := d.ImplementedBy
		if implementedBy == nil {
			// Arrays are never null, same convention as the coverage document.
			implementedBy = []string{}
		}
		doc.Drift = append(doc.Drift, jsonDrift{
			Group:              d.Group,
			Kind:               string(d.Kind),
			Breaking:           d.Breaking(),
			Model:              d.Model,
			Field:              d.Field,
			Detail:             d.Detail,
			SuggestedAttribute: d.SuggestedAttribute,
			ImplementedBy:      implementedBy,
		})
	}
	return json.MarshalIndent(doc, "", "  ")
}

// DriftMarkdown renders a drift set for a tracking issue or PR body: breaking
// drift first under a warning, additions and notes after.
func DriftMarkdown(drift []Drift, lockVersion, sdkVersion string) string {
	var b strings.Builder

	b.WriteString("# Field drift on covered groups\n\n")
	if lockVersion != "" || sdkVersion != "" {
		fmt.Fprintf(&b, "Lock: `%s` — SDK: `%s`\n\n", orDash(lockVersion), orDash(sdkVersion))
	}

	if len(drift) == 0 {
		b.WriteString("No drift: the SDK still has the shape the lock acknowledges.\n")
		return b.String()
	}

	var breaking, informational []Drift
	for _, d := range drift {
		if d.Breaking() {
			breaking = append(breaking, d)
		} else {
			informational = append(informational, d)
		}
	}

	if len(breaking) > 0 {
		b.WriteString("> [!WARNING]\n")
		fmt.Fprintf(&b, "> %d breaking change(s) on covered groups. The provider still compiles in the old shape; "+
			"triage these before (or in) the PR that bumps the SDK.\n", len(breaking))
		b.WriteString("\n## Breaking\n\n")
		writeDriftTable(&b, breaking)
	}

	if len(informational) > 0 {
		b.WriteString("\n## New capability and notes\n\n")
		writeDriftTable(&b, informational)
	}

	return b.String()
}

func writeDriftTable(b *strings.Builder, drift []Drift) {
	b.WriteString("| Group | Kind | Where | Detail | Suggested attribute | Implemented by |\n|---|---|---|---|---|---|\n")
	for _, d := range drift {
		where := d.Model
		if d.Field != "" {
			if where != "" {
				where += "."
			}
			where += d.Field
		}
		fmt.Fprintf(b, "| `%s` | `%s` | %s | %s | %s | %s |\n",
			d.Group, d.Kind, orDash(codeOrEmpty(where)), orDash(d.Detail),
			orDash(codeOrEmpty(d.SuggestedAttribute)), codeList(d.ImplementedBy))
	}
}

// DriftText renders a short human-readable drift summary for terminal use.
func DriftText(drift []Drift, lockVersion, sdkVersion string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "lock %s vs SDK %s\n", orDash(lockVersion), orDash(sdkVersion))
	breaking := countBreaking(drift)
	fmt.Fprintf(&b, "%d drift row(s): %d breaking, %d informational\n", len(drift), breaking, len(drift)-breaking)

	for _, d := range drift {
		marker := " "
		if d.Breaking() {
			marker = "!"
		}
		fmt.Fprintf(&b, "  %s %s\n", marker, d)
	}
	return b.String()
}

func countBreaking(drift []Drift) int {
	n := 0
	for _, d := range drift {
		if d.Breaking() {
			n++
		}
	}
	return n
}

func codeOrEmpty(s string) string {
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}
