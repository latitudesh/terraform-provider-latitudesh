package sdkcoverage

import (
	"reflect"
	"testing"
)

// Every case here is a real naming pattern taken from latitudesh-go-sdk v1.18.1.
// Exact-name CRUD matching fails on most of them, which is why the classifier is
// prefix-based.
func TestClassifyRealSDKNamingPatterns(t *testing.T) {
	tests := []struct {
		name    string
		methods []string
		want    string
	}{
		{
			name:    "bare CRUD with suffixed update",
			methods: []string{"Create", "List", "Get", "Delete", "UpdateVirtualMachine"},
			want:    "CRUD",
		},
		{
			name:    "single read is Retrieve, not Get",
			methods: []string{"Create", "Retrieve", "Update", "Delete", "ListAll", "RemoveFromProject"},
			want:    "CRUD",
		},
		{
			name:    "HTTP-verb prefixes and no update",
			methods: []string{"GetStorageVolumes", "PostStorageVolumes", "GetStorageVolume", "DeleteStorageVolumes"},
			want:    "CR-D",
		},
		{
			name:    "fully entity-suffixed CRUD",
			methods: []string{"ListKubernetesClusters", "CreateKubernetesCluster", "GetKubernetesCluster", "UpdateKubernetesCluster", "DeleteKubernetesCluster"},
			want:    "CRUD",
		},
		{
			name:    "readable only through List, no Get at all",
			methods: []string{"List", "Create", "Update", "Delete"},
			want:    "CRUD",
		},
		{
			name:    "delete-only fragment is not resource-shaped",
			methods: []string{"Delete"},
			want:    "---D",
		},
		{
			name:    "read-only group maps to a data source",
			methods: []string{"List"},
			want:    "-R--",
		},
		{
			name:    "Fetch counts as a read",
			methods: []string{"Fetch", "Get"},
			want:    "-R--",
		},
		{
			name:    "assignment create and delete",
			methods: []string{"Assign", "ListAssignments", "DeleteAssignment"},
			want:    "CR-D",
		},
		{
			name:    "Modify counts as an update",
			methods: []string{"ModifyProjectKey"},
			want:    "--U-",
		},
		{
			name:    "no methods at all",
			methods: nil,
			want:    "----",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.methods).Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The longest matching prefix has to win, or "Unlock" reads as "Lock" and
// "UnscheduleDeletion" as "Schedule".
func TestClassifyPrefersLongestPrefix(t *testing.T) {
	tests := []struct {
		method string
		want   capability
	}{
		{"Lock", capAction},
		{"Unlock", capAction},
		{"ScheduleDeletion", capAction},
		{"UnscheduleDeletion", capAction},
		{"Update", capUpdate},
		{"Put", capUpdate},
		{"Post", capCreate},
		{"Patch", capUpdate},
		{"DeleteAssignment", capDelete},
		{"DestroyNetworkAttachment", capDelete},
		{"ShowVirtualMachineMetrics", capRead},
		{"FrobnicateWidget", capUnknown},
	}

	for _, tt := range tests {
		if got := classifyMethod(tt.method); got != tt.want {
			t.Errorf("classifyMethod(%q) = %v, want %v", tt.method, got, tt.want)
		}
	}
}

func TestClassifySeparatesActionsFromCRUD(t *testing.T) {
	caps := Classify([]string{
		"List", "Create", "Get", "Update", "Delete",
		"Lock", "Unlock", "RunAction", "StartRescueMode", "ExitRescueMode",
		"ScheduleDeletion", "UnscheduleDeletion", "Reinstall",
	})

	if got := caps.Summary(); got != "CRUD" {
		t.Errorf("Summary() = %q, want CRUD", got)
	}
	want := []string{
		"Lock", "Unlock", "RunAction", "StartRescueMode", "ExitRescueMode",
		"ScheduleDeletion", "UnscheduleDeletion", "Reinstall",
	}
	if !reflect.DeepEqual(caps.Actions, want) {
		t.Errorf("Actions = %v, want %v", caps.Actions, want)
	}
	if len(caps.Unclassified) != 0 {
		t.Errorf("Unclassified = %v, want none", caps.Unclassified)
	}
}

// An unfamiliar naming style must surface rather than being silently dropped, so
// a human notices that the classifier needs a new rule.
func TestClassifyReportsUnclassifiedMethods(t *testing.T) {
	caps := Classify([]string{"FrobnicateWidget", "Get"})

	if want := []string{"FrobnicateWidget"}; !reflect.DeepEqual(caps.Unclassified, want) {
		t.Errorf("Unclassified = %v, want %v", caps.Unclassified, want)
	}
	if !caps.Readable {
		t.Error("Readable = false, want true (Get should still classify)")
	}
}

func TestCapabilityShapes(t *testing.T) {
	tests := []struct {
		name           string
		methods        []string
		wantResource   bool
		wantDataSource bool
	}{
		{"full CRUD", []string{"Create", "Get", "Update", "Delete"}, true, false},
		{"create read delete", []string{"PostStorageVolumes", "GetStorageVolume", "DeleteStorageVolumes"}, true, false},
		{"read only", []string{"List", "Get"}, false, true},
		{"delete only fragment", []string{"Delete"}, false, false},
		{"actions only", []string{"Lock", "Unlock"}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := Classify(tt.methods)
			if got := caps.ResourceShaped(); got != tt.wantResource {
				t.Errorf("ResourceShaped() = %v, want %v", got, tt.wantResource)
			}
			if got := caps.DataSourceShaped(); got != tt.wantDataSource {
				t.Errorf("DataSourceShaped() = %v, want %v", got, tt.wantDataSource)
			}
		})
	}
}
