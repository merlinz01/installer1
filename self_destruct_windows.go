package installer1

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

func selfDestructPrepare() {
	if !slices.Contains(os.Args, "--tempuninstaller") {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		tmpDir, err := os.MkdirTemp(os.TempDir(), "uninstall-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmpDir)
		tmpPath := filepath.Join(tmpDir, "uninstaller.exe")
		inputFile, err := os.Open(exePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer inputFile.Close()
		outputFile, err := os.Create(tmpPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer outputFile.Close()
		_, err = io.Copy(outputFile, inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		err = outputFile.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cmd := exec.Command(tmpPath, "--tempuninstaller")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Start()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func selfDestruct() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass",
		"-Command", fmt.Sprintf(`Start-Sleep 5; Remove-Item -Path "%s" -Recurse -Force`, filepath.Dir(exePath)))
	err = cmd.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
