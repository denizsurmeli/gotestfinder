# Go Test Finder (Rust Version)

A Rust rewrite of the Go test finder tool using [skim](https://github.com/skim-rs/skim) for fuzzy selection.

## Features

- **Fast test discovery**: Uses regex to parse Go test files and find test functions and subtests
- **Built-in fuzzy finder**: Uses skim library (no external dependencies)
- **Multi-selection**: Select multiple tests with Tab key
- **Direct execution**: Automatically runs `go test` with selected patterns
- **Build tags support**: Pass build tags to go test
- **Single binary**: No external dependencies required

## Installation

```bash
cd rust-version
cargo build --release
```

The binary will be available at `target/release/gotestfinder`.

## Usage

### Basic usage (print test patterns)
```bash
gotestfinder /path/to/go/project
```

### Interactive mode with skim
```bash
gotestfinder --fzf /path/to/go/project
```

### With build tags
```bash
gotestfinder --fzf --tags integration /path/to/go/project
```

### With verbose output
```bash
gotestfinder --fzf --verbose /path/to/go/project
```

### Combined options
```bash
gotestfinder --fzf --verbose --tags integration /path/to/go/project
```

### Options
- `--fzf`: Enable interactive fuzzy selection mode
- `--tags <TAGS>`: Build tags to pass to go test
- `-v, --verbose`: Enable verbose output (adds -v flag to go test)
- `--subtests <true|false>`: Show individual subtests (default: true)
- `--parent <true|false>`: Show parent test patterns (default: true)

## Interactive Mode

In interactive mode:
- **Arrow keys / Ctrl+j/k**: Navigate
- **Tab**: Select/deselect multiple tests (multi-selection)
- **Enter**: Run selected tests with go test
- **Ctrl+c / Esc**: Cancel selection
- **Ctrl+a**: Select all
- **Ctrl+d**: Deselect all

**Multi-selection**: Use Tab key to toggle selection on individual tests. Selected tests will be highlighted. Press Enter to run all selected tests together.

## Advantages over Go version

1. **No external dependencies**: Skim is built-in, no need to install fzf
2. **Single binary**: Easy distribution
3. **Better performance**: Rust's speed for file parsing
4. **Memory safety**: Rust's memory safety guarantees
5. **Cross-platform**: Easier compilation for different platforms

## Dependencies

- `skim`: Fuzzy finder library
- `clap`: Command line parsing
- `walkdir`: Directory traversal
- `regex`: Pattern matching
- `anyhow`: Error handling