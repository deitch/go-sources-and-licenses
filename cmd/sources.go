package cmd

import (
	"archive/zip"
	"bytes"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/deitch/go-sources-and-licenses/pkg"
)

const (
	modFile         = "go.mod"
	defaultTemplate = `{{.Module}} {{.Version}} {{.Licenses}} {{.Path}}`
)

type pkgInfo struct {
	Module   string
	Version  string
	Licenses []string
	Path     string
}

func (p pkgInfo) String() string {
	return fmt.Sprintf("%s@%s", p.Module, p.Version)
}

func sources() *cobra.Command {
	var (
		version, outpath, format, prefix string
		find, module, src, binary        bool
	)

	cmd := &cobra.Command{
		Use:     "sources",
		Aliases: []string{"source", "licenses", "license"},
		Short:   "Download source",
		Args:    cobra.ExactArgs(1),
		Long: `Download sources for a golang package or directory.
		There is to be a single argument, one of a module name, the path to a source directory, or the path to a binary.
		The usage of that argument is determined by the arguments --module, --src and --binary.

		Examples:
		
		get licenses for a module, asking for a specific version:
			licenses -m -v v1.21.0 cloud.google.com/go/storage 

		get licenses for a module, asking for the latest known version:
			licenses -m cloud.google.com/go/storage 

		get licenses for module source code:
			licenses -s $GOPATH/src/github.com/deitch/go-sources-and-licenses
		
		get sources for a module, asking for a specific version:
			sources -o /tmp/output.zip -m -v v1.21.0 cloud.google.com/go/storage

		get sources for module source code:
			sources -o /tmp/output.zip -s $GOPATH/src/github.com/deitch/go-sources-and-licenses
		
		get sources for source code to any modules found in the tree under a path (--find):
			sources -o /tmp/output.zip -s --find $GOPATH/src/github.com/deitch/
		
		get sources for a specific go binary
			sources -o /tmp/output.zip -b /usr/local/bin/compare

		get sources for any binary found in the tree under a path (--find)
			sources -o /tmp/output.zip -b --find /usr/local/bin
		`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				fsys       fs.FS
				err        error
				existing   = make(map[string]bool)
				pkgInfos   []pkgInfo
				moduleName string
			)

			target := args[0]

			tmpl, err := template.New("sources").Parse(format)
			if err != nil {
				return fmt.Errorf("failed to parse template: %v", err)
			}

			switch {
			case (cmd.CalledAs() == "sources" || cmd.CalledAs() == "source") && outpath == "":
				return fmt.Errorf("must specify output path")
			case (!module && !src && !binary) || (module && src) || (module && binary) || (src && binary) || (module && src && binary):
				return fmt.Errorf("must specify exactly one of --binary, --module or --src")
			case module:
				moduleName = target
				fsys, err = pkg.GetModule(moduleName, version, proxyURL, false)
				if err != nil {
					return fmt.Errorf("failed to get module %s: %v", moduleName, err)
				}
				log.Printf("writing module %s version %s from direct package", moduleName, version)
				added, err := writeModuleFromSource(outpath, prefix, moduleName, version, fsys, existing)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case src && !find:
				// get version
				if version == "" {
					version = GoVersion(target)
				}
				fsys = os.DirFS(target)
				log.Printf("writing module from source directory %s", target)
				added, err := writeModuleFromSource(outpath, prefix, "", version, fsys, existing)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case src && find:
				log.Printf("find for source enabled based at %s", target)
				fsys = os.DirFS(target)
				err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil && !errors.Is(err, io.EOF) {
						return fmt.Errorf("failed to walk %s: %v", path, err)
					}
					// we only are looking for directories with go.mod in them
					if !strings.HasSuffix(path, modFile) {
						return nil
					}
					dir := filepath.Dir(path)
					if version == "" {
						version = GoVersion(filepath.Join(target, dir))
					}
					sub, err := fs.Sub(fsys, dir)
					if err != nil {
						return fmt.Errorf("failed to get subdirectory %s: %v", path, err)
					}
					log.Printf("writing module from directory %s", dir)
					added, err := writeModuleFromSource(outpath, prefix, "", version, sub, existing)
					if err != nil {
						return err
					}
					for _, a := range added {
						existing[a.String()] = true
					}
					pkgInfos = append(pkgInfos, added...)
					return nil
				})
				if err != nil {
					return fmt.Errorf("failed to walk directory %s: %v", target, err)
				}
			case binary && !find:
				log.Printf("writing info from binary  %s", target)
				f, err := os.Open(target)
				if err != nil {
					return fmt.Errorf("failed to open %s: %v", target, err)
				}
				defer f.Close()
				added, err := writeModuleFromBinary(outpath, prefix, f, existing)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case binary && find:
				log.Printf("find for go binaries enabled based at %s", target)
				fsys = os.DirFS(target)
				err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil && !errors.Is(err, io.EOF) {
						return fmt.Errorf("failed to walk %s: %v", path, err)
					}
					// we only care about regular files
					if d.IsDir() {
						return nil
					}
					fi, err := d.Info()
					if err != nil {
						return fmt.Errorf("failed to get info for %s: %v", path, err)
					}
					if !fi.Mode().IsRegular() {
						return nil
					}
					// we only are looking for files of type golang
					f, err := fsys.Open(path)
					if err != nil {
						return fmt.Errorf("failed to open %s: %v", path, err)
					}
					defer f.Close()
					// since fsys is actually returned by os.DirFS, we know that returns a *os.File
					// which implements ReaderAt
					fra, ok := f.(io.ReaderAt)
					if !ok {
						return fmt.Errorf("failed to convert %s to io.ReaderAt", path)
					}
					added, err := writeModuleFromBinary(outpath, prefix, fra, existing)
					// unfortunately, go's buildinfo.Read() does not distinguish between errors opening the file,
					// and errors of the wrong file type. Oh well.
					if err != nil {
						return nil
					}
					log.Printf("scanned binary at %s", path)
					for _, a := range added {
						existing[a.String()] = true
					}
					pkgInfos = append(pkgInfos, added...)
					return nil
				})
				if err != nil {
					return fmt.Errorf("failed to walk directory %s: %v", target, err)
				}
			}

			for _, p := range pkgInfos {
				tmpl.Execute(os.Stdout, p)
				fmt.Println()
			}

			return nil
		},
	}
	cmd.Flags().BoolVarP(&module, "module", "m", false, "argument is name of module to find and check from the Internet")
	cmd.Flags().BoolVarP(&src, "src", "s", false, "argument is path to a golang module source directory to check. If provided with `--find`, will look for all directories in the tree, finding those with `go.mod` to treat as a module source and scan it.")
	cmd.Flags().BoolVarP(&binary, "binary", "b", false, "argument is a binary to check. If provided with `--find`, will look for all files in the tree, to see if it is a go binary and scan it.")
	cmd.Flags().StringVarP(&version, "version", "v", "", "version of a module to check; useful only with `--module`, no meaning otherwise. Leave blank to get latest.")
	cmd.Flags().BoolVarP(&find, "find", "f", false, "find recursively within the provided directory; useful only with --src and --binary, ignored otherwise")
	cmd.Flags().StringVarP(&outpath, "out", "o", "", "output directory for the zip files; useful only with `sources` command, ignored otherwise")
	cmd.Flags().StringVar(&format, "template", defaultTemplate, "output template to use. Available fields are: .Module, .Version, .Licenses, .Path")
	cmd.Flags().StringVar(&prefix, "prefix", "", "prefix to prepend to each output filename")
	return cmd
}

func cleanFilename(module, version, ext string) string {
	cleanModule := strings.Replace(module, "/", "_", -1)
	if version != "" {
		version = fmt.Sprintf("@%s", version)
	}
	return fmt.Sprintf("%s%s.%s", cleanModule, version, ext)
}

// getWriter returns a writer for the output file, and the filename. The filename is relative to the outpath,
// and not absolute
func getWriter(outpath, prefix, module, version string) (io.WriteCloser, string, error) {
	var (
		w        io.WriteCloser
		filename string
	)
	if outpath == "" {
		w = NopWriteCloser{io.Discard}
	} else {
		filename = cleanFilename(module, version, "zip")
		if prefix != "" {
			filename = filepath.Join(prefix, filename)
		}
		outFile := filepath.Join(outpath, filename)
		outDir := filepath.Dir(outFile)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, "", fmt.Errorf("failed to create output directory %s: %v", outDir, err)
		}
		// if the file already exists, we treat it as already downloaded
		// and skip it
		if fi, err := os.Stat(outFile); err == nil && fi.Size() != 0 {
			return NopWriteCloser{io.Discard}, filename, nil
		}
		f, err := os.Create(outFile)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create output file %s: %v", outFile, err)
		}
		w = f
	}

	return w, filename, nil
}

func writeModuleFromSource(outpath, prefix, name, version string, fsys fs.FS, existing map[string]bool) (pkgInfos []pkgInfo, err error) {
	info, err := writeModule(outpath, prefix, name, version, fsys)
	if err != nil {
		return nil, fmt.Errorf("failed to get package %s@%s: %w", name, version, err)
	}
	pkgInfos = append(pkgInfos, info)
	existing[info.String()] = true

	f, err := fsys.Open(modFile)
	if err != nil {
		log.Warnf("failed to open mod file %s@%s %s: %v", info.Path, info.Version, modFile, err)
	} else {
		defer f.Close()
		mod, err := pkg.ParseMod(f)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mod file %s@%s %s: %v", info.Path, info.Version, modFile, err)
		}
		for _, p := range mod.Requires {
			if _, ok := existing[p.String()]; ok {
				continue
			}
			// was it replaced? Try by version and then by name
			var (
				replaced bool
				info     pkgInfo
			)
			if r, ok := mod.Replace[p.String()]; ok {
				p = r
				replaced = true
			} else if r, ok := mod.Replace[p.Name]; ok {
				p = r
				replaced = true
			}
			// is the module a path one due to replaces? We ignore those
			if replaced && p.Version == "" {
				continue
			}
			_, info, err = getAndWriteModule(outpath, prefix, p.Name, p.Version)

			if err != nil {
				return nil, fmt.Errorf("failed to get package %s@%s: %v", p.Name, p.Version, err)
			}
			existing[p.String()] = true
			pkgInfos = append(pkgInfos, info)
		}
	}
	return
}

func writeModuleFromBinary(outpath, prefix string, r io.ReaderAt, existing map[string]bool) (pkgInfos []pkgInfo, err error) {
	info, err := buildinfo.Read(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read build info: %v", err)
	}
	name, version := info.Main.Path, info.Main.Version

	// we will not consider it an error if we cannot retrieve the version if it was calculated from ldflags,
	// only if it was actually part of the official binary itself
	var calculatedVersion bool
	// try to parse version from build flags
	if version == "" || version == "(devel)" {
		version = parseVersionFromBuildFlags(info.Settings)
		calculatedVersion = true
	}
	if version != "" && version != "(devel)" {
		_, info, err := getAndWriteModule(outpath, prefix, name, version)
		if err != nil && !calculatedVersion {
			return nil, fmt.Errorf("failed to get package %s@%s: %v", name, version, err)
		}
		if err == nil {
			existing[info.String()] = true
			pkgInfos = append(pkgInfos, info)
		}
	}

	for _, d := range info.Deps {
		if d.Version == "" || d.Version == "(devel)" {
			continue
		}
		if _, ok := existing[fmt.Sprintf("%s@%s", d.Path, d.Version)]; ok {
			continue
		}
		_, info, err := getAndWriteModule(outpath, prefix, d.Path, d.Version)
		if err != nil {
			if errors.Is(err, ErrNoModFile{}) {
				continue
			}
			return nil, fmt.Errorf("failed to get package %s@%s: %v", d.Path, d.Version, err)
		}
		existing[info.String()] = true
		pkgInfos = append(pkgInfos, info)
	}
	return
}

func writeModule(outpath, prefix, name, version string, fsys fs.FS) (p pkgInfo, err error) {
	// do we need the modFile? Depends on if the name was given
	if name == "" {
		f, err := fsys.Open(modFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return p, ErrNoModFile{}
			}
			return p, fmt.Errorf("failed to open modfile %s: %v", modFile, err)
		}
		defer f.Close()
		// read the package name
		mod, err := pkg.ParseMod(f)
		if err != nil {
			return p, fmt.Errorf("failed to parse %s: %v", modFile, err)
		}
		name = mod.Name
	}
	// if there was no version given, and we can get .git info, use that to construct the version
	if version == "" {
	}

	// create the outfile
	w, filename, err := getWriter(outpath, prefix, name, version)
	if err != nil {
		return p, fmt.Errorf("failed to create output file %s: %v", outpath, err)
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	defer zw.Close()
	pkgLicenses, err := pkg.WriteToZip(fsys, zw)
	if err != nil {
		return p, fmt.Errorf("failed to write to zip: %v", err)
	}
	p = pkgInfo{Module: name, Version: version, Licenses: pkgLicenses, Path: filename}
	return
}

func getAndWriteModule(outpath, prefix, name, version string) (fsys fs.FS, p pkgInfo, err error) {
	fsys, err = pkg.GetModule(name, version, proxyURL, false)
	if err != nil {
		return fsys, p, fmt.Errorf("failed to get module %s: %v", name, err)
	}
	p, err = writeModule(outpath, prefix, name, version, fsys)
	return
}

// GoVersion calculates the go version to use for the given module.
// Assumes existence of git command on the path.
func GoVersion(dir string) string {
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	// get the most recent tag that matches semver
	var (
		tag    string
		out    bytes.Buffer
		stderr bytes.Buffer
	)
	cmd := exec.Command(git, "-C", dir, "--no-pager", "describe", "--match='v[0-9].[0-9].[0-9]*'", "--abbrev=0", "--tags")
	cmd.Stderr = &stderr
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		log.Warnf("failed to get git tag: %s", stderr.Bytes())
		tag = "v0.0.0"
	} else {
		tag = strings.TrimSpace(out.String())
		if tag == "" {
			tag = "v0.0.0"
		}
	}
	out.Reset()
	stderr.Reset()

	// get number of commits since last tag
	commitList := "HEAD"
	if tag != "v0.0.0" && tag != "" {
		commitList = fmt.Sprintf("%s..HEAD", tag)
	}
	cmd = exec.Command(git, "-C", dir, "rev-list", commitList, "--count")
	cmd.Stderr = &stderr
	cmd.Stdout = &out
	if err = cmd.Run(); err != nil {
		log.Warnf("failed to get git rev-list: %s", stderr.Bytes())
		return ""
	}
	count := strings.TrimSpace(out.String())
	// if the count is 0, just return the tag
	if count == "0" {
		return tag
	}
	out.Reset()
	stderr.Reset()

	cmd = exec.Command(git, "-C", dir, "--no-pager", "show",
		"--quiet",
		"--abbrev=12",
		"--date=format-local:%Y%m%d%H%M%S",
		"--format=%cd-%h")
	cmd.Env = append(cmd.Env, "TZ=LTC")
	cmd.Stderr = &stderr
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		log.Warnf("failed to get git show: %s", stderr.Bytes())
		return ""
	}
	dateAndCommit := strings.TrimSpace(out.String())
	return fmt.Sprintf("%s-%s", tag, dateAndCommit)
}

func parseVersionFromBuildFlags(settings []debug.BuildSetting) (fullVersion string) {
	for _, s := range settings {
		if s.Key != "-ldflags" {
			continue
		}
		ldflags := s.Value
		// parse for -X following by main.version or main.Version
		if ldflags == "" {
			return ""
		}

		for _, pattern := range knownBuildFlagPatterns {
			groups := matchNamedCaptureGroups(pattern, ldflags)
			v, ok := groups["version"]

			if !ok {
				continue
			}

			fullVersion = v
			if !strings.HasPrefix(v, "v") {
				fullVersion = fmt.Sprintf("v%s", v)
			}
			components := strings.Split(v, ".")

			if len(components) == 0 {
				continue
			}

			return
		}
		break
	}
	return
}

// This section below is taken from github.com/anchore/syft and modified. With thanks to their work on it.
// It was released under the Apache 2.0 license.

// devel is used to recognize the current default version when a golang main distribution is built
// https://github.com/golang/go/issues/29228 this issue has more details on the progress of being able to
// inject the correct version into the main module of the build process

var knownBuildFlagPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)\.([gG]it)?([bB]uild)?[vV]ersion=(\S+/)*(?P<version>v?\d+.\d+.\d+[-\w]*)`),
	regexp.MustCompile(`(?m)\.([tT]ag)=(\S+/)*(?P<version>v?\d+.\d+.\d+[-\w]*)`),
}

// matchNamedCaptureGroups takes a regular expression and string and returns all of the named capture group results in a map.
// This is only for the first match in the regex. Callers shouldn't be providing regexes with multiple capture groups with the same name.
func matchNamedCaptureGroups(regEx *regexp.Regexp, content string) map[string]string {
	// note: we are looking across all matches and stopping on the first non-empty match. Why? Take the following example:
	// input: "cool something to match against" pattern: `((?P<name>match) (?P<version>against))?`. Since the pattern is
	// encapsulated in an optional capture group, there will be results for each character, but the results will match
	// on nothing. The only "true" match will be at the end ("match against").
	allMatches := regEx.FindAllStringSubmatch(content, -1)
	var results map[string]string
	for _, match := range allMatches {
		// fill a candidate results map with named capture group results, accepting empty values, but not groups with
		// no names
		for nameIdx, name := range regEx.SubexpNames() {
			if nameIdx > len(match) || len(name) == 0 {
				continue
			}
			if results == nil {
				results = make(map[string]string)
			}
			results[name] = match[nameIdx]
		}
		// note: since we are looking for the first best potential match we should stop when we find the first one
		// with non-empty results.
		if !isEmptyMap(results) {
			break
		}
	}
	return results
}

func isEmptyMap(m map[string]string) bool {
	if len(m) == 0 {
		return true
	}
	for _, value := range m {
		if value != "" {
			return false
		}
	}
	return true
}
