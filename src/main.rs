use anyhow::Result;
use clap::Parser;
use regex::Regex;
use skim::prelude::*;
use std::io::Cursor;
use std::io::{self, Write};
use std::path::Path;
use std::process::Command;
use walkdir::WalkDir;

#[derive(Parser)]
#[command(name = "gotestfinder")]
#[command(about = "Find and run Go tests with fuzzy selection")]
struct Args {
    /// Directory to search for tests
    directory: String,

    /// Show individual subtests
    #[arg(long, default_value = "true")]
    subtests: bool,

    /// Show parent test patterns
    #[arg(long, default_value = "true")]
    parent: bool,

    /// Use skim for interactive test selection and execution
    #[arg(long)]
    fzf: bool,

    /// Build tags to pass to go test
    #[arg(long)]
    tags: Option<String>,

    /// Enable verbose output (-v flag for go test)
    #[arg(short, long)]
    verbose: bool,
}

#[derive(Debug, Clone)]
struct TestInfo {
    name: String,
    #[allow(dead_code)]
    file: String,
    #[allow(dead_code)]
    line: usize,
    subtests: Vec<String>,
}

fn main() -> Result<()> {
    let args = Args::parse();

    let tests = find_tests(&args.directory)?;

    if args.fzf {
        run_with_skim(tests, args.tags, args.verbose)?;
    } else {
        print_tests(&tests, args.subtests, args.parent);
    }

    Ok(())
}

fn find_tests(dir: &str) -> Result<Vec<TestInfo>> {
    let mut tests = Vec::new();

    for entry in WalkDir::new(dir) {
        let entry = entry?;
        let path = entry.path();

        if path.extension().is_some_and(|ext| ext == "go")
            && path
                .file_name()
                .is_some_and(|name| name.to_string_lossy().ends_with("_test.go"))
        {
            tests.extend(parse_test_file(path)?);
        }
    }

    Ok(tests)
}

fn parse_test_file(path: &Path) -> Result<Vec<TestInfo>> {
    let content = std::fs::read_to_string(path)?;
    let mut tests = Vec::new();

    let test_func_regex = Regex::new(r"func\s+(Test\w+)\s*\([^)]*\*testing\.[TB]\w*\)")?;
    let subtest_regex = Regex::new(r#"\.Run\s*\(\s*"([^"]+)""#)?;

    let lines: Vec<&str> = content.lines().collect();

    for (line_num, line) in lines.iter().enumerate() {
        if let Some(caps) = test_func_regex.captures(line) {
            let test_name = caps.get(1).unwrap().as_str().to_string();
            let mut subtests = Vec::new();

            let mut brace_count = 0;
            let mut in_function = false;

            for (_, &func_line) in lines.iter().enumerate().skip(line_num) {
                if func_line.contains('{') {
                    brace_count += func_line.matches('{').count();
                    in_function = true;
                }
                if func_line.contains('}') {
                    brace_count = brace_count.saturating_sub(func_line.matches('}').count());
                }

                if in_function && brace_count == 0 {
                    break;
                }

                if in_function {
                    for caps in subtest_regex.captures_iter(func_line) {
                        if let Some(subtest_name) = caps.get(1) {
                            subtests.push(subtest_name.as_str().to_string());
                        }
                    }
                }
            }

            tests.push(TestInfo {
                name: test_name,
                file: path.to_string_lossy().to_string(),
                line: line_num + 1,
                subtests,
            });
        }
    }

    Ok(tests)
}

fn print_tests(tests: &[TestInfo], show_subtests: bool, show_parent: bool) {
    for test in tests {
        if test.subtests.is_empty() {
            println!("^{}$", test.name);
        } else {
            if show_parent {
                println!("^{}$", test.name);
            }
            if show_subtests {
                for subtest in &test.subtests {
                    println!("^{}/{}$", test.name, subtest);
                }
            }
        }
    }
}

fn run_with_skim(tests: Vec<TestInfo>, tags: Option<String>, verbose: bool) -> Result<()> {
    let test_patterns = collect_test_patterns(&tests);

    if test_patterns.is_empty() {
        println!("No tests found");
        return Ok(());
    }

    let selected_tests = skim_select(&test_patterns)?;

    if selected_tests.is_empty() {
        println!("No tests selected");
        return Ok(());
    }

    let run_pattern = build_run_pattern(&selected_tests);
    execute_go_test(&run_pattern, tags, verbose)?;

    Ok(())
}

fn collect_test_patterns(tests: &[TestInfo]) -> Vec<String> {
    let mut patterns = Vec::new();

    for test in tests {
        if test.subtests.is_empty() {
            patterns.push(test.name.clone());
        } else {
            patterns.push(test.name.clone());
            for subtest in &test.subtests {
                patterns.push(format!("{}/{}", test.name, subtest));
            }
        }
    }

    patterns
}

fn skim_select(options: &[String]) -> Result<Vec<String>> {
    let options_str = options.join("\n");
    let item_reader = SkimItemReader::default();
    let items = item_reader.of_bufread(Cursor::new(options_str));

    let skim_options = SkimOptionsBuilder::default()
        .height("50%".to_string())
        .color(Some("light".to_string()))
        .multi(true)
        .prompt("Select tests (TAB to multi-select): ".to_string())
        .header(Some(
            "Press TAB to select multiple tests, ENTER to confirm".to_string(),
        ))
        .build()
        .map_err(|e| anyhow::anyhow!("Failed to build skim options: {}", e))?;

    let result = Skim::run_with(&skim_options, Some(items));

    print!("\x1b[2J\x1b[H");
    io::stdout().flush().unwrap();

    if let Some(output) = result {
        if output.is_abort {
            return Ok(vec![]);
        }

        Ok(output
            .selected_items
            .iter()
            .map(|item| item.output().to_string())
            .collect())
    } else {
        Ok(vec![])
    }
}

fn build_run_pattern(selected_tests: &[String]) -> String {
    if selected_tests.is_empty() {
        return String::new();
    }

    if selected_tests.len() == 1 {
        return selected_tests[0].clone();
    }

    selected_tests.join("|")
}

fn execute_go_test(run_pattern: &str, tags: Option<String>, verbose: bool) -> Result<()> {
    let mut cmd = Command::new("go");
    cmd.args(["test", "-count=1"]);

    if verbose {
        cmd.arg("-v");
    }

    if let Some(tags_value) = tags {
        cmd.arg(format!("-tags={}", tags_value));
    }

    if !run_pattern.is_empty() {
        cmd.arg("-run").arg(run_pattern);
    }

    cmd.arg("./...");

    println!(
        "Running: go {}",
        cmd.get_args()
            .map(|arg| arg.to_string_lossy())
            .collect::<Vec<_>>()
            .join(" ")
    );

    let status = cmd.status()?;

    if !status.success() {
        std::process::exit(status.code().unwrap_or(1));
    }

    Ok(())
}
