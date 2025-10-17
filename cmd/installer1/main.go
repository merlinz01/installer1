package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/merlinz01/installer1/pkg/build"
	"github.com/merlinz01/installer1/pkg/output"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var err error
	var verbose bool
	flag.BoolVar(&verbose, "verbose", false, "Enable debug output")
	var quiet bool
	flag.BoolVar(&quiet, "quiet", false, "Suppress non-error output")
	var silent bool
	flag.BoolVar(&silent, "silent", false, "Suppress all output")
	var logFile string
	flag.StringVar(&logFile, "log", "", "Log file (default: stderr)")
	var targetDir string
	flag.StringVar(&targetDir, "target", "", "Target directory (default: current working directory)")
	var ignorePatterns string
	flag.StringVar(&ignorePatterns, "ignore", ".git,.svn,node_modules", "Comma-separated list of glob patterns to ignore when including directories")
	var compression string
	flag.StringVar(&compression, "compression", "lzma", "Compression method to use for files (options: lzma, bzip2, gzip, none)")
	var out string
	flag.StringVar(&out, "output", "./installer", "Output installer file name")
	var osName string
	flag.StringVar(&osName, "os", "", "Target operating system (default: current OS)")
	var arch string
	flag.StringVar(&arch, "arch", "", "Target architecture (default: current architecture)")
	flag.Parse()
	if targetDir == "" {
		targetDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}
	}
	targetDir, err = filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		defer f.Close()
		output.SetOutputFile(f)
	}
	if verbose {
		output.SetDebugLevel(output.LevelDebug)
	} else if quiet {
		output.SetDebugLevel(output.LevelError)
	} else if silent {
		output.SetDebugLevel(output.LevelNone)
	} else {
		output.SetDebugLevel(output.LevelInfo)
	}
	var ignore []string
	if ignorePatterns != "" {
		for pattern := range strings.SplitSeq(ignorePatterns, ",") {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				ignore = append(ignore, pattern)
			}
		}
	}
	b := &build.BuildParams{
		TargetDir:      targetDir,
		BuildDir:       path.Join(targetDir, "build"),
		IgnorePatterns: ignore,
		Compression:    compression,
		OutputFile:     out,
		OS:             osName,
		Arch:           arch,
	}
	err = build.Build(b)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
}
