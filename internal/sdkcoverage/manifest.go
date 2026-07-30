package sdkcoverage

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SDKModulePath is the Go module this package reconciles against.
const SDKModulePath = "github.com/latitudesh/latitudesh-go-sdk"

// ManifestFile is the manifest's conventional name at the repository root.
const ManifestFile = "sdk-coverage.yaml"

// SupportedManifestVersion is the only manifest schema version this code
// understands. Bump it together with any breaking change to the layout below.
const SupportedManifestVersion = 1

// Ceiling caps how far Terraform goes for a service group, and so how much of it
// gets generated. It only appears on exclusions: with generation as the default,
// "generate everything the shape supports" is expressed by having no entry at all.
type Ceiling string

const (
	// CeilingNone excludes the group from Terraform entirely.
	CeilingNone Ceiling = "none"

	// CeilingDataSource allows a data source but not a resource — for groups whose
	// lifecycle is too incomplete to manage (no destroy, no read by id), or that
	// simply should not be managed.
	CeilingDataSource Ceiling = "datasource"
)

// Rationale explains why a group is excluded. It exists because the reason decides
// whether the exclusion should ever expire: an API limitation can disappear when
// the API grows, while a product decision does not change because an endpoint
// appeared.
type Rationale string

const (
	// RationaleAPIConstraint means the API does not offer enough to go further.
	// These are re-checked against the SDK surface on every run and reported once
	// the constraint stops holding — as information, never as a failure.
	RationaleAPIConstraint Rationale = "api_constraint"

	// RationaleProductDecision means we do not want this in Terraform, whatever
	// the API offers. Never auto-revisited.
	RationaleProductDecision Rationale = "product_decision"

	// RationaleDeprecated means the upstream endpoint is going away.
	RationaleDeprecated Rationale = "deprecated"
)

// Entry is the manifest record for one SDK service group.
//
// Coverage is not stated here: a group is covered exactly when ImplementedBy is
// non-empty, and that list is verified against the provider's own registration.
// A separate status field would be a second source of truth for the same fact, free
// to contradict the first.
//
// An entry is optional. A group with no entry is generated, which is why there is
// no "planned" or "undecided" state — nothing waits on a human writing a line.
type Entry struct {
	// ImplementedBy lists the Terraform type names backed by this group. Written by
	// the scaffolding agent when its PR merges, or by hand when a resource is
	// written the usual way. It is a list because the mapping is many-to-many:
	// PrivateNetworks backs both latitudesh_virtual_network and
	// latitudesh_vlan_assignment, while Tags is used by four provider files.
	ImplementedBy []string `yaml:"implemented_by,omitempty"`

	// Ceiling and Rationale record an exclusion. Both or neither: a ceiling without
	// a reason is unauditable, and a reason without a ceiling does not say how much
	// to stop generating.
	Ceiling   Ceiling   `yaml:"ceiling,omitempty"`
	Rationale Rationale `yaml:"rationale,omitempty"`

	// Notes is free-form context for whoever reviews this group next. Useful on its
	// own: an entry carrying only notes still generates, it just arrives with the
	// reviewer already warned about something.
	Notes string `yaml:"notes,omitempty"`
}

// Covered reports whether the provider implements this group.
func (e Entry) Covered() bool { return len(e.ImplementedBy) > 0 }

// Excluded reports whether this entry caps generation.
func (e Entry) Excluded() bool { return e.Ceiling != "" || e.Rationale != "" }

// Manifest is the hand-curated record of intent, keyed by the SDK's dotted group
// path.
type Manifest struct {
	Version   int              `yaml:"version"`
	SDKModule string           `yaml:"sdk_module"`
	Groups    map[string]Entry `yaml:"groups"`
}

// LoadManifest reads and parses the manifest at path.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("sdkcoverage: reading manifest: %w", err)
	}

	var manifest Manifest
	// KnownFields makes a typo in a manifest key an error rather than a setting
	// that silently does nothing.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("sdkcoverage: parsing %s: %w", path, err)
	}

	// version and sdk_module are the manifest's identity. Decoding them without
	// checking them would let a manifest describe a different schema or a
	// different SDK than the one actually being reconciled, and still pass.
	if manifest.Version != SupportedManifestVersion {
		return Manifest{}, fmt.Errorf(
			"sdkcoverage: %s declares version %d, but this build understands version %d",
			path, manifest.Version, SupportedManifestVersion)
	}
	if manifest.SDKModule != SDKModulePath {
		return Manifest{}, fmt.Errorf(
			"sdkcoverage: %s declares sdk_module %q, but coverage is reconciled against %q",
			path, manifest.SDKModule, SDKModulePath)
	}

	if manifest.Groups == nil {
		manifest.Groups = map[string]Entry{}
	}
	return manifest, nil
}

func knownCeilings() []Ceiling {
	return []Ceiling{CeilingNone, CeilingDataSource}
}

func knownRationales() []Rationale {
	return []Rationale{RationaleAPIConstraint, RationaleProductDecision, RationaleDeprecated}
}
