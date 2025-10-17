package build

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/dsnet/compress/bzip2"
	"github.com/merlinz01/installer1/pkg/output"
	"github.com/ulikunitz/xz/lzma"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

type builder struct {
	// Build parameters
	params *BuildParams
	// Source file information
	fileset *token.FileSet
	pkg     *packages.Package
	// Paths of included files
	includedFiles []string
	// Paths of included directories
	includedDirs []string
	// Directory where installer source files are generated
	installerDir string
	// Directory where uninstaller source files are generated
	uninstallerDir string
	// Relative path to uninstaller binary
	uninstallerPath string
	// Directory where gathered files are stored
	filesDir string
	// Map of original source path to fileInfo
	filesMap map[string]fileInfo
}

type fileInfo struct {
	// The source path as specified in/relative to the installer method call
	originalSourcePath string
	// The absolute path on disk where the file was found
	absoluteSourcePath string
	// The path where the file is stored in the build files directory
	storagePath string
	// SHA256 hash of the file content
	contentHash string
	// Size of the file in bytes
	size int64
	// Whether the file is a directory
	isDir bool
	// Compression algorithm used, if any
	compression string
}

func (b *builder) build() error {
	err := b.prepare()
	if err != nil {
		return err
	}
	err = b.parseSourceDir()
	if err != nil {
		return err
	}
	err = b.checkInstallUninstall()
	if err != nil {
		return err
	}
	err = b.scanIncludedFiles()
	if err != nil {
		return err
	}
	err = b.compileUninstaller()
	if err != nil {
		return err
	}
	err = b.gatherFiles()
	if err != nil {
		return err
	}
	err = b.pruneUnusedFiles()
	if err != nil {
		return err
	}
	err = b.writeIndexFile()
	if err != nil {
		return err
	}
	err = b.writeInstallerSources()
	if err != nil {
		return err
	}
	err = b.writeUninstallerSources()
	if err != nil {
		return err
	}
	err = b.compileInstaller()
	if err != nil {
		return err
	}
	return nil
}

func (b *builder) prepare() error {
	output.Debug("Validating build params")
	switch b.params.Compression {
	case "lzma", "bzip2", "gzip", "none":
	default:
		return fmt.Errorf("unsupported compression method: %s", b.params.Compression)
	}
	output.Debug("Compression method: %s", b.params.Compression)
	if b.params.TargetDir == "" {
		return errors.New("target dir is required")
	}
	output.Debug("Target dir: %s", b.params.BuildDir)
	if fi, err := os.Stat(b.params.TargetDir); err != nil || !fi.IsDir() {
		return errors.New("target dir does not exist")
	}
	if b.params.BuildDir == "" {
		return errors.New("build dir is required")
	}
	output.Debug("Build dir: %s", b.params.BuildDir)
	_, err := os.Stat(b.params.BuildDir)
	if os.IsNotExist(err) {
		err = os.Mkdir(b.params.BuildDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create build dir: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to stat build dir: %w", err)
	}
	gitignorePath := path.Join(b.params.BuildDir, ".gitignore")
	err = os.WriteFile(gitignorePath, []byte("*\n"), 0644)
	if err != nil {
		return fmt.Errorf("failed to write .gitignore: %w", err)
	}
	output.Debug(".gitignore written: %s", gitignorePath)
	b.installerDir = path.Join(b.params.BuildDir, "installer")
	b.uninstallerDir = path.Join(b.params.BuildDir, "uninstaller")
	b.filesDir = path.Join(b.installerDir, "files")
	return nil
}

func (b *builder) parseSourceDir() error {
	output.Debug("Parsing source files")
	cfg := &packages.Config{
		Mode:  packages.LoadTypes | packages.LoadSyntax | packages.LoadFiles,
		Fset:  token.NewFileSet(),
		Tests: false,
		Dir:   b.params.TargetDir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return fmt.Errorf("failed to load package: %w", err)
	}
	if len(pkgs) == 0 {
		return errors.New("no packages found")
	}
	pkg := pkgs[0]
	if packages.PrintErrors(pkgs) > 0 {
		return errors.New("failed to load package")
	}
	b.fileset = cfg.Fset
	b.pkg = pkg
	output.Debug("Package name: %s", b.pkg.Types.Name())
	output.Debug("Package path: %s", b.pkg.Types.Path())
	return nil
}

func (b *builder) checkInstallUninstall() error {
	scope := b.pkg.Types.Scope()
	installFunc := scope.Lookup("Install")
	if installFunc == nil {
		return errors.New("source file must contain an Install function")
	}
	installFuncType, ok := installFunc.Type().(*types.Signature)
	if !ok {
		return errors.New("Install must be a function")
	}
	if installFuncType.Params().Len() != 1 || installFuncType.Results().Len() != 1 {
		return errors.New("Install must take a single parameter and return a single value")
	}
	if installFuncType.Params().At(0).Type().String() != "*github.com/merlinz01/installer1.Installer" {
		return fmt.Errorf("Install must take a single parameter of type *installer1.Installer, got %s", installFuncType.Params().At(0).Type().String())
	}
	if !types.Identical(installFuncType.Results().At(0).Type(), types.Universe.Lookup("error").Type()) {
		return fmt.Errorf("Install must return an error, got %s", installFuncType.Results().At(0).Type().String())
	}
	uninstallFunc := scope.Lookup("Uninstall")
	if uninstallFunc == nil {
		return errors.New("source file must contain an Uninstall function")
	}
	uninstallFuncType, ok := uninstallFunc.Type().(*types.Signature)
	if !ok {
		return errors.New("Uninstall must be a function")
	}
	if uninstallFuncType.Params().Len() != 1 || uninstallFuncType.Results().Len() != 1 {
		return errors.New("Uninstall must have no parameters and return a single value")
	}
	if uninstallFuncType.Params().At(0).Type().String() != "*github.com/merlinz01/installer1.Installer" {
		return fmt.Errorf("Uninstall must take a single parameter of type *installer1.Installer, got %s", uninstallFuncType.Params().At(0).Type().String())
	}
	if !types.Identical(uninstallFuncType.Results().At(0).Type(), types.Universe.Lookup("error").Type()) {
		return fmt.Errorf("Uninstall must return an error, got %s", uninstallFuncType.Results().At(0).Type().String())
	}
	output.Debug("Source file contains valid Install and Uninstall functions")
	return nil
}

func (b *builder) scanIncludedFiles() error {
	output.Debug("Scanning for included files")
	analyzer := &analysis.Analyzer{
		Run: b.runIncludeScanner,
	}
	pass := &analysis.Pass{
		Fset:      b.fileset,
		Files:     b.pkg.Syntax,
		Pkg:       b.pkg.Types,
		TypesInfo: b.pkg.TypesInfo,
		Report: func(d analysis.Diagnostic) {
			output.Warn("Include scanner: %s", d.Message)
		},
	}
	_, err := analyzer.Run(pass)
	if err != nil {
		return fmt.Errorf("failed to run include scanner: %w", err)
	}
	output.Info("Included files:")
	for _, file := range b.includedFiles {
		output.Info("  %s", file)
	}
	output.Info("Included directories:")
	for _, dir := range b.includedDirs {
		output.Info("  %s", dir)
	}
	return nil
}

func (b *builder) runIncludeScanner(pass *analysis.Pass) (any, error) {
	var inDir string
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			installerIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			obj, ok := pass.TypesInfo.Uses[installerIdent]
			if !ok {
				return true
			}
			installerVar, ok := obj.(*types.Var)
			if !ok {
				return true
			}
			installerPointer, ok := installerVar.Type().(*types.Pointer)
			if !ok {
				return true
			}
			installerNamed, ok := installerPointer.Elem().(*types.Named)
			if !ok {
				return true
			}
			pkgName := installerNamed.Obj().Pkg()
			if pkgName == nil {
				return true
			}
			if pkgName.Path() != "github.com/merlinz01/installer1" {
				return true
			}
			if sel.Sel.Name != "IncludeFile" && sel.Sel.Name != "IncludeDir" && sel.Sel.Name != "File" && sel.Sel.Name != "Dir" && sel.Sel.Name != "SetInDir" {
				return true
			}
			if len(call.Args) < 1 {
				output.Warn("File/Dir/IncludeFile/IncludeDir/SetInDir must have a single argument")
				return true
			}
			arg, ok := call.Args[0].(*ast.BasicLit)
			if !ok || arg.Kind != token.STRING {
				output.Warn("File/Dir/IncludeFile/IncludeDir/SetInDir first argument must be a string literal")
				return true
			}
			pathValue, err := parseStringLiteral(arg.Value)
			if err != nil {
				output.Warn("Failed to parse source path argument: %v", err)
				return true
			}
			switch sel.Sel.Name {
			case "IncludeFile", "File":
				b.includedFiles = append(b.includedFiles, path.Join(inDir, pathValue))
			case "IncludeDir", "Dir":
				b.includedDirs = append(b.includedDirs, path.Join(inDir, pathValue))
			case "SetInDir":
				inDir = filepath.Clean(pathValue)
			}
			return true
		})
	}
	return nil, nil
}

func parseStringLiteral(lit string) (string, error) {
	if len(lit) < 2 {
		return "", errors.New("invalid string literal")
	}
	return lit[1 : len(lit)-1], nil
}

func (b *builder) gatherFiles() error {
	output.Debug("Gathering included files")
	err := os.MkdirAll(b.filesDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create files dir: %w", err)
	}
	b.filesMap = make(map[string]fileInfo)
	for _, filePath := range b.includedFiles {
		err := b.addFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to add included file %s: %w", filePath, err)
		}
	}
	for _, dirPath := range b.includedDirs {
		err := b.addDir(dirPath)
		if err != nil {
			return fmt.Errorf("failed to add included directory %s: %w", dirPath, err)
		}
	}
	uninstallerPath, err := filepath.Rel(b.params.TargetDir, path.Join(b.uninstallerDir, "uninstaller"))
	if err != nil {
		return fmt.Errorf("failed to get uninstaller path: %w", err)
	}
	b.addFile(uninstallerPath)
	b.uninstallerPath = uninstallerPath
	output.Info("Total files gathered: %d", len(b.filesMap))
	return nil
}

func (b *builder) addFile(sourcePath string) error {
	absPath := sourcePath
	if !filepath.IsAbs(sourcePath) {
		absPath = path.Join(b.params.TargetDir, sourcePath)
	}
	absPath = path.Clean(absPath)
	stat, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	if stat.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", sourcePath)
	}
	if _, exists := b.filesMap[sourcePath]; exists {
		return nil
	}
	hash := sha256.New()
	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			_, err := hash.Write(buf[:n])
			if err != nil {
				return fmt.Errorf("failed to hash file: %w", err)
			}
		}
		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("failed to read file: %w", err)
		}
	}
	sum := hash.Sum(nil)
	output.Debug("Included file: %s", sourcePath)
	fileInfo := fileInfo{
		originalSourcePath: sourcePath,
		absoluteSourcePath: absPath,
		contentHash:        fmt.Sprintf("%x", sum),
		isDir:              false,
		size:               stat.Size(),
		compression:        b.params.Compression,
	}
	fileInfo.storagePath = path.Join(b.filesDir, fileInfo.contentHash+"-"+b.params.Compression)
	if stat, err := os.Stat(fileInfo.storagePath); err == nil && !stat.IsDir() {
		output.Debug("File already exists in cache: %s", sourcePath)
		b.filesMap[sourcePath] = fileInfo
		return nil
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}
	out, err := os.Create(fileInfo.storagePath)
	if err != nil {
		return fmt.Errorf("failed to create storage file: %w", err)
	}
	defer out.Close()
	var reader io.Reader = f
	switch b.params.Compression {
	case "lzma":
		lw, err := lzma.NewWriter(out)
		if err != nil {
			return fmt.Errorf("failed to create lzma writer: %w", err)
		}
		defer lw.Close()
		_, err = io.CopyBuffer(lw, reader, buf)
		if err != nil {
			return fmt.Errorf("failed to copy file to storage: %w", err)
		}
		err = lw.Close()
		if err != nil {
			return fmt.Errorf("failed to finalize lzma compression: %w", err)
		}
	case "bzip2":
		bw, err := bzip2.NewWriter(out, &bzip2.WriterConfig{Level: bzip2.BestCompression})
		if err != nil {
			return fmt.Errorf("failed to create bzip2 writer: %w", err)
		}
		defer bw.Close()
		_, err = io.CopyBuffer(bw, reader, buf)
		if err != nil {
			return fmt.Errorf("failed to copy file to storage: %w", err)
		}
		err = bw.Close()
		if err != nil {
			return fmt.Errorf("failed to finalize bzip2 compression: %w", err)
		}
	case "gzip":
		gw := gzip.NewWriter(out)
		defer gw.Close()
		_, err = io.CopyBuffer(gw, reader, buf)
		if err != nil {
			return fmt.Errorf("failed to copy file to storage: %w", err)
		}
		err = gw.Close()
		if err != nil {
			return fmt.Errorf("failed to finalize gzip compression: %w", err)
		}
	case "none":
		_, err = io.CopyBuffer(out, reader, buf)
		if err != nil {
			return fmt.Errorf("failed to copy file to storage: %w", err)
		}
	default:
		return fmt.Errorf("unsupported compression method: %s", b.params.Compression)
	}
	b.filesMap[sourcePath] = fileInfo
	return nil
}

func (b *builder) addDir(sourcePath string) error {
	absPath := sourcePath
	if !filepath.IsAbs(sourcePath) {
		absPath = path.Join(b.params.TargetDir, sourcePath)
	}
	absPath = path.Clean(absPath)
	stat, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat directory: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", sourcePath)
	}
	if _, exists := b.filesMap[sourcePath]; exists {
		return nil
	}
	fileInfo := fileInfo{
		originalSourcePath: sourcePath,
		absoluteSourcePath: absPath,
		isDir:              true,
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}
	for _, entry := range entries {
		if b.matchesIgnorePatterns(entry.Name()) {
			output.Debug("Ignoring path due to ignore patterns: %s", entry.Name())
			continue
		}
		entryPath := path.Join(sourcePath, entry.Name())
		if entry.IsDir() {
			err := b.addDir(entryPath)
			if err != nil {
				return err
			}
		} else {
			err := b.addFile(entryPath)
			if err != nil {
				return err
			}
		}
	}
	b.filesMap[sourcePath] = fileInfo
	return nil
}

func (b *builder) matchesIgnorePatterns(p string) bool {
	for _, pattern := range b.params.IgnorePatterns {
		matched, err := path.Match(pattern, p)
		if err != nil {
			output.Warn("Invalid ignore pattern %s: %v", pattern, err)
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

func (b *builder) pruneUnusedFiles() error {
	output.Debug("Pruning unused files from build directory")
	entries, err := os.ReadDir(b.filesDir)
	if err != nil {
		return fmt.Errorf("failed to read files dir: %w", err)
	}
	usedFiles := make(map[string]struct{})
	for _, info := range b.filesMap {
		if !info.isDir {
			usedFiles[info.storagePath] = struct{}{}
		}
	}
	for _, entry := range entries {
		entryPath := path.Join(b.filesDir, entry.Name())
		if _, used := usedFiles[entryPath]; !used {
			err := os.Remove(entryPath)
			if err != nil {
				return fmt.Errorf("failed to remove unused file %s: %w", entryPath, err)
			}
			output.Debug("Removed unused file: %s", entryPath)
		}
	}
	return nil
}

func (b *builder) writeIndexFile() error {
	output.Debug("Writing index file")
	indexPath := path.Join(b.filesDir, "index.json")
	f, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("failed to create index file: %w", err)
	}
	defer f.Close()
	type indexEntry struct {
		OriginalSourcePath string `json:"source_path"`
		StoragePath        string `json:"storage_path"`
		Compression        string `json:"compression"`
		IsDir              bool   `json:"is_dir"`
		Size               int64  `json:"size"`
		ContentHash        string `json:"content_hash"`
	}
	entries := make([]indexEntry, 0, len(b.filesMap))
	for _, info := range b.filesMap {
		relStoragePath := ""
		if !info.isDir {
			relStoragePath, err = filepath.Rel(b.filesDir, info.storagePath)
			if err != nil {
				return fmt.Errorf("failed to get relative path for file %s: %w", info.storagePath, err)
			}
		}
		entries = append(entries, indexEntry{
			OriginalSourcePath: info.originalSourcePath,
			StoragePath:        relStoragePath,
			Compression:        info.compression,
			IsDir:              info.isDir,
			Size:               info.size,
			ContentHash:        info.contentHash,
		})
	}
	index := struct {
		Files           []indexEntry `json:"files"`
		UninstallerPath string       `json:"uninstaller_path"`
	}{
		Files:           entries,
		UninstallerPath: b.uninstallerPath,
	}
	encoder := json.NewEncoder(f)
	err = encoder.Encode(index)
	if err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}
	output.Debug("Index file written: %s", indexPath)
	return nil
}

func (b *builder) writeInstallerSources() error {
	output.Debug("Writing installer source files")
	err := os.MkdirAll(b.installerDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create installer dir: %w", err)
	}
	installerSource := []byte(`// Code generated by installer1; DO NOT EDIT.
package main

import (
	"github.com/merlinz01/installer1"
	"embed"
)

//go:embed files
var _installer_embedded_files embed.FS

func main() {
	i := installer1.NewInstaller(&_installer_embedded_files)
	i.Main(Install)
}
`)
	mainPath := path.Join(b.installerDir, "main.go")
	err = os.WriteFile(mainPath, installerSource, 0644)
	if err != nil {
		return fmt.Errorf("failed to write installer main file: %w", err)
	}
	output.Debug("Installer main file written: %s", mainPath)
	sourceFilePath := ""
	for _, f := range b.pkg.GoFiles {
		if strings.HasSuffix(f, ".go") {
			sourceFilePath = f
			break
		}
	}
	if sourceFilePath == "" {
		return errors.New("failed to find source .go file")
	}
	sourceContent, err := os.ReadFile(sourceFilePath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}
	destSourcePath := path.Join(b.installerDir, path.Base(sourceFilePath))
	err = os.WriteFile(destSourcePath, sourceContent, 0644)
	if err != nil {
		return fmt.Errorf("failed to write source file to installer dir: %w", err)
	}
	output.Debug("Source file copied to installer dir: %s", destSourcePath)
	return nil
}

func (b *builder) compileInstaller() error {
	output.Debug("Compiling installer binary")
	outputPath := b.params.OutputFile
	cmd := []string{"go", "build", "-o", outputPath, "-ldflags", "-s -w", b.installerDir}
	output.Debug("Running command: %s", strings.Join(cmd, " "))
	env := []string{}
	if b.params.OS != "" {
		env = append(env, "GOOS="+b.params.OS)
		output.Debug("Setting GOOS=%s", b.params.OS)
	}
	if b.params.Arch != "" {
		env = append(env, "GOARCH="+b.params.Arch)
		output.Debug("Setting GOARCH=%s", b.params.Arch)
	}
	err := runCommand(cmd, env)
	if err != nil {
		return fmt.Errorf("failed to compile installer: %w", err)
	}
	output.Info("Installer binary created: %s", outputPath)
	return nil
}

func runCommand(cmd []string, env []string) error {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Env = append(os.Environ(), env...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func (b *builder) writeUninstallerSources() error {
	output.Debug("Writing uninstaller source files")
	err := os.MkdirAll(b.uninstallerDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create uninstaller dir: %w", err)
	}
	uninstallerSource := []byte(`// Code generated by installer1; DO NOT EDIT.
package main

import (
	"github.com/merlinz01/installer1"
)

func main() {
	u := installer1.NewInstaller(nil)
	u.Main(Uninstall)
}
`)
	mainPath := path.Join(b.uninstallerDir, "main.go")
	err = os.WriteFile(mainPath, uninstallerSource, 0644)
	if err != nil {
		return fmt.Errorf("failed to write uninstaller main file: %w", err)
	}
	output.Debug("Uninstaller main file written: %s", mainPath)
	sourceFilePath := ""
	for _, f := range b.pkg.GoFiles {
		if strings.HasSuffix(f, ".go") {
			sourceFilePath = f
			break
		}
	}
	if sourceFilePath == "" {
		return errors.New("failed to find source .go file")
	}
	sourceContent, err := os.ReadFile(sourceFilePath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}
	destSourcePath := path.Join(b.uninstallerDir, path.Base(sourceFilePath))
	err = os.WriteFile(destSourcePath, sourceContent, 0644)
	if err != nil {
		return fmt.Errorf("failed to write source file to uninstaller dir: %w", err)
	}
	output.Debug("Source file copied to uninstaller dir: %s", destSourcePath)
	return nil
}

func (b *builder) compileUninstaller() error {
	output.Debug("Compiling uninstaller binary")
	outputPath := path.Join(b.uninstallerDir, "uninstaller")
	cmd := []string{"go", "build", "-o", outputPath, "-ldflags", "-s -w", b.uninstallerDir}
	output.Debug("Running command: %s", strings.Join(cmd, " "))
	env := []string{}
	if b.params.OS != "" {
		env = append(env, "GOOS="+b.params.OS)
		output.Debug("Setting GOOS=%s", b.params.OS)
	}
	if b.params.Arch != "" {
		env = append(env, "GOARCH="+b.params.Arch)
		output.Debug("Setting GOARCH=%s", b.params.Arch)
	}
	err := runCommand(cmd, env)
	if err != nil {
		return fmt.Errorf("failed to compile uninstaller: %w", err)
	}
	output.Info("Uninstaller binary created: %s", outputPath)
	return nil
}
