package sdkcoverage

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Violation is a disagreement between the SDK surface, the manifest, and what the
// provider actually ships. Every violation is actionable by a human editing the
// manifest or the provider.
//
// Violations are limited to contradictions. Two things deliberately are not
// violations:
//
//   - A group with no entry. That is the normal trigger for generating one, not a
//     blind spot, so an SDK bump does not go red over it.
//   - An API that grew past a recorded ceiling. That is an opportunity, and the
//     person bumping the SDK is not the person who decides product scope. It
//     surfaces as Revisit instead.
type Violation struct {
	// Group is the SDK group the violation concerns, or "" when it concerns a
	// Terraform type with no owning group.
	Group   string
	Message string
}

func (v Violation) String() string {
	if v.Group == "" {
		return v.Message
	}
	return v.Group + ": " + v.Message
}

// GroupReport is one row of the coverage report: the SDK facts, the manifest's
// intent, and the capabilities derived from the methods.
type GroupReport struct {
	Group
	Entry
	Capabilities Capabilities
}

// Generates reports what scaffolding this group should produce, given its derived
// shape and any ceiling. Returns nil when nothing should be generated.
func (g GroupReport) Generates() []string {
	if g.Covered() {
		return nil
	}

	var kinds []string
	switch g.Ceiling {
	case CeilingNone:
		return nil
	case CeilingDataSource:
		if g.Capabilities.Readable {
			kinds = append(kinds, "datasource")
		}
	default:
		if g.Capabilities.ResourceShaped() {
			kinds = append(kinds, "resource")
		}
		if g.Capabilities.Readable {
			kinds = append(kinds, "datasource")
		}
	}
	return kinds
}

// Report is the full reconciliation result.
type Report struct {
	Violations []Violation

	Covered  []GroupReport
	Excluded []GroupReport

	// Pending are groups with no entry whose shape supports scaffolding. This is
	// the generation queue, and it needs no bookkeeping to stay current.
	Pending []GroupReport

	// Unshaped are groups with no entry whose lifecycle is too partial to generate
	// anything coherent from. They need a human, not an agent.
	Unshaped []GroupReport

	// Revisit holds groups excluded on API grounds whose constraint no longer
	// holds, because the SDK has since grown past the recorded ceiling.
	// Informational: a prompt to re-triage, never a failure.
	Revisit []GroupReport
}

// OK reports whether the SDK, the manifest, and the provider all agree.
func (r Report) OK() bool { return len(r.Violations) == 0 }

func (r Report) all() []GroupReport {
	var out []GroupReport
	for _, bucket := range [][]GroupReport{r.Covered, r.Excluded, r.Pending, r.Unshaped} {
		out = append(out, bucket...)
	}
	return out
}

// Total is the number of SDK groups accounted for.
func (r Report) Total() int { return len(r.all()) }

// Unclassified returns groups with methods the prefix classifier did not
// recognize. Reported but never a violation: an unfamiliar naming style is a
// signal for a human, not a reason to fail an SDK bump.
func (r Report) Unclassified() []GroupReport {
	var out []GroupReport
	for _, g := range r.all() {
		if len(g.Capabilities.Unclassified) > 0 {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Reconcile cross-checks the SDK surface, the manifest, and the Terraform type
// names the provider registers.
//
// shipped is passed in as plain strings rather than read from the provider here,
// so this package never imports the provider package — the gate test lives inside
// that package and would otherwise be an import cycle.
func Reconcile(surface Surface, manifest Manifest, shipped []string) Report {
	var report Report

	shippedSet := make(map[string]bool, len(shipped))
	for _, name := range shipped {
		shippedSet[name] = true
	}
	claimed := make(map[string]bool, len(shipped))

	for _, name := range surface.Names() {
		group := surface.Groups[name]
		entry := manifest.Groups[name] // zero value is a valid "no entry"
		row := GroupReport{Group: group, Entry: entry, Capabilities: Classify(group.Methods)}

		if entry.Covered() {
			report.Violations = append(report.Violations, validateCovered(name, entry)...)
			for _, tfType := range entry.ImplementedBy {
				claimed[tfType] = true
				if !shippedSet[tfType] {
					report.Violations = append(report.Violations, Violation{
						Group: name,
						Message: fmt.Sprintf(
							"implemented_by names %q, which the provider does not register, so this group's coverage is a fiction — "+
								"update the name here if the resource was renamed, or drop it from the list if it was removed", tfType),
					})
				}
			}
			report.Covered = append(report.Covered, row)
			continue
		}

		report.Violations = append(report.Violations, validateUncovered(name, entry)...)

		switch {
		case entry.Excluded():
			report.Excluded = append(report.Excluded, row)
			// An API-grounds exclusion is only true while the API stays put.
			if entry.Rationale == RationaleAPIConstraint && outgrewCeiling(entry.Ceiling, row.Capabilities) {
				report.Revisit = append(report.Revisit, row)
			}
		case len(row.Generates()) > 0:
			report.Pending = append(report.Pending, row)
		default:
			report.Unshaped = append(report.Unshaped, row)
		}
	}

	// Manifest entries for groups the SDK no longer exposes: a rename or removal
	// on an SDK bump that would otherwise pass unnoticed.
	for name := range manifest.Groups {
		if _, ok := surface.Groups[name]; ok {
			continue
		}
		report.Violations = append(report.Violations, Violation{
			Group: name,
			Message: fmt.Sprintf(
				"listed in %s but the SDK no longer exposes it — rename this entry to match if the group was renamed "+
					"upstream, or delete it if the group is gone (`sdkcoverage groups` lists the current names)", ManifestFile),
		})
	}

	// Terraform types no group claims. With undeclared groups no longer a
	// violation, this check carries real weight: an unclaimed type means its SDK
	// group still reads as ungenerated, so the scaffolding agent would write a
	// second resource competing with the one already there.
	for _, tfType := range shipped {
		if !claimed[tfType] {
			report.Violations = append(report.Violations, Violation{
				Message: fmt.Sprintf(
					"the provider registers %q but no group claims it, so that group still counts as ungenerated and "+
						"would be scaffolded again — add %q to implemented_by on the SDK group its resource calls "+
						"(`sdkcoverage groups` lists them)", tfType, tfType),
			})
		}
	}

	sort.Slice(report.Violations, func(i, j int) bool {
		if report.Violations[i].Group != report.Violations[j].Group {
			return report.Violations[i].Group < report.Violations[j].Group
		}
		return report.Violations[i].Message < report.Violations[j].Message
	})

	return report
}

// validateCovered checks an implemented group. A ceiling caps what to generate and a
// rationale explains why nothing was generated, so neither belongs on a group that
// is already built — what got built is the answer.
func validateCovered(name string, entry Entry) []Violation {
	if !entry.Excluded() {
		return nil
	}
	return []Violation{{
		Group:   name,
		Message: "implemented_by is set, so ceiling and rationale do not apply — drop them",
	}}
}

// validateUncovered checks a group nothing was built for. Having no entry at all is
// fine and means "generate it"; what cannot stand is a half-written exclusion.
func validateUncovered(name string, entry Entry) []Violation {
	var violations []Violation

	switch {
	case entry.Ceiling == "" && entry.Rationale == "":
		return nil // no entry, or notes only: generate it
	case entry.Ceiling == "":
		violations = append(violations, Violation{
			Group: name,
			Message: fmt.Sprintf(
				"rationale %q needs a ceiling to say how much to stop generating (%s)",
				entry.Rationale, ceilingList()),
		})
	case entry.Rationale == "":
		violations = append(violations, Violation{
			Group: name,
			Message: fmt.Sprintf(
				"ceiling %q needs a rationale (%s)", entry.Ceiling, rationaleList()),
		})
	}

	if entry.Ceiling != "" && !validCeiling(entry.Ceiling) {
		violations = append(violations, Violation{
			Group:   name,
			Message: fmt.Sprintf("unknown ceiling %q (want %s)", entry.Ceiling, ceilingList()),
		})
	}
	if entry.Rationale != "" && !validRationale(entry.Rationale) {
		violations = append(violations, Violation{
			Group:   name,
			Message: fmt.Sprintf("unknown rationale %q (want %s)", entry.Rationale, rationaleList()),
		})
	}

	return violations
}

// outgrewCeiling reports whether the SDK now offers more than the recorded ceiling
// assumed, which means an api_constraint exclusion is stale.
func outgrewCeiling(ceiling Ceiling, caps Capabilities) bool {
	switch ceiling {
	case CeilingNone:
		// Anything readable is at least data-source material.
		return caps.Readable
	case CeilingDataSource:
		return caps.ResourceShaped()
	default:
		return false
	}
}

func validCeiling(c Ceiling) bool {
	for _, known := range knownCeilings() {
		if c == known {
			return true
		}
	}
	return false
}

func validRationale(r Rationale) bool {
	for _, known := range knownRationales() {
		if r == known {
			return true
		}
	}
	return false
}

func ceilingList() string {
	names := make([]string, 0, len(knownCeilings()))
	for _, c := range knownCeilings() {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}

func rationaleList() string {
	names := make([]string, 0, len(knownRationales()))
	for _, r := range knownRationales() {
		names = append(names, string(r))
	}
	return strings.Join(names, ", ")
}

func methodList(methods []string) string {
	if len(methods) == 0 {
		return "none"
	}
	return strings.Join(methods, ", ")
}

// PinnedModuleDir resolves the local source directory of a module already
// required by this repo, so the SDK surface can be parsed straight from the
// module cache with no network access and no vendoring.
func PinnedModuleDir(modulePath string) (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modulePath).Output()
	if err != nil {
		return "", fmt.Errorf("sdkcoverage: resolving %s: %w", modulePath, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("sdkcoverage: %s has no local directory (run `go mod download`)", modulePath)
	}
	return dir, nil
}
