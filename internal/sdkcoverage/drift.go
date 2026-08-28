package sdkcoverage

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// DriftKind classifies one field-level difference between the lock and an SDK
// tree.
type DriftKind string

const (
	// Breaking kinds: the SDK no longer has a shape the provider's compiled-in
	// mappings could still assume, so silently absorbing it risks a runtime
	// break. These fail the gate when found between the lock and the PINNED SDK.

	DriftFieldRemoved           DriftKind = "field_removed"
	DriftFieldTypeChanged       DriftKind = "field_type_changed"
	DriftFieldRequiredChanged   DriftKind = "field_required_changed"
	DriftEnumValueRemoved       DriftKind = "enum_value_removed"
	DriftMethodRemoved          DriftKind = "method_removed"
	DriftMethodSignatureChanged DriftKind = "method_signature_changed"

	// Informational kinds: new capability or behavior notes on a covered group.
	// Reported and queued for a fix, never a failure — the same stance the
	// group-level report takes on pending groups.

	DriftFieldAdded     DriftKind = "field_added"
	DriftFieldRenamed   DriftKind = "field_renamed"
	DriftEnumValueAdded DriftKind = "enum_value_added"
	DriftMethodAdded    DriftKind = "method_added"
	DriftModelAdded     DriftKind = "model_added"
	DriftModelRemoved   DriftKind = "model_removed"
	DriftDeprecated     DriftKind = "deprecated"
	DriftDefaultChanged DriftKind = "default_changed"
	DriftDocChanged     DriftKind = "doc_changed"
	DriftGroupUnlocked  DriftKind = "group_unlocked"
	DriftGroupStale     DriftKind = "group_stale"
)

// Breaking reports whether this kind fails the gate. A removed model is not
// here on purpose: removing a model only matters through the field or
// signature that referenced it, and that reference drifts as its own breaking
// row.
func (k DriftKind) Breaking() bool {
	switch k {
	case DriftFieldRemoved, DriftFieldTypeChanged, DriftFieldRequiredChanged,
		DriftEnumValueRemoved, DriftMethodRemoved, DriftMethodSignatureChanged:
		return true
	}
	return false
}

// Drift is one difference between the acknowledged lock and an SDK tree,
// attributed to the covered group it appeared under.
type Drift struct {
	// Group is the dotted group path the drift belongs to. A model shared by
	// several covered groups drifts once per group: each mapping owner needs to
	// see it.
	Group string

	Kind DriftKind

	// Model is the qualified model name, e.g. "components.ServerData". Empty for
	// method- and group-level drift.
	Model string

	// Field names what moved inside the model: a field's wire name, an enum
	// value, or a method name. Empty for model- and group-level drift.
	Field string

	// Detail is the human-readable old→new explanation.
	Detail string

	// SuggestedAttribute is the Terraform attribute name a reviewer would expect
	// for an added field. Advisory only, like SuggestTypeName: the real mapping
	// is many-to-many and gets decided in review.
	SuggestedAttribute string

	// ImplementedBy names the Terraform types that own this group's mapping —
	// who has to look.
	ImplementedBy []string
}

// Breaking reports whether this drift fails the gate.
func (d Drift) Breaking() bool { return d.Kind.Breaking() }

func (d Drift) String() string {
	var b strings.Builder
	b.WriteString(d.Group)
	if d.Model != "" {
		b.WriteString(" " + d.Model)
	}
	if d.Field != "" {
		b.WriteString(" " + d.Field)
	}
	fmt.Fprintf(&b, ": %s", d.Kind)
	if d.Detail != "" {
		b.WriteString(" — " + d.Detail)
	}
	return b.String()
}

// DiffFieldSurfaces compares the acknowledged baseline (the lock) against a
// parsed surface. current is expected to hold exactly the covered groups;
// manifest supplies each group's ImplementedBy for attribution.
func DiffFieldSurfaces(baseline, current FieldSurface, manifest Manifest) []Drift {
	var drift []Drift

	for _, group := range sortedKeys(current.Groups) {
		implementedBy := manifest.Groups[group].ImplementedBy

		base, locked := baseline.Groups[group]
		if !locked {
			drift = append(drift, Drift{
				Group:         group,
				Kind:          DriftGroupUnlocked,
				Detail:        "covered but absent from the lock — run `make fields-sync` to seed it",
				ImplementedBy: implementedBy,
			})
			continue
		}
		drift = append(drift, diffGroup(group, base, current.Groups[group], implementedBy)...)
	}

	// Locked groups the parsed surface no longer carries: the group left
	// coverage, or left the SDK entirely. The group-level gate owns the latter;
	// either way the stale entry just wants a resync.
	for _, group := range sortedKeys(baseline.Groups) {
		if _, ok := current.Groups[group]; !ok {
			drift = append(drift, Drift{
				Group:  group,
				Kind:   DriftGroupStale,
				Detail: "locked but no longer a covered group — run `make fields-sync` to drop it",
			})
		}
	}

	sort.Slice(drift, func(i, j int) bool {
		a, b := drift[i], drift[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Kind < b.Kind
	})
	return drift
}

func diffGroup(group string, base, cur GroupModels, implementedBy []string) []Drift {
	var drift []Drift
	row := func(kind DriftKind, model, field, detail string) {
		drift = append(drift, Drift{
			Group: group, Kind: kind, Model: model, Field: field,
			Detail: detail, ImplementedBy: implementedBy,
		})
	}

	for _, name := range sortedKeys(base.Methods) {
		baseM := base.Methods[name]
		curM, ok := cur.Methods[name]
		if !ok {
			row(DriftMethodRemoved, "", name, "method gone; signature was "+baseM.Signature)
			continue
		}
		if baseM.Signature != curM.Signature {
			row(DriftMethodSignatureChanged, "", name, baseM.Signature+" -> "+curM.Signature)
		}
		if baseM.Deprecated != curM.Deprecated {
			row(DriftDeprecated, "", name, deprecationDetail(curM.Deprecated))
		}
	}
	for _, name := range sortedKeys(cur.Methods) {
		if _, ok := base.Methods[name]; !ok {
			row(DriftMethodAdded, "", name, "new method "+cur.Methods[name].Signature)
		}
	}

	var removed, added []string
	for _, name := range sortedKeys(base.Models) {
		if _, ok := cur.Models[name]; !ok {
			removed = append(removed, name)
		}
	}
	for _, name := range sortedKeys(cur.Models) {
		if _, ok := base.Models[name]; !ok {
			added = append(added, name)
		}
	}

	// A removed and an added model with the identical shape is almost certainly
	// a Speakeasy rename (operation renames cascade into their model names).
	// Say so, and never expand either side into a per-field cascade: one line
	// out, one line in.
	for _, name := range removed {
		detail := "model gone"
		for _, other := range added {
			if reflect.DeepEqual(base.Models[name], cur.Models[other]) {
				detail = "model gone — possibly renamed to " + other
				break
			}
		}
		row(DriftModelRemoved, name, "", detail)
	}
	for _, name := range added {
		row(DriftModelAdded, name, "", "new model: "+summarizeModel(cur.Models[name]))
	}

	for _, name := range sortedKeys(base.Models) {
		if _, ok := cur.Models[name]; ok {
			drift = append(drift, diffModel(group, name, base.Models[name], cur.Models[name], implementedBy)...)
		}
	}

	return drift
}

func diffModel(group, model string, base, cur ModelShape, implementedBy []string) []Drift {
	var drift []Drift
	row := func(kind DriftKind, field, detail, suggested string) {
		drift = append(drift, Drift{
			Group: group, Kind: kind, Model: model, Field: field,
			Detail: detail, SuggestedAttribute: suggested, ImplementedBy: implementedBy,
		})
	}

	baseEnum := stringSet(base.Enum)
	curEnum := stringSet(cur.Enum)
	for _, v := range base.Enum {
		if !curEnum[v] {
			row(DriftEnumValueRemoved, v, "enum value gone — anything still sending or unmarshaling it breaks", "")
		}
	}
	for _, v := range cur.Enum {
		if !baseEnum[v] {
			row(DriftEnumValueAdded, v, "new enum value", "")
		}
	}

	if base.Doc != cur.Doc {
		row(DriftDocChanged, "", "documentation changed — check for a behavior change the types cannot show", "")
	}

	baseFields := fieldsByIdentity(base.Fields)
	curFields := fieldsByIdentity(cur.Fields)

	for _, key := range sortedKeys(baseFields) {
		bf := baseFields[key]
		cf, ok := curFields[key]
		if !ok {
			row(DriftFieldRemoved, fieldLabel(bf), fmt.Sprintf("field gone; was %s", bf.Type), "")
			continue
		}
		if bf.Name != cf.Name {
			row(DriftFieldRenamed, fieldLabel(cf), fmt.Sprintf("Go name %s -> %s (wire name unchanged)", bf.Name, cf.Name), "")
		}
		// A field that only gained or lost its pointer alongside an optionality
		// flip is one fact, not two: the required↔optional row carries it.
		pointerFlipOnly := strings.TrimPrefix(bf.Type, "*") == strings.TrimPrefix(cf.Type, "*") &&
			bf.Optional != cf.Optional
		if bf.Type != cf.Type && !pointerFlipOnly {
			row(DriftFieldTypeChanged, fieldLabel(cf), fmt.Sprintf("type %s -> %s", bf.Type, cf.Type), "")
		}
		if bf.Optional != cf.Optional {
			row(DriftFieldRequiredChanged, fieldLabel(cf), requiredDetail(cf.Optional), "")
		}
		if bf.Default != cf.Default {
			row(DriftDefaultChanged, fieldLabel(cf), fmt.Sprintf("default %s -> %s", orNone(bf.Default), orNone(cf.Default)), "")
		}
		if bf.Deprecated != cf.Deprecated {
			row(DriftDeprecated, fieldLabel(cf), deprecationDetail(cf.Deprecated), "")
		}
		if bf.Doc != cf.Doc {
			row(DriftDocChanged, fieldLabel(cf), "documentation changed — check for a behavior change the types cannot show", "")
		}
	}
	for _, key := range sortedKeys(curFields) {
		if _, ok := baseFields[key]; !ok {
			cf := curFields[key]
			row(DriftFieldAdded, fieldLabel(cf), fmt.Sprintf("new field %s", cf.Type), SuggestAttributeName(cf))
		}
	}

	return drift
}

// fieldsByIdentity keys fields by what identifies them on the wire: the wire
// name when there is one, the Go name otherwise. A wire rename therefore reads
// as removed+added — which is what it is to the API — while a Go-only rename
// stays matched and reads as the cosmetic drift it is.
func fieldsByIdentity(fields []FieldShape) map[string]FieldShape {
	out := make(map[string]FieldShape, len(fields))
	for _, f := range fields {
		out[fieldLabel(f)] = f
	}
	return out
}

func fieldLabel(f FieldShape) string {
	if f.Wire != "" {
		return f.Wire
	}
	return f.Name
}

func summarizeModel(m ModelShape) string {
	switch {
	case len(m.Enum) > 0:
		return fmt.Sprintf("enum of %d value(s)", len(m.Enum))
	case len(m.Fields) > 0:
		return fmt.Sprintf("%d field(s)", len(m.Fields))
	default:
		return "empty struct"
	}
}

func deprecationDetail(deprecated bool) string {
	if deprecated {
		return "now marked Deprecated"
	}
	return "no longer marked Deprecated"
}

func requiredDetail(optional bool) string {
	if optional {
		return "required -> optional"
	}
	return "optional -> required"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}
