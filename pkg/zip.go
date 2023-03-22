package pkg

import (
	"archive/zip"
	"io"
	"io/fs"
	"strings"
)

func WriteToZip(fsys fs.FS, zw *zip.Writer) ([]string, error) {
	licenseListers, err := writeToZip(fsys, zw)
	if err != nil {
		return nil, err
	}
	var licenses []string
	for _, r := range licenseListers {
		if l, ok := r.(*licenseReader); ok {
			licenses = append(licenses, l.licenses...)
		}
	}
	return licenses, nil

}
func writeToZip(fsys fs.FS, zw *zip.Writer) ([]io.ReadCloser, error) {
	var licenseListers []io.ReadCloser
	// is our fs a zip reader in the first place?
	if tr, ok := fsys.(*zip.Reader); ok {
		// just copy it all over
		for _, f := range tr.File {
			w, err := zw.CreateHeader(&f.FileHeader)
			if err != nil {
				return nil, err
			}
			// nothing more to do with directories
			if strings.HasSuffix(f.FileHeader.Name, "/") {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			reader := licenseChecker(r, f.Name)
			licenseListers = append(licenseListers, reader)
			defer reader.Close()
			_, err = io.Copy(w, reader)
			if err != nil {
				return nil, err
			}
		}
	} else {
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
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
				// ignore them
				return nil
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
				reader := licenseChecker(r, path)
				licenseListers = append(licenseListers, reader)
				defer reader.Close()
				_, err = io.Copy(w, reader)
				return err
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return licenseListers, nil
}
