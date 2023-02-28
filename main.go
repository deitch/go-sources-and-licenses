package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/licensecheck"
)

const (
	proxyURL           = "https://proxy.golang.org"
	coverageThreshold  = 75
	unknownLicenseType = "UNKNOWN"
)

/*
How this works:
1. Get the URL via proxy to download the zip of a module
2. Pass the zip to the license parser
3. Find all license files in the zip
4. Parse for licenses
*/
func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: licenses <module> <version>")
		os.Exit(1)
	}
	module := os.Args[1]
	// must be a URL or ignore
	if !strings.Contains(module, ".") {
		log.Fatalf("module must be a URL, do not support built in modules %s", module)
	}
	version := os.Args[2]
	if version == "latest" {
		log.Printf("getting latest version of %s", module)
		versions, err := getVersions(module, proxyURL)
		if err != nil {
			log.Fatalf("failed to get versions: %v", err)
		}
		version = versions[len(versions)-1]
		log.Printf("version is %s", version)
	}
	fsys, err := getModule(module, version, proxyURL)
	if err != nil {
		log.Fatalf("failed to get module: %v", err)
	}
	licenses := findLicenses(module, fsys)
	fmt.Println(licenses)
}

func getModule(module, version, proxy string) (fs.FS, error) {
	// first see if we have it locally
	goPath := os.Getenv("GOPATH")
	if goPath != "" {
		modPath := filepath.Join(goPath, "pkg", "mod", fmt.Sprintf("%s@%s", module, version))
		if fi, err := os.Stat(modPath); err == nil && fi != nil && fi.IsDir() {
			log.Printf("found module locally at %s", modPath)
			modFS := os.DirFS(modPath)
			return modFS, nil
		}
	}

	// we could not get it locally, so get it from the proxy

	// get the module zip
	resp, err := http.Get(fmt.Sprintf("%s/%s/@v/%s.zip", proxy, module, version))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get module zip: %s", resp.Status)
	}
	// read the zip
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	log.Print("found module via proxy")
	return zip.NewReader(bytes.NewReader(b), resp.ContentLength)
}

func getVersions(module, proxy string) ([]string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/%s/@v/list", proxy, module))
	if err != nil {
		return nil, err
	}
	var versions []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		versions = append(versions, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return versions, nil

}

func findLicenses(module string, fsys fs.FS) []string {
	var (
		licenses []string
		isVendor bool
	)
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		filename := filepath.Base(p)
		// ignore any tat are not a known filetype
		if _, ok := fileNames[filename]; !ok {
			return nil
		}
		// make sure it is not in a vendored path
		parts := strings.Split(filepath.Dir(p), string(filepath.Separator))
		for _, part := range parts {
			if part == "vendor" {
				isVendor = true
				break
			}
		}
		if isVendor {
			return nil
		}
		// read the file contents
		rc, err := fsys.Open(p)
		if err != nil {
			return nil
		}
		defer rc.Close()
		contents, err := io.ReadAll(rc)
		if err != nil {
			return nil
		}
		cov := licensecheck.Scan(contents)

		if cov.Percent < float64(coverageThreshold) {
			licenses = append(licenses, unknownLicenseType)
		}
		for _, m := range cov.Match {
			licenses = append(licenses, m.ID)
		}
		return nil
	})
	return licenses
}
