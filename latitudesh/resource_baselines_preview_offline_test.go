package latitudesh

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestBaselineDiskLayoutRoundTrip(t *testing.T) {
	role := components.BaselineDiskLayoutGroupRoleStorage
	count := int64(2)
	raid := components.RaidLevelRaid1
	fs := components.FilesystemExt4
	mount := "/data"

	in := []components.BaselineDiskLayoutGroup{
		{
			Role:       &role,
			Count:      &count,
			RaidLevel:  &raid,
			Filesystem: &fs,
			MountPoint: &mount,
		},
	}

	model := baselineDiskLayoutToModel(in)
	if len(model) != 1 {
		t.Fatalf("expected 1 disk layout group, got %d", len(model))
	}
	if model[0].Role.ValueString() != "storage" {
		t.Errorf("role = %q, want storage", model[0].Role.ValueString())
	}
	if model[0].Count.ValueInt64() != 2 {
		t.Errorf("count = %d, want 2", model[0].Count.ValueInt64())
	}
	if model[0].RaidLevel.ValueString() != "raid-1" {
		t.Errorf("raid_level = %q, want raid-1", model[0].RaidLevel.ValueString())
	}
	if model[0].Filesystem.ValueString() != "ext4" {
		t.Errorf("filesystem = %q, want ext4", model[0].Filesystem.ValueString())
	}
	if model[0].MountPoint.ValueString() != "/data" {
		t.Errorf("mount_point = %q, want /data", model[0].MountPoint.ValueString())
	}

	back := baselineDiskLayoutFromModel(model)
	if len(back) != 1 {
		t.Fatalf("expected 1 disk layout group back, got %d", len(back))
	}
	if *back[0].Role != components.BaselineDiskLayoutGroupRoleStorage {
		t.Errorf("round-tripped role = %q, want storage", *back[0].Role)
	}
	if *back[0].Count != 2 {
		t.Errorf("round-tripped count = %d, want 2", *back[0].Count)
	}
	if *back[0].RaidLevel != components.RaidLevelRaid1 {
		t.Errorf("round-tripped raid_level = %q, want raid-1", *back[0].RaidLevel)
	}
	if *back[0].Filesystem != components.FilesystemExt4 {
		t.Errorf("round-tripped filesystem = %q, want ext4", *back[0].Filesystem)
	}
	if *back[0].MountPoint != "/data" {
		t.Errorf("round-tripped mount_point = %q, want /data", *back[0].MountPoint)
	}
}

// TestBaselineDiskLayoutToModelEmpty guards the "nil in, nil out" contract:
// an empty API disk_layout must clear state rather than leave a stale value.
func TestBaselineDiskLayoutToModelEmpty(t *testing.T) {
	if got := baselineDiskLayoutToModel(nil); got != nil {
		t.Errorf("baselineDiskLayoutToModel(nil) = %#v, want nil", got)
	}
	if got := baselineDiskLayoutToModel([]components.BaselineDiskLayoutGroup{}); got != nil {
		t.Errorf("baselineDiskLayoutToModel(empty) = %#v, want nil", got)
	}
}

func TestBaselinePlatformSlugs(t *testing.T) {
	slugA := "c2-small-x86"
	slugB := "c2-medium-x86"
	in := []components.Platforms{
		{Slug: &slugA},
		{Slug: nil}, // a platform missing its slug is skipped, not zero-valued
		{Slug: &slugB},
	}

	got := baselinePlatformSlugs(in)
	want := []string{slugA, slugB}
	if len(got) != len(want) {
		t.Fatalf("baselinePlatformSlugs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("baselinePlatformSlugs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBaselineSSHKeyIDs(t *testing.T) {
	id := "sshk_123"
	in := []components.BaselineDataSSHKeys{
		{ID: &id},
		{ID: nil},
	}

	got := baselineSSHKeyIDs(in)
	if len(got) != 1 || got[0] != id {
		t.Errorf("baselineSSHKeyIDs() = %v, want [%q]", got, id)
	}
}

// TestValidateBaselinesPreviewConfig_PlatformsRequired guards the SDK's field
// doc for CreateBaselineAttributes.Platforms ("Required when target_type is
// \"platforms\"") — a config that targets platforms without listing any must
// fail at plan time rather than surface as a 422 from the API.
func TestValidateBaselinesPreviewConfig_PlatformsRequired(t *testing.T) {
	data := BaselinesPreviewResourceModel{
		Name:            types.StringValue("test"),
		TargetType:      types.StringValue("platforms"),
		OperatingSystem: types.StringValue("ubuntu_24_04_x64_lts"),
		Platforms:       types.ListNull(types.StringType),
	}

	diags := validateBaselinesPreviewConfig(data)
	if !diags.HasError() {
		t.Fatal("expected an error for target_type=platforms with no platforms, got none")
	}

	found := false
	for _, d := range diags.Errors() {
		if d.Summary() == "Missing platforms" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a %q diagnostic, got: %v", "Missing platforms", diags)
	}
}

// TestValidateBaselinesPreviewConfig_PlatformsSatisfied is the paired
// happy-path case: a non-empty platforms list must not raise the error above.
func TestValidateBaselinesPreviewConfig_PlatformsSatisfied(t *testing.T) {
	platforms, diags := types.ListValueFrom(context.Background(), types.StringType, []string{"c2-small-x86"})
	if diags.HasError() {
		t.Fatalf("building platforms list: %v", diags)
	}

	data := BaselinesPreviewResourceModel{
		Name:            types.StringValue("test"),
		TargetType:      types.StringValue("platforms"),
		OperatingSystem: types.StringValue("ubuntu_24_04_x64_lts"),
		Platforms:       platforms,
	}

	if got := validateBaselinesPreviewConfig(data); got.HasError() {
		t.Errorf("expected no error with platforms set, got: %v", got)
	}
}

// TestValidateBaselinesPreviewConfig_DiskLayoutRolePlacement guards the field
// placement rules from components.BaselineDiskLayoutGroup's doc comments:
// raid_level/filesystem are "only valid" for specific roles, and mount_point
// is "required for the 'storage' role".
func TestValidateBaselinesPreviewConfig_DiskLayoutRolePlacement(t *testing.T) {
	nullLayout := func(role string) BaselineDiskLayoutModel {
		return BaselineDiskLayoutModel{
			Role:       types.StringValue(role),
			Count:      types.Int64Null(),
			RaidLevel:  types.StringNull(),
			Filesystem: types.StringNull(),
			MountPoint: types.StringNull(),
		}
	}

	cases := []struct {
		name    string
		layout  func() BaselineDiskLayoutModel
		wantErr bool
	}{
		{"storage without mount_point", func() BaselineDiskLayoutModel { return nullLayout("storage") }, true},
		{"storage with mount_point", func() BaselineDiskLayoutModel {
			l := nullLayout("storage")
			l.MountPoint = types.StringValue("/data")
			return l
		}, false},
		{"os with filesystem", func() BaselineDiskLayoutModel {
			l := nullLayout("os")
			l.Filesystem = types.StringValue("ext4")
			return l
		}, true},
		{"os without filesystem or mount", func() BaselineDiskLayoutModel { return nullLayout("os") }, false},
		{"raw with raid_level", func() BaselineDiskLayoutModel {
			l := nullLayout("raw")
			l.RaidLevel = types.StringValue("raid-1")
			return l
		}, true},
		{"raw with nothing extra", func() BaselineDiskLayoutModel { return nullLayout("raw") }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := BaselinesPreviewResourceModel{
				Name:            types.StringValue("test"),
				TargetType:      types.StringValue("all_servers"),
				OperatingSystem: types.StringValue("ubuntu_24_04_x64_lts"),
				Platforms:       types.ListNull(types.StringType),
				DiskLayout:      []BaselineDiskLayoutModel{tc.layout()},
			}
			diags := validateBaselinesPreviewConfig(data)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("%s: HasError() = %v, want %v (diags: %v)", tc.name, got, tc.wantErr, diags)
			}
		})
	}
}
