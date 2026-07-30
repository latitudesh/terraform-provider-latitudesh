package sdkcoverage

import (
	"fmt"
	"strings"
)

// Markdown renders the report as a coverage table suitable for a tracking issue
// or a roadmap document.
func (r Report) Markdown(sdkVersion string) string {
	var b strings.Builder

	b.WriteString("# Terraform provider coverage of `latitudesh-go-sdk`\n\n")
	if sdkVersion != "" {
		fmt.Fprintf(&b, "SDK version: `%s`\n\n", sdkVersion)
	}
	fmt.Fprintf(&b, "%d of %d service groups covered — %d pending generation, %d excluded, %d need a human.\n",
		len(r.Covered), r.Total(), len(r.Pending), len(r.Excluded), len(r.Unshaped))

	b.WriteString("\nCRUD shapes are derived from the SDK's method names. The SDK is generated " +
		"from the API's OpenAPI spec, so a shape can understate what the API actually offers " +
		"when the spec is incomplete — treat it as a floor, not a ceiling.\n")

	if len(r.Violations) > 0 {
		fmt.Fprintf(&b, "\n## Violations (%d)\n\n", len(r.Violations))
		for _, v := range r.Violations {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}

	if len(r.Pending) > 0 {
		b.WriteString("\n## Pending generation\n\n")
		b.WriteString("No manifest entry, so scaffolding is expected. Reviewing the generated " +
			"PR is where the keep-or-drop decision gets made; dropping it means adding a " +
			"ceiling and rationale here.\n\n")
		b.WriteString("| Group | CRUD | Would generate | Notes |\n|---|---|---|---|\n")
		for _, g := range r.Pending {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n",
				g.Name, g.Capabilities.Summary(), strings.Join(g.Generates(), " + "), orDash(g.Notes))
		}
	}

	if len(r.Unshaped) > 0 {
		b.WriteString("\n## Too partial to generate\n\n")
		b.WriteString("Readable but not resource-shaped, or not readable at all — no coherent " +
			"resource or data source falls out of these. They need a human decision, not an agent.\n\n")
		b.WriteString("| Group | CRUD | Methods | Notes |\n|---|---|---|---|\n")
		for _, g := range r.Unshaped {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n",
				g.Name, g.Capabilities.Summary(), methodList(g.Methods), orDash(g.Notes))
		}
	}

	if len(r.Revisit) > 0 {
		b.WriteString("\n## Worth revisiting\n\n")
		b.WriteString("Excluded on API grounds, but the SDK has since grown past the recorded " +
			"ceiling. Not a failure — re-triage when convenient.\n\n")
		b.WriteString("| Group | Ceiling | CRUD now | Notes |\n|---|---|---|---|\n")
		for _, g := range r.Revisit {
			fmt.Fprintf(&b, "| `%s` | %s | `%s` | %s |\n",
				g.Name, g.Ceiling, g.Capabilities.Summary(), orDash(g.Notes))
		}
	}

	if len(r.Covered) > 0 {
		b.WriteString("\n## Covered\n\n")
		b.WriteString("| Group | CRUD | Implemented by |\n|---|---|---|\n")
		for _, g := range r.Covered {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n",
				g.Name, g.Capabilities.Summary(), codeList(g.ImplementedBy))
		}
	}

	if len(r.Excluded) > 0 {
		b.WriteString("\n## Excluded\n\n")
		b.WriteString("| Group | Ceiling | Rationale | Notes |\n|---|---|---|---|\n")
		for _, g := range r.Excluded {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
				g.Name, g.Ceiling, g.Rationale, orDash(g.Notes))
		}
	}

	if unclassified := r.Unclassified(); len(unclassified) > 0 {
		b.WriteString("\n## Unrecognized method names\n\n")
		b.WriteString("The SDK uses a naming style this classifier does not know. ")
		b.WriteString("Worth a look, but not a failure.\n\n")
		for _, g := range unclassified {
			fmt.Fprintf(&b, "- `%s`: %s\n", g.Name, methodList(g.Capabilities.Unclassified))
		}
	}

	return b.String()
}

// Text renders a short human-readable summary for terminal use.
func (r Report) Text(sdkVersion string) string {
	var b strings.Builder

	if sdkVersion != "" {
		fmt.Fprintf(&b, "SDK %s\n", sdkVersion)
	}
	fmt.Fprintf(&b, "%d/%d covered, %d pending generation, %d excluded, %d need a human\n",
		len(r.Covered), r.Total(), len(r.Pending), len(r.Excluded), len(r.Unshaped))

	section := func(title string, rows []GroupReport, detail func(GroupReport) string) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, g := range rows {
			fmt.Fprintf(&b, "  %-24s %s  %s\n", g.Name, g.Capabilities.Summary(), detail(g))
		}
	}

	section("Pending generation", r.Pending, func(g GroupReport) string {
		return strings.Join(g.Generates(), " + ")
	})
	section("Too partial to generate", r.Unshaped, func(g GroupReport) string {
		return g.Capabilities.ShapeHint()
	})
	section("Excluded", r.Excluded, func(g GroupReport) string {
		return string(g.Ceiling) + " / " + string(g.Rationale)
	})
	section("Worth revisiting", r.Revisit, func(g GroupReport) string {
		return "recorded ceiling " + string(g.Ceiling) + ", SDK now offers more"
	})

	if len(r.Violations) > 0 {
		fmt.Fprintf(&b, "\n%d violation(s):\n", len(r.Violations))
		for _, v := range r.Violations {
			fmt.Fprintf(&b, "  %s\n", v)
		}
	}

	return b.String()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func codeList(items []string) string {
	if len(items) == 0 {
		return "—"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = "`" + item + "`"
	}
	return strings.Join(quoted, ", ")
}
