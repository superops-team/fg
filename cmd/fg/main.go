package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	fg "github.com/superops-team/fg"
	"github.com/superops-team/fg/grep"
	"github.com/superops-team/fg/picker"
)

const defaultLimit = 20

type cliConfig struct {
	root      string
	limit     int
	showScore bool
	grepText  string
	help      bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, rest, err := parseArgs(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if cfg.help {
		printUsage(stdout)
		return 0
	}
	if cfg.limit <= 0 {
		cfg.limit = defaultLimit
	}

	query := ""
	if len(rest) > 0 {
		query = strings.Join(rest, " ")
	}
	if query == "" && cfg.grepText == "" {
		printUsage(stderr)
		return 2
	}

	if cfg.grepText != "" {
		return runGrep(cfg, query, stdout, stderr)
	}
	return runSearch(cfg, query, stdout, stderr)
}

func parseArgs(args []string, output io.Writer) (cliConfig, []string, error) {
	var cfg cliConfig
	fs := flag.NewFlagSet("fg", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&cfg.root, "r", ".", "search root")
	fs.StringVar(&cfg.root, "root", ".", "search root")
	fs.IntVar(&cfg.limit, "limit", defaultLimit, "maximum number of results")
	fs.BoolVar(&cfg.showScore, "score", false, "print score with each path")
	fs.StringVar(&cfg.grepText, "grep", "", "search file contents")
	fs.BoolVar(&cfg.help, "help", false, "show help")
	fs.BoolVar(&cfg.help, "h", false, "show help")
	flagArgs, rest := splitFlagArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return cfg, nil, err
	}
	return cfg, rest, nil
}

func splitFlagArgs(args []string) ([]string, []string) {
	flagArgs := make([]string, 0, len(args))
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if isValueFlag(arg) {
			flagArgs = append(flagArgs, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		if isBoolFlag(arg) || strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			continue
		}
		rest = append(rest, arg)
	}
	return flagArgs, rest
}

func isValueFlag(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if before, _, ok := strings.Cut(name, "="); ok {
		name = before
	}
	switch name {
	case "r", "root", "limit", "grep":
		return true
	default:
		return false
	}
}

func isBoolFlag(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	switch name {
	case "score", "help", "h":
		return true
	default:
		return false
	}
}

func runSearch(cfg cliConfig, query string, stdout, stderr io.Writer) int {
	results, err := fg.Search(cfg.root, query, cfg.limit)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, r := range results {
		if cfg.showScore {
			fmt.Fprintf(stdout, "%d\t%s\n", r.Score, r.Path)
		} else {
			fmt.Fprintln(stdout, r.Path)
		}
	}
	return 0
}

func runGrep(cfg cliConfig, fileQuery string, stdout, stderr io.Writer) int {
	paths, err := grepTargets(cfg.root, fileQuery, cfg.limit)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	matcher := grep.New(grep.Options{})
	results, err := matcher.SearchMany(paths, cfg.grepText, cfg.limit)
	return printGrepResults(stdout, stderr, results, err, cfg.limit)
}

func printGrepResults(stdout, stderr io.Writer, results []grep.FileResult, err error, limit int) int {
	if err != nil {
		defer fmt.Fprintln(stderr, err)
	}
	printedFiles := 0
	for _, fr := range results {
		if printedFiles >= limit {
			break
		}
		printedFiles++
		for _, line := range fr.Lines {
			fmt.Fprintf(stdout, "%s:%d:%s\n", fr.Path, line.Lineno, line.Text)
		}
	}
	if err != nil {
		return 1
	}
	return 0
}

func grepTargets(root, fileQuery string, limit int) ([]string, error) {
	if fileQuery != "" {
		results, err := fg.Search(root, fileQuery, limit)
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(results))
		for _, r := range results {
			paths = append(paths, r.Path)
		}
		return paths, nil
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	p := picker.New(root, picker.Options{})
	defer p.Close()
	if err := p.Scan(); err != nil {
		return nil, err
	}
	paths := make([]string, 0, p.FileCount())
	for i := 0; i < p.FileCount(); i++ {
		paths = append(paths, p.PathAt(i))
	}
	return paths, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: fg [flags] [query]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Modes:")
	fmt.Fprintln(w, "  1) Fuzzy file search")
	fmt.Fprintln(w, "     fg [flags] \"type:go main\"")
	fmt.Fprintln(w, "  2) Grep inside matched files")
	fmt.Fprintln(w, "     fg [flags] --grep \"TODO\"")
	fmt.Fprintln(w, "  3) Fuzzy file search + grep in the filtered subset")
	fmt.Fprintln(w, "     fg [flags] \"type:go main\" --grep \"TODO\"")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Query constraints:")
	fmt.Fprintln(w, "  type:go           language / extension family filter")
	fmt.Fprintln(w, "  *.go              extension glob")
	fmt.Fprintln(w, "  size:>1KB         size compare (> >= < <= =)")
	fmt.Fprintln(w, "  modified:7d       modified within duration window")
	fmt.Fprintln(w, "  /src/             path segment match")
	fmt.Fprintln(w, "  **/*.go           glob path match")
	fmt.Fprintln(w, "  !vendor           negation")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Root ignore:")
	fmt.Fprintln(w, "  - Always skips .git, .svn, .hg, .idea, node_modules")
	fmt.Fprintln(w, "  - Reads root-level .gitignore and .ignore")
	fmt.Fprintln(w, "  - When the candidate index can prove no fuzzy match, fg returns no results instead of falling back to all files")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -r, --root string   search root (default \".\")")
	fmt.Fprintln(w, "      --limit int     maximum number of results (default 20)")
	fmt.Fprintln(w, "      --score         print score with each path")
	fmt.Fprintln(w, "      --grep string   search file contents")
	fmt.Fprintln(w, "  -h, --help          show help")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Library API:")
	fmt.Fprintln(w, "  - fg.Search(root, query, limit)")
	fmt.Fprintln(w, "  - fg.Open(ctx, fg.Options{Root: root}) + SearchContext / Refresh / Close")
}
