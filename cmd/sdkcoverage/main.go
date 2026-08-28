// Command sdkcoverage reports how much of the pinned latitudesh-go-sdk surface
// the Terraform provider exposes, and fails when the SDK, the coverage manifest,
// and the provider's registration disagree.
//
// Usage:
//
//	sdkcoverage report            # markdown coverage table
//	sdkcoverage check             # exit 1 on any violation
//	sdkcoverage groups            # dump the raw SDK surface (no manifest needed)
//	sdkcoverage shipped           # registered Terraform type names by kind, as JSON
//	sdkcoverage fields            # field-level shape of the covered groups (-write updates the lock)
//	sdkcoverage drift             # diff the lock against an SDK tree (-strict fails on breaking drift)
//
// Everything runs offline against the module cache; no API token is involved.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/latitudesh/terraform-provider-latitudesh/v2/internal/sdkcoverage"
	"github.com/latitudesh/terraform-provider-latitudesh/v2/latitudesh"
)

// providerTypeName mirrors what the provider reports in its own Metadata, and is
// the prefix every Terraform type name carries.
const providerTypeName = "latitudesh"

func main() {
	if err := run(os.Args[1:]); err != nil {
		// Errors from internal/sdkcoverage already carry a "sdkcoverage:" prefix.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no command given")
	}

	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	manifestPath := flags.String("manifest", defaultManifestPath(), "path to the coverage manifest")
	sdkDir := flags.String("sdk-dir", "", "SDK source directory (defaults to the pinned module in the module cache)")
	format := flags.String("format", "", "report format: markdown, text, or json (report and drift)")
	lockPath := flags.String("lock", "", "path to the field lock (defaults to "+sdkcoverage.FieldLockFile+" next to the manifest)")
	write := flags.Bool("write", false, "write the field lock instead of printing it (fields only)")
	group := flags.String("group", "", "restrict the lock update to the named covered group(s), comma-separated (fields only)")
	strict := flags.Bool("strict", false, "exit non-zero on breaking drift (drift only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *lockPath == "" {
		*lockPath = filepath.Join(filepath.Dir(*manifestPath), sdkcoverage.FieldLockFile)
	}

	switch command {
	case "groups":
		return runGroups(*sdkDir)
	case "report":
		return runReport(*manifestPath, *sdkDir, *format)
	case "check":
		return runCheck(*manifestPath, *sdkDir, *lockPath)
	case "shipped":
		return runShipped()
	case "fields":
		return runFields(*manifestPath, *sdkDir, *lockPath, *group, *write)
	case "drift":
		return runDrift(*manifestPath, *sdkDir, *lockPath, *format, *strict)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: sdkcoverage <report|check|groups|shipped|fields|drift> [flags]

  report   render the coverage table (markdown by default)
  check    exit non-zero when the SDK, the manifest, and the provider disagree
  groups   dump the raw SDK service-group surface, for seeding the manifest
  shipped  dump the registered Terraform type names by kind, as JSON
  fields   render the covered groups' field-level shape as a lock document
  drift    diff the committed field lock against an SDK tree

flags:
  -manifest path   coverage manifest (default: sdk-coverage.yaml at the repo root)
  -sdk-dir path    SDK source directory (default: pinned module in the module cache)
  -format fmt      markdown (default), text, or json, for report and drift
  -lock path       field lock (default: sdk-fields.lock.yaml next to the manifest)
  -write           write the field lock instead of printing it, for fields
  -group names     restrict the lock update to these covered groups (comma-separated), for fields
  -strict          exit non-zero on breaking drift, for drift
`)
}

// runGroups prints the SDK surface without consulting the manifest, which is how
// the manifest gets seeded in the first place and how a human inspects what the
// parser actually sees.
func runGroups(sdkDir string) error {
	surface, dir, err := loadSurface(sdkDir)
	if err != nil {
		return err
	}

	fmt.Printf("# %s\n# %d service groups\n\n", dir, len(surface.Groups))
	for _, name := range surface.Names() {
		group := surface.Groups[name]
		caps := sdkcoverage.Classify(group.Methods)

		fmt.Printf("%-26s %s", name, caps.Summary())
		fmt.Printf("  %s\n", caps.ShapeHint())

		if group.TypeName != lastSegment(name) {
			fmt.Printf("  type: %s\n", group.TypeName)
		}
		for _, method := range group.Methods {
			fmt.Printf("  - %s\n", method)
		}
		fmt.Println()
	}
	return nil
}

func runReport(manifestPath, sdkDir, format string) error {
	report, version, err := reconcile(manifestPath, sdkDir)
	if err != nil {
		return err
	}

	switch format {
	case "text":
		fmt.Print(report.Text(version))
	case "", "markdown":
		fmt.Print(report.Markdown(version))
	case "json":
		data, err := report.JSON(version, providerTypeName)
		if err != nil {
			return fmt.Errorf("sdkcoverage: rendering JSON report: %w", err)
		}
		fmt.Println(string(data))
	default:
		// Falling back to markdown here would hand automation a different format
		// than it asked for, and report success while doing it.
		return fmt.Errorf("unsupported report format %q (want markdown, text, or json)", format)
	}
	return nil
}

func runCheck(manifestPath, sdkDir, lockPath string) error {
	report, version, err := reconcile(manifestPath, sdkDir)
	if err != nil {
		return err
	}

	if !report.OK() {
		fmt.Fprintf(os.Stderr, "%d violation(s):\n", len(report.Violations))
		for _, v := range report.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		return fmt.Errorf("coverage manifest is out of sync")
	}

	// Field drift is checked with the same severity split as the gate test:
	// breaking drift between this SDK tree and the committed lock fails, so this
	// command and TestProviderSDKFieldDrift cannot disagree; additive drift is a
	// note. A missing lock is inert — pre-seed checkouts keep working.
	if err := checkFieldDrift(manifestPath, sdkDir, lockPath); err != nil {
		return err
	}

	fmt.Printf("ok: %s — %d/%d covered, %d pending generation, %d excluded, %d need a human\n",
		version, len(report.Covered), report.Total(),
		len(report.Pending), len(report.Excluded), len(report.Unshaped))
	if len(report.Revisit) > 0 {
		// Informational by design: an API that grew is an opportunity, not a
		// defect, so it must not fail the run.
		fmt.Printf("note: %d group(s) excluded on API grounds now exceed their recorded ceiling — see `report`\n",
			len(report.Revisit))
	}
	return nil
}

func checkFieldDrift(manifestPath, sdkDir, lockPath string) error {
	lock, err := sdkcoverage.LoadFieldLock(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	fields, _, err := loadFieldSurface(manifestPath, sdkDir)
	if err != nil {
		return err
	}
	manifest, err := sdkcoverage.LoadManifest(manifestPath)
	if err != nil {
		return err
	}

	drift := sdkcoverage.DiffFieldSurfaces(lock.Surface(), fields, manifest)
	var breaking []sdkcoverage.Drift
	informational := 0
	for _, d := range drift {
		if d.Breaking() {
			breaking = append(breaking, d)
		} else {
			informational++
		}
	}

	if len(breaking) > 0 {
		fmt.Fprintf(os.Stderr, "%d breaking field drift(s) against %s:\n", len(breaking), lockPath)
		for _, d := range breaking {
			fmt.Fprintf(os.Stderr, "  - %s\n", d)
		}
		return fmt.Errorf("field lock is out of sync — fix the mapping (or deliberately omit it), then `make fields-sync`")
	}
	if informational > 0 {
		fmt.Printf("note: %d informational field drift(s) on covered groups — see `drift`\n", informational)
	}
	return nil
}

// runShipped prints the provider's registered type names split by kind. The
// scaffold validation gate consumes this to verify that every REQUESTED kind was
// actually registered — the merged view cannot answer that, because one kind
// claiming a type name (ssh_key the resource) hides whether the sibling kind
// (ssh_key the data source) was ever wired up.
func runShipped() error {
	ctx := context.Background()
	shipped := sdkcoverage.ShippedByKind(ctx, latitudesh.New("dev")(), providerTypeName)

	data, err := json.MarshalIndent(shipped, "", "  ")
	if err != nil {
		return fmt.Errorf("sdkcoverage: rendering shipped types: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// runFields renders the field-level shape of the covered groups as a lock
// document. -write is the only path in this tool that touches the filesystem,
// and it is how drift gets accepted: regenerate the lock in the PR that maps
// (or deliberately omits) the change, and let the lock's diff be reviewed.
//
// -group narrows the update to the named covered groups, keeping the rest of
// the lock byte-for-byte untouched. That is what lets a PR fixing some groups'
// drift avoid silently accepting every other group's — the full regenerate is
// for seeding and for PRs that really do triage everything.
func runFields(manifestPath, sdkDir, lockPath, group string, write bool) error {
	fields, _, err := loadFieldSurface(manifestPath, sdkDir)
	if err != nil {
		return err
	}

	var lock sdkcoverage.FieldLock
	if group == "" {
		lock = sdkcoverage.BuildFieldLock(fields)
	} else {
		lock, err = sdkcoverage.LoadFieldLock(lockPath)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("sdkcoverage: -group updates an existing lock, and %s does not exist — seed it first with a full `fields -write`", lockPath)
		}
		if err != nil {
			return err
		}
		for _, g := range strings.Split(group, ",") {
			g = strings.TrimSpace(g)
			models, ok := fields.Groups[g]
			if !ok {
				return fmt.Errorf("sdkcoverage: %q is not a covered group in this SDK tree (see `report`)", g)
			}
			lock.Groups[g] = models
		}
		// The header labels the tree the lock was last written from; the
		// untouched groups keep their acknowledged shapes regardless.
		lock.SDKVersion = fields.SDKVersion
	}

	if !write {
		fmt.Print(string(lock.Marshal()))
		return nil
	}
	if err := sdkcoverage.WriteFieldLock(lockPath, lock); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d covered group(s), SDK %s)\n", lockPath, len(lock.Groups), lock.SDKVersion)
	return nil
}

// runDrift diffs the committed lock against an SDK tree. A missing lock is an
// inert no-op, not an error: it keeps the command safe to wire into automation
// before the lock has ever been seeded, and after, the gate makes sure the
// lock cannot quietly disappear.
func runDrift(manifestPath, sdkDir, lockPath, format string, strict bool) error {
	lock, err := sdkcoverage.LoadFieldLock(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return renderDrift(nil, "", "", true, format, strict)
	}
	if err != nil {
		return err
	}

	fields, _, err := loadFieldSurface(manifestPath, sdkDir)
	if err != nil {
		return err
	}

	manifest, err := sdkcoverage.LoadManifest(manifestPath)
	if err != nil {
		return err
	}

	drift := sdkcoverage.DiffFieldSurfaces(lock.Surface(), fields, manifest)
	return renderDrift(drift, lock.SDKVersion, fields.SDKVersion, false, format, strict)
}

func renderDrift(drift []sdkcoverage.Drift, lockVersion, sdkVersion string, lockMissing bool, format string, strict bool) error {
	switch format {
	case "text":
		if lockMissing {
			fmt.Println("no field lock found; nothing to diff (seed one with `sdkcoverage fields -write`)")
			return nil
		}
		fmt.Print(sdkcoverage.DriftText(drift, lockVersion, sdkVersion))
	case "", "markdown":
		if lockMissing {
			fmt.Println("no field lock found; nothing to diff (seed one with `sdkcoverage fields -write`)")
			return nil
		}
		fmt.Print(sdkcoverage.DriftMarkdown(drift, lockVersion, sdkVersion))
	case "json":
		data, err := sdkcoverage.DriftJSON(drift, lockVersion, sdkVersion, lockMissing)
		if err != nil {
			return fmt.Errorf("sdkcoverage: rendering drift JSON: %w", err)
		}
		fmt.Println(string(data))
	default:
		return fmt.Errorf("unsupported drift format %q (want markdown, text, or json)", format)
	}

	if strict {
		breaking := 0
		for _, d := range drift {
			if d.Breaking() {
				breaking++
			}
		}
		if breaking > 0 {
			return fmt.Errorf("%d breaking field drift(s) — fix the mapping or regenerate the lock (`make fields-sync`) and get the diff reviewed", breaking)
		}
	}
	return nil
}

// loadFieldSurface parses the field-level shape of the manifest's covered
// groups from the given (or pinned) SDK tree.
func loadFieldSurface(manifestPath, sdkDir string) (sdkcoverage.FieldSurface, string, error) {
	surface, dir, err := loadSurface(sdkDir)
	if err != nil {
		return sdkcoverage.FieldSurface{}, "", err
	}

	manifest, err := sdkcoverage.LoadManifest(manifestPath)
	if err != nil {
		return sdkcoverage.FieldSurface{}, "", err
	}

	var covered []string
	for name, entry := range manifest.Groups {
		// Covered groups that the SDK no longer exposes are the group-level
		// gate's finding, not a field-parse error.
		if _, ok := surface.Groups[name]; ok && entry.Covered() {
			covered = append(covered, name)
		}
	}
	sort.Strings(covered)

	fields, err := sdkcoverage.ParseFieldSurface(dir, surface, covered)
	if err != nil {
		return sdkcoverage.FieldSurface{}, "", err
	}
	return fields, dir, nil
}

func reconcile(manifestPath, sdkDir string) (sdkcoverage.Report, string, error) {
	surface, dir, err := loadSurface(sdkDir)
	if err != nil {
		return sdkcoverage.Report{}, "", err
	}

	manifest, err := sdkcoverage.LoadManifest(manifestPath)
	if err != nil {
		return sdkcoverage.Report{}, "", err
	}

	ctx := context.Background()
	shipped := sdkcoverage.ShippedTypeNames(ctx, latitudesh.New("dev")(), providerTypeName)

	return sdkcoverage.Reconcile(surface, manifest, shipped), sdkcoverage.VersionFromDir(dir), nil
}

func loadSurface(sdkDir string) (sdkcoverage.Surface, string, error) {
	dir := sdkDir
	if dir == "" {
		resolved, err := sdkcoverage.PinnedModuleDir(sdkcoverage.SDKModulePath)
		if err != nil {
			return sdkcoverage.Surface{}, "", err
		}
		dir = resolved
	}

	surface, err := sdkcoverage.ParseSurface(dir)
	if err != nil {
		return sdkcoverage.Surface{}, "", err
	}
	return surface, dir, nil
}

// defaultManifestPath finds the manifest by walking up from the working
// directory, so the command works from anywhere in the repo.
func defaultManifestPath() string {
	dir, err := os.Getwd()
	if err != nil {
		return sdkcoverage.ManifestFile
	}
	for {
		candidate := filepath.Join(dir, sdkcoverage.ManifestFile)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return sdkcoverage.ManifestFile
		}
		dir = parent
	}
}

func lastSegment(dotted string) string {
	if i := len(dotted) - 1; i >= 0 {
		for ; i >= 0; i-- {
			if dotted[i] == '.' {
				return dotted[i+1:]
			}
		}
	}
	return dotted
}
