package pkg

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

func WriteToZip(fsys fs.FS, zw *zip.Writer) error {
	// is our fs a zip reader in the first place?
	if tr, ok := fsys.(*zip.Reader); ok {
		// just copy it all over
		for _, f := range tr.File {
			w, err := zw.CreateHeader(&f.FileHeader)
			if err != nil {
				return err
			}
			// nothing more to do with directories
			if strings.HasSuffix(f.FileHeader.Name, "/") {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return err
			}
			defer r.Close()
			_, err = io.Copy(w, r)
			if err != nil {
				return err
			}
		}
	} else {
		return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// ignore git directory
			if path == ".git" || strings.HasPrefix(path, ".git/") {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := zip.FileInfoHeader(fi)
			if err != nil {
				return err
			}
			switch {
			case d.IsDir():
				if !strings.HasSuffix(hdr.Name, "/") {
					hdr.Name += "/"
				}
				_, err := zw.CreateHeader(hdr)
				return err
			case d.Type() == fs.ModeSymlink:
				return fmt.Errorf("symlinks not supported for %s", path)
			default:
				w, err := zw.CreateHeader(hdr)
				if err != nil {
					return err
				}
				r, err := fsys.Open(path)
				if err != nil {
					return err
				}
				defer r.Close()
				_, err = io.Copy(w, r)
				return err
			}
		})
	}
	return nil
}
