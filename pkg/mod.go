package pkg

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type ModFile struct {
	Name      string
	GoVersion string
	Requires  []Package
	Replace   map[string]Package
}

func ParseMod(r io.Reader) (*ModFile, error) {
	var m ModFile
	m.Replace = map[string]Package{}
	sc := bufio.NewScanner(r)
	var inRequire, inReplace, inRetract bool

	for sc.Scan() {
		line := sc.Text()
		// ignore comments
		if strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		switch {
		case len(parts) == 0:
			continue
		case parts[0] == "module":
			if m.Name != "" {
				return nil, fmt.Errorf("invalid go.mod: multiple module lines")
			}
			m.Name = parts[1]
		case parts[0] == "go":
			if m.GoVersion != "" {
				return nil, fmt.Errorf("invalid go.mod: multiple go lines")
			}
			m.GoVersion = parts[1]
		case parts[0] == "require":
			if inRequire {
				return nil, fmt.Errorf("invalid go.mod: nested require blocks")
			}
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid go.mod: standalone require on a line")
			}
			if parts[1] == "(" {
				inRequire = true
				continue
			}
			entry, err := requireEntry(parts[1:])
			if err != nil {
				return nil, err
			}
			m.Requires = append(m.Requires, entry)
		case parts[0] == "replace":
			if inReplace {
				return nil, fmt.Errorf("invalid go.mod: nested replace blocks")
			}
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid go.mod: standalone replace on a line")
			}
			if parts[1] == "(" {
				inReplace = true
				continue
			}
			old, replace, err := replaceEntry(parts[1:])
			if err != nil {
				return nil, err
			}
			m.Replace[old.String()] = replace
		case parts[0] == "retract":
			if inRetract {
				return nil, fmt.Errorf("invalid go.mod: nested require blocks")
			}
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid go.mod: standalone retract on a line")
			}
			if parts[1] == "(" {
				inRetract = true
				continue
			}
		case parts[0] == ")":
			switch {
			case inRequire:
				inRequire = false
			case inReplace:
				inReplace = false
			case inRetract:
				inRetract = false
			default:
				return nil, fmt.Errorf("invalid go.mod: unexpected closing paren")
			}
		default:
			// just a regular line
			switch {
			case inRequire:
				entry, err := requireEntry(parts)
				if err != nil {
					return nil, err
				}
				m.Requires = append(m.Requires, entry)
			case inReplace:
				old, replace, err := replaceEntry(parts)
				if err != nil {
					return nil, err
				}
				m.Replace[old.String()] = replace
			case inRetract:
				// just ignore
			default:
				return nil, fmt.Errorf("invalid go.mod: unexpected line")
			}
		}
	}
	return &m, nil
}

func requireEntry(line []string) (p Package, err error) {
	if len(line) < 2 {
		return Package{}, fmt.Errorf("invalid go.mod: standalone require on a line")
	}
	// strip any leading or trailing quotes
	entry := Package{
		Name:    strings.Trim(line[0], `"`),
		Version: line[1],
	}
	if len(line) > 3 && strings.HasSuffix(line[len(line)-1], "indirect") {
		entry.Indirect = true
	}
	return entry, nil
}

func replaceEntry(line []string) (old, new Package, err error) {
	// potential structure of line:
	// module-path [module-version] => replacement-path [replacement-version]
	var (
		preParts, postParts []string
		inPre               = true
	)

	for _, part := range line {
		if part == "=>" {
			inPre = false
			continue
		}
		if inPre {
			preParts = append(preParts, part)
		} else {
			postParts = append(postParts, part)
		}
	}
	if len(preParts) < 1 || len(postParts) < 1 {
		return old, new, fmt.Errorf("invalid go.mod: invalid replace line")
	}
	old = Package{
		Name: strings.Trim(preParts[0], `"`),
	}
	new = Package{
		Name: strings.Trim(postParts[0], `"`),
	}

	if len(preParts) > 1 {
		old.Version = preParts[1]
	}
	if len(postParts) > 1 {
		new.Version = postParts[1]
	}
	return
}
