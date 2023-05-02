package cmd

type ErrNoModFile struct{}

func (e ErrNoModFile) Error() string {
	return "no go.mod file found"
}
