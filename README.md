# Installer1

A Go-based framework for building self-contained installers.

Very early development stage - not ready for production use.

Why? Because NSIS is a pain for anything non-trivial.
We can do better in 2025.

## Features

- Write your installer logic in good old plain Go
- Full access to Go's standard library and third-party packages
- Scans your Go source code file to detect which files need to be included
- Embeds those files into a self-contained executable using Go's embed feature
- Provides a simple API for installing and removing files on the target system
- Automatically generates both installer and uninstaller binaries
- Supports file compression to reduce binary size
- Works cross-platform (in theory)

## Unfeatures

- No GUI (yet)
- No advanced installation options (yet)
- No control over exe icons, version info, etc. (yet)
- Only supports operating systems that Go supports
- Installer binaries are larger than NSIS installers due to the embedded Go runtime

## Usage

TODO - see the `example` directory for usage.

## Command Line Options

The `installer1` build tool supports the following options:

```sh
-target <dir>         Directory containing installer script (default: current directory)
-output <file>        Output installer file name (default: ./installer)
-compression <type>   Compression method: gzip, none (default: gzip)
-ignore <patterns>    Comma-separated glob patterns to ignore (default: .git,.svn,node_modules)
-os <name>            Target OS for cross-compilation (default: current OS)
-arch <name>          Target architecture (default: current architecture)
-verbose              Enable debug output
-quiet                Suppress non-error output
-silent               Suppress all output
-log <file>           Log file path (default: stderr)
```

## Requirements

- Go 1.25 or later
- A Go source file that uses the Installer1 API

## License

This project is licensed under the MIT License.
See the [LICENSE.txt](LICENSE.txt) file for details.

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.
