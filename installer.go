package installer1

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ulikunitz/xz/lzma"
)

type Installer struct {
	embeddedFiles          []byte
	filesMap               map[string]fileInfo
	inDir                  string
	outDir                 string
	uninstallerPath        string
	uninstallerCompression string
}

func NewInstaller(embedFs *embed.FS) *Installer {
	i := &Installer{
		filesMap: make(map[string]fileInfo),
	}
	if embedFs != nil {
		i.updateEmbeddedFiles(embedFs)
	}
	return i
}

func (i *Installer) updateEmbeddedFiles(embedFs *embed.FS) {
	indexJson, err := embedFs.ReadFile("files/index.json")
	if err != nil {
		log.Printf("Failed to read embedded index.json: %v", err)
		return
	}
	type indexEntry struct {
		SourcePath  string `json:"source_path"`
		StoragePath string `json:"storage_path"`
		IsDir       bool   `json:"is_dir"`
		Compression string `json:"compression"`
		Size        int    `json:"size"`
		ContentHash string `json:"content_hash"`
	}
	var index struct {
		Entries         []indexEntry `json:"entries"`
		UninstallerPath string       `json:"uninstaller_path"`
	}
	err = json.Unmarshal(indexJson, &index)
	if err != nil {
		log.Printf("Failed to parse embedded index.json: %v", err)
		return
	}
	for _, entry := range index.Entries {
		if entry.IsDir {
			i.filesMap[entry.SourcePath] = fileInfo{
				sourcePath:  entry.SourcePath,
				isDir:       true,
				content:     nil,
				compression: "",
			}
			continue
		}
		content, err := embedFs.ReadFile("files/" + entry.StoragePath)
		if err != nil {
			log.Printf("Failed to read embedded file %s: %v", entry.StoragePath, err)
			continue
		}
		i.filesMap[entry.SourcePath] = fileInfo{
			sourcePath:  entry.SourcePath,
			isDir:       false,
			content:     content,
			compression: entry.Compression,
		}
	}
	i.uninstallerPath = index.UninstallerPath
}

type fileInfo struct {
	sourcePath  string
	isDir       bool
	content     []byte
	compression string
}

func (f *fileInfo) decompress() error {
	switch f.compression {
	case "":
		return nil
	case "lzma":
		r, err := lzma.NewReader(bytes.NewReader(f.content))
		if err != nil {
			return err
		}
		decompressed, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		f.content = decompressed
	case "bzip2":
		r := bzip2.NewReader(bytes.NewReader(f.content))
		decompressed, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		f.content = decompressed
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(f.content))
		if err != nil {
			return err
		}
		defer r.Close()
		decompressed, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		f.content = decompressed
	default:
		return fmt.Errorf("unsupported compression: %s", f.compression)
	}
	f.compression = ""
	return nil
}

type panicErr struct {
	err error
}

func (e panicErr) Error() string {
	return e.err.Error()
}

func (e panicErr) Unwrap() error {
	return e.err
}

func panicWithErr(err error) {
	panic(panicErr{err: err})
}

// Include the specified file in the installer.
// Useful for where the automatic file detection misses something.
// This is a no-op at runtime, but uses are detected by the build process.
func (i *Installer) IncludeFile(sourcePath string) {}

// Include the specified directory in the installer.
// Useful for where the automatic file detection misses something.
// This is a no-op at runtime, but uses are detected by the build process.
func (i *Installer) IncludeDir(sourcePath string) {}

// Write the specified file to the target system.
// Calls to this function are detected by the build process
// and the specified file is included in the installer.
func (i *Installer) File(sourcePath string, destPath string) {
	sourcePath = filepath.Clean(sourcePath)
	sourcePath = filepath.Join(i.inDir, sourcePath)
	destPath, err := i.getOutPath(destPath)
	if err != nil {
		panicWithErr(err)
	}
	if i.filesMap == nil {
		panicWithErr(errors.New("installer files not initialized"))
	}
	file, ok := i.filesMap[sourcePath]
	if !ok {
		panicWithErr(errors.New("file not included in installer: " + sourcePath))
	}
	if file.isDir {
		panicWithErr(errors.New("path is a directory, not a file: " + sourcePath))
	}
	err = writeFile(destPath, file)
	if err != nil {
		panicWithErr(err)
	}
}

func writeFile(destPath string, file fileInfo) error {
	err := file.decompress()
	if err != nil {
		return err
	}
	destDir := filepath.Dir(destPath)
	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		return err
	}
	err = os.WriteFile(destPath, file.content, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Dir creates the specified directory on the target system
// and copies all included files that are under the specified directory.
// Calls to this function are detected by the build process
// and the specified directory and its contents are included in the installer.
func (i *Installer) Dir(sourcePath string, destPath string) {
	sourcePath = filepath.Clean(sourcePath)
	sourcePath = filepath.Join(i.inDir, sourcePath)
	destPath, err := i.getOutPath(destPath)
	if err != nil {
		panicWithErr(err)
	}
	if i.filesMap == nil {
		panicWithErr(errors.New("installer files not initialized"))
	}
	err = os.MkdirAll(destPath, 0755)
	if err != nil {
		panicWithErr(err)
	}
	for src, file := range i.filesMap {
		if isSubpath(src, sourcePath) {
			relPath, err := filepath.Rel(sourcePath, src)
			if err != nil {
				panicWithErr(err)
			}
			targetPath := filepath.Join(destPath, relPath)
			if file.isDir {
				err = os.MkdirAll(targetPath, 0755)
				if err != nil {
					panicWithErr(err)
				}
			} else {
				err = writeFile(targetPath, file)
				if err != nil {
					panicWithErr(err)
				}
			}
		}
	}
}

func isSubpath(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (i *Installer) getOutPath(destPath string) (string, error) {
	if i.outDir == "" {
		return "", errors.New("output directory not set")
	}
	destPath = filepath.Clean(destPath)
	if filepath.IsAbs(destPath) {
		return destPath, nil
	}
	outPath := filepath.Join(i.outDir, destPath)
	return outPath, nil
}

// Set the output directory for installation.
func (i *Installer) SetOutDir(dir string) {
	dir = filepath.Clean(dir)
	i.outDir = dir
}

// Set the input directory for relative paths.
// This is mostly a no-op at runtime, but uses are detected by the build process.
func (i *Installer) SetInDir(dir string) {
	dir = filepath.Clean(dir)
	i.inDir = dir
}

// Create the specified directory on the target system, including any necessary parents.
func (i *Installer) Mkdir(dir string) {
	dir, err := i.getOutPath(dir)
	if err != nil {
		panicWithErr(err)
	}
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		panicWithErr(err)
	}
}

// Write the uninstaller executable to the target system.
func (i *Installer) Uninstaller(destPath string) {
	destPath, err := i.getOutPath(destPath)
	if err != nil {
		panicWithErr(err)
	}
	file, ok := i.filesMap[i.uninstallerPath]
	if !ok {
		panicWithErr(errors.New("uninstaller not included in installer"))
	}
	if file.isDir {
		panicWithErr(errors.New("uninstaller path is a directory, not a file"))
	}
	err = writeFile(destPath, file)
	if err != nil {
		panicWithErr(err)
	}
}

// Remove the specified file or directory from the target system.
// If the path is a directory, it and all its contents are removed.
func (i *Installer) Remove(path string) {
	path, err := i.getOutPath(path)
	if err != nil {
		panicWithErr(err)
	}
	err = os.RemoveAll(path)
	if err != nil {
		panicWithErr(err)
	}
}

// Exists checks if the specified file or directory exists on the target system.
func (i *Installer) Exists(path string) bool {
	path, err := i.getOutPath(path)
	if err != nil {
		panicWithErr(err)
	}
	_, err = os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// Exec runs the specified command on the target system
// using the system shell and waits for it to complete.
func (i *Installer) ExecShell(command string) {
	args := []string{}
	if runtime.GOOS == "windows" {
		args = []string{"cmd", "/C", command}
	} else {
		args = []string{"sh", "-c", command}
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		panicWithErr(err)
	}
}

// Main entry point for running the installer.
func (i *Installer) run(installFunc func(*Installer) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(panicErr); ok {
				err = pe.err
			} else {
				panic(r)
			}
		}
	}()
	if installFunc == nil {
		return errors.New("install function is nil")
	}
	return installFunc(i)
}

func (i *Installer) InstallMain(installFunc func(*Installer) error) {
	err := i.run(installFunc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func (i *Installer) UninstallMain(uninstallFunc func(*Installer) error) {
	copyUninstaller := true
	argv := os.Args
	for _, arg := range argv {
		if arg == "--nocopyuninstaller" {
			copyUninstaller = false
			break
		}
	}
	if copyUninstaller {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		tmpDir := os.TempDir()
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
		cmd := exec.Command(tmpPath, "--nocopyuninstaller")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Start()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	err := i.run(uninstallFunc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
