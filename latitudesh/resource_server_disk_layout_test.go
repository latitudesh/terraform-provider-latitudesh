package latitudesh

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dlGroup builds a DiskLayoutModel. Empty raid/mount strings are treated as
// "not set" (null); use dlGroupRaw when an explicit empty string is needed.
func dlGroup(count int64, role, raid, mount string) DiskLayoutModel {
	m := DiskLayoutModel{
		Count:      types.Int64Value(count),
		Role:       types.StringValue(role),
		RaidLevel:  types.StringNull(),
		MountPoint: types.StringNull(),
	}
	if raid != "" {
		m.RaidLevel = types.StringValue(raid)
	}
	if mount != "" {
		m.MountPoint = types.StringValue(mount)
	}
	return m
}

func TestValidateDiskLayoutGroups(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		groups    []DiskLayoutModel
		shouldErr bool
	}{
		{
			name: "valid os + storage with raid",
			groups: []DiskLayoutModel{
				dlGroup(2, "os", "raid-1", ""),
				dlGroup(2, "storage", "raid-0", "/data"),
			},
		},
		{
			name:   "valid minimal single os",
			groups: []DiskLayoutModel{dlGroup(1, "os", "", "")},
		},
		{
			name:   "valid os + raw",
			groups: []DiskLayoutModel{dlGroup(2, "os", "raid-1", ""), dlGroup(2, "raw", "", "")},
		},
		{
			name:      "two os groups",
			groups:    []DiskLayoutModel{dlGroup(1, "os", "", ""), dlGroup(1, "os", "", "")},
			shouldErr: true,
		},
		{
			name:      "no os group",
			groups:    []DiskLayoutModel{dlGroup(1, "raw", "", "")},
			shouldErr: true,
		},
		{
			name: "two storage groups",
			groups: []DiskLayoutModel{
				dlGroup(1, "os", "", ""),
				dlGroup(1, "storage", "", "/a"),
				dlGroup(1, "storage", "", "/b"),
			},
			shouldErr: true,
		},
		{
			name:      "storage without mount_point",
			groups:    []DiskLayoutModel{dlGroup(1, "os", "", ""), dlGroup(1, "storage", "", "")},
			shouldErr: true,
		},
		{
			name:      "mount_point on os",
			groups:    []DiskLayoutModel{dlGroup(1, "os", "", "/boot")},
			shouldErr: true,
		},
		{
			name:      "raid_level on raw",
			groups:    []DiskLayoutModel{dlGroup(1, "os", "", ""), dlGroup(2, "raw", "raid-0", "")},
			shouldErr: true,
		},
		{
			name:      "mount_point on raw",
			groups:    []DiskLayoutModel{dlGroup(1, "os", "", ""), dlGroup(1, "raw", "", "/x")},
			shouldErr: true,
		},
		{
			name:      "raid_level requires count >= 2",
			groups:    []DiskLayoutModel{dlGroup(1, "os", "raid-1", "")},
			shouldErr: true,
		},
		{
			name:      "count below 1",
			groups:    []DiskLayoutModel{dlGroup(0, "os", "", "")},
			shouldErr: true,
		},
		{
			// Empty-string mount_point on os is treated as "not set" and must not
			// error, matching how the API-mapping helpers omit it.
			name: "empty mount_point on os is ignored",
			groups: []DiskLayoutModel{{
				Count:      types.Int64Value(1),
				Role:       types.StringValue("os"),
				RaidLevel:  types.StringNull(),
				MountPoint: types.StringValue(""),
			}},
		},
		{
			// Unknown role skips the cross-group os/storage counting so it does
			// not produce false errors on interpolated configs.
			name: "unknown role skips cross-group checks",
			groups: []DiskLayoutModel{{
				Count:      types.Int64Value(1),
				Role:       types.StringUnknown(),
				RaidLevel:  types.StringNull(),
				MountPoint: types.StringNull(),
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			validateDiskLayoutGroups(tc.groups, &diags)
			if tc.shouldErr && !diags.HasError() {
				t.Fatalf("expected an error, got none")
			}
			if !tc.shouldErr && diags.HasError() {
				t.Fatalf("expected no error, got: %v", diags.Errors())
			}
		})
	}
}

func TestDiskLayoutChanged(t *testing.T) {
	t.Parallel()

	base := []DiskLayoutModel{
		dlGroup(2, "os", "raid-1", ""),
		dlGroup(2, "storage", "raid-0", "/data"),
	}

	cases := []struct {
		name    string
		a, b    []DiskLayoutModel
		changed bool
	}{
		{"both empty", nil, nil, false},
		{"identical", base, base, false},
		{
			"different length",
			base,
			[]DiskLayoutModel{dlGroup(2, "os", "raid-1", "")},
			true,
		},
		{
			"reordered groups (order is significant)",
			base,
			[]DiskLayoutModel{
				dlGroup(2, "storage", "raid-0", "/data"),
				dlGroup(2, "os", "raid-1", ""),
			},
			true,
		},
		{
			"raid_level null vs value",
			[]DiskLayoutModel{dlGroup(2, "os", "", "")},
			[]DiskLayoutModel{dlGroup(2, "os", "raid-1", "")},
			true,
		},
		{
			"count differs",
			[]DiskLayoutModel{dlGroup(2, "os", "raid-1", "")},
			[]DiskLayoutModel{dlGroup(4, "os", "raid-1", "")},
			true,
		},
		{
			"empty vs non-empty",
			nil,
			base,
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diskLayoutChanged(tc.a, tc.b); got != tc.changed {
				t.Fatalf("diskLayoutChanged = %v, want %v", got, tc.changed)
			}
		})
	}
}
