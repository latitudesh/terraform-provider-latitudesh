// Command sdkcoverage reports how much of the pinned latitudesh-go-sdk surface
// the Terraform provider exposes, and fails when the SDK, the coverage manifest,
// and the provider's registration disagree.
//
// Usage:
//
//	sdkcoverage report            # markdown coverage table
//	sdkcoverage check             # exit 1 on any violation
//	sdkcoverage groups            # dump the raw SDK surface (no manifest needed)
//
// Everything runs offline against the module cache; no API token is involved.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	format := flags.String("format", "", "report format: markdown, text, or json (report only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	switch command {
	case "groups":
		return runGroups(*sdkDir)
	case "report":
		return runReport(*manifestPath, *sdkDir, *format)
	case "check":
		return runCheck(*manifestPath, *sdkDir)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: sdkcoverage <report|check|groups> [flags]

  report   render the coverage table (markdown by default)
  check    exit non-zero when the SDK, the manifest, and the provider disagree
  groups   dump the raw SDK service-group surface, for seeding the manifest

flags:
  -manifest path   coverage manifest (default: sdk-coverage.yaml at the repo root)
  -sdk-dir path    SDK source directory (default: pinned module in the module cache)
  -format fmt      markdown (default), text, or json, for report
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

func runCheck(manifestPath, sdkDir string) error {
	report, version, err := reconcile(manifestPath, sdkDir)
	if err != nil {
		return err
	}

	if report.OK() {
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

	fmt.Fprintf(os.Stderr, "%d violation(s):\n", len(report.Violations))
	for _, v := range report.Violations {
		fmt.Fprintf(os.Stderr, "  - %s\n", v)
	}
	return fmt.Errorf("coverage manifest is out of sync")
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
