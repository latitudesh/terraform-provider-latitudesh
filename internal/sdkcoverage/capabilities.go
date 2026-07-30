package sdkcoverage

import (
	"sort"
	"strings"
)

// capability is what a single SDK method tells us a group can do.
type capability int

const (
	capUnknown capability = iota
	capCreate
	capRead
	capUpdate
	capDelete
	capAction
)

// Capabilities is the CRUD shape of a service group, derived from its method
// names on every run and never persisted — so the manifest cannot drift out of
// sync with the SDK.
type Capabilities struct {
	Creatable bool
	Readable  bool
	Updatable bool
	Deletable bool

	// Actions are methods that operate on an existing entity rather than
	// implementing CRUD (server lock, rescue mode, metrics). A group made up
	// only of these is usually not resource-worthy.
	Actions []string

	// Unclassified are methods whose name matched no known prefix. A non-empty
	// list means the SDK introduced a naming style this classifier does not
	// understand yet, and a human should look.
	Unclassified []string
}

// Summary renders the CRUD shape compactly, e.g. "CRUD", "CR-D", "-R--".
func (c Capabilities) Summary() string {
	flag := func(set bool, letter string) string {
		if set {
			return letter
		}
		return "-"
	}
	return flag(c.Creatable, "C") + flag(c.Readable, "R") + flag(c.Updatable, "U") + flag(c.Deletable, "D")
}

// ResourceShaped reports whether the group looks like something Terraform could
// manage: it can be created and destroyed, and read back.
func (c Capabilities) ResourceShaped() bool {
	return c.Creatable && c.Deletable && c.Readable
}

// DataSourceShaped reports whether the group is strictly read-only, which maps to
// a Terraform data source rather than a resource (regions, roles).
//
// Updatable counts against this: Plans and UserProfile are read-mostly but do
// expose an update, so calling them read-only would overstate the case. They fall
// through as neither shape and get triaged by hand.
func (c Capabilities) DataSourceShaped() bool {
	return c.Readable && !c.Creatable && !c.Deletable && !c.Updatable
}

// ShapeHint describes what a group could become in Terraform, based only on its
// derived capabilities. It is a hint for triage, not a decision — the manifest's
// ceiling is where a decision gets recorded.
func (c Capabilities) ShapeHint() string {
	switch {
	case c.ResourceShaped():
		return "resource-shaped"
	case c.DataSourceShaped():
		return "read-only"
	default:
		return "partial lifecycle"
	}
}

// methodPrefixes maps a method-name prefix to the capability it implies.
//
// Prefix matching rather than exact names is deliberate: the SDK is generated
// per-operation and its CRUD naming is not consistent across groups. All of
// VirtualMachines.UpdateVirtualMachine, BlockStorage.PostStorageVolumes,
// SSHKeys.Retrieve, ElasticIps.CreateElasticIP and Tags.List (a group with no
// Get at all) have to land in the right bucket.
var methodPrefixes = map[string]capability{
	// create
	"Create": capCreate,
	"Post":   capCreate,
	"Assign": capCreate,
	// read
	"Get":      capRead,
	"List":     capRead,
	"Retrieve": capRead,
	"Fetch":    capRead,
	"Show":     capRead,
	// update
	"Update": capUpdate,
	"Patch":  capUpdate,
	"Put":    capUpdate,
	"Modify": capUpdate,
	// delete
	"Delete":  capDelete,
	"Destroy": capDelete,
	"Remove":  capDelete,
	// actions
	"Lock":       capAction,
	"Unlock":     capAction,
	"Start":      capAction,
	"Exit":       capAction,
	"Run":        capAction,
	"Reinstall":  capAction,
	"Schedule":   capAction,
	"Unschedule": capAction,
	"Refresh":    capAction,
	"Mount":      capAction,
}

// orderedPrefixes is methodPrefixes sorted longest-first, so that the most
// specific prefix wins: "UnscheduleDeletion" must match "Unschedule" and not
// "Schedule", and "Unlock" must not be read as "Lock".
var orderedPrefixes = buildOrderedPrefixes()

func buildOrderedPrefixes() []string {
	prefixes := make([]string, 0, len(methodPrefixes))
	for prefix := range methodPrefixes {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if len(prefixes[i]) != len(prefixes[j]) {
			return len(prefixes[i]) > len(prefixes[j])
		}
		return prefixes[i] < prefixes[j]
	})
	return prefixes
}

// Classify derives a group's CRUD shape from its method names.
func Classify(methods []string) Capabilities {
	var caps Capabilities
	for _, method := range methods {
		switch classifyMethod(method) {
		case capCreate:
			caps.Creatable = true
		case capRead:
			caps.Readable = true
		case capUpdate:
			caps.Updatable = true
		case capDelete:
			caps.Deletable = true
		case capAction:
			caps.Actions = append(caps.Actions, method)
		default:
			caps.Unclassified = append(caps.Unclassified, method)
		}
	}
	return caps
}

func classifyMethod(method string) capability {
	for _, prefix := range orderedPrefixes {
		if strings.HasPrefix(method, prefix) {
			return methodPrefixes[prefix]
		}
	}
	return capUnknown
}
