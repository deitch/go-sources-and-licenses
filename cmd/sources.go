package cmd

import (
	"archive/zip"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

func sources() *cobra.Command {
	var (
		version, outpath, format, prefix     string
		recursive, find, module, src, binary bool
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
				log.Debugf("writing module %s version %s from direct package", moduleName, version)
				added, err := writeModuleFromSource(outpath, prefix, moduleName, version, fsys, recursive)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case src && !find:
				fsys = os.DirFS(target)
				log.Debugf("writing module from source directory %s", target)
				added, err := writeModuleFromSource(outpath, prefix, "", version, fsys, recursive)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case src && find:
				log.Debugf("find for source enabled based at %s", target)
				fsys = os.DirFS(target)
				fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil && !errors.Is(err, io.EOF) {
						return fmt.Errorf("failed to walk %s: %v", path, err)
					}
					// we only are looking for directories with go.mod in them
					if !strings.HasSuffix(path, modFile) {
						return nil
					}
					sub, err := fs.Sub(fsys, filepath.Dir(path))
					if err != nil {
						return fmt.Errorf("failed to get subdirectory %s: %v", path, err)
					}
					log.Debugf("writing module %s version %s from inside directory %s", moduleName, version, path)
					added, err := writeModuleFromSource(outpath, prefix, "", version, sub, recursive)
					if err != nil {
						return err
					}
					pkgInfos = append(pkgInfos, added...)
					return nil
				})
			case binary && !find:
				log.Debugf("writing info from binary  %s", target)
				f, err := os.Open(target)
				if err != nil {
					return fmt.Errorf("failed to open %s: %v", target, err)
				}
				defer f.Close()
				added, err := writeModuleFromBinary(outpath, prefix, f, recursive)
				if err != nil {
					return err
				}
				pkgInfos = append(pkgInfos, added...)
			case binary && find:
				log.Debugf("find for go binaries enabled based at %s", target)
				fsys = os.DirFS(target)
				fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil && !errors.Is(err, io.EOF) {
						return fmt.Errorf("failed to walk %s: %v", path, err)
					}
					// we only care about regular files
					if d.IsDir() {
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
					added, err := writeModuleFromBinary(outpath, prefix, fra, recursive)
					if err != nil {
						return err
					}
					pkgInfos = append(pkgInfos, added...)
					return nil
				})
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
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "recurse into subpackages")
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
		f, err := os.Create(outFile)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create output file %s: %v", outFile, err)
		}
		w = f
	}

	return w, filename, nil
}

func writeModuleFromSource(outpath, prefix, name, version string, fsys fs.FS, recursive bool) (pkgInfos []pkgInfo, err error) {
	info, err := writeModule(outpath, prefix, name, version, fsys)
	if err != nil {
		return nil, fmt.Errorf("failed to get package %s@%s: %w", name, version, err)
	}
	pkgInfos = append(pkgInfos, info)

	if recursive {
		f, err := fsys.Open(modFile)
		if err != nil {
			log.Debugf("failed to open mod file %s@%s %s: %v", info.Path, info.Version, modFile, err)
		} else {
			defer f.Close()
			mod, err := pkg.ParseMod(f)
			if err != nil {
				return nil, fmt.Errorf("failed to parse mod file %s@%s %s: %v", info.Path, info.Version, modFile, err)
			}
			for _, p := range mod.Requires {
				fsys, err := pkg.GetModule(p.Name, p.Version, proxyURL, false)
				if err != nil && !errors.Is(err, ErrNoModFile{}) {
					return nil, fmt.Errorf("failed to get package %s@%s: %v", p.Name, p.Version, err)
				}
				pkgs, err := writeModuleFromSource(outpath, prefix, p.Name, p.Version, fsys, recursive)
				if err != nil {
					if errors.Is(err, ErrNoModFile{}) {
						continue
					}
					return nil, fmt.Errorf("failed to get package %s@%s: %v", p.Name, p.Version, err)
				}
				pkgInfos = append(pkgInfos, pkgs...)
			}
		}
	}
	return
}

func writeModuleFromBinary(outpath, prefix string, r io.ReaderAt, recursive bool) (pkgInfos []pkgInfo, err error) {
	info, err := buildinfo.Read(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read build info: %v", err)
	}
	name, version := info.Main.Path, info.Main.Version
	if version != "" && version != "(devel)" {
		_, info, err := getAndWriteModule(outpath, prefix, name, version)
		if err != nil {
			return nil, fmt.Errorf("failed to get package %s@%s: %v", name, version, err)
		}
		pkgInfos = append(pkgInfos, info)
	}

	for _, d := range info.Deps {
		if d.Version == "" || d.Version == "(devel)" {
			continue
		}
		fsys, info, err := getAndWriteModule(outpath, prefix, d.Path, d.Version)
		if err != nil {
			if errors.Is(err, ErrNoModFile{}) {
				continue
			}
			return nil, fmt.Errorf("failed to get package %s@%s: %v", d.Path, d.Version, err)
		}
		pkgInfos = append(pkgInfos, info)

		if recursive {
			f, err := fsys.Open(modFile)
			if err != nil {
				log.Debugf("failed to open mod file %s@%s %s: %v", info.Path, info.Version, modFile, err)
			} else {
				defer f.Close()
				mod, err := pkg.ParseMod(f)
				if err != nil {
					return nil, fmt.Errorf("failed to parse mod file %s@%s %s: %v", info.Path, info.Version, modFile, err)
				}
				for _, p := range mod.Requires {
					fsys, err := pkg.GetModule(p.Name, p.Version, proxyURL, false)
					if err != nil {
						return nil, fmt.Errorf("failed to get package %s@%s: %v", p.Name, p.Version, err)
					}
					pkgs, err := writeModuleFromSource(outpath, prefix, p.Name, p.Version, fsys, recursive)
					if err != nil {
						return nil, fmt.Errorf("failed to get package %s@%s: %v", p.Name, p.Version, err)
					}
					pkgInfos = append(pkgInfos, pkgs...)
				}
			}
		}
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
