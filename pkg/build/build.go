package build

import (
	"github.com/merlinz01/installer1/pkg/output"
)

type BuildParams struct {
	TargetDir      string
	BuildDir       string
	IgnorePatterns []string
	Compression    string
	OutputFile     string
	OS             string
	Arch           string
}

func Build(b *BuildParams) error {
	output.Info("Starting build...")
	builder := &builder{
		params: b,
	}
	err := builder.build()
	if err != nil {
		return err
	}
	output.Info("Build completed successfully")
	return nil
}
