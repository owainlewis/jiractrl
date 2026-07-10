package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://jira.example.com"
	defaultFields  = "summary,status,assignee,priority,issuetype"
	binaryName     = "jiractrl"
)

type config struct {
	BaseURL           string
	Token             string
	Timeout           time.Duration
	DefaultMaxResults int
	DefaultOutput     string
	Profiles          map[string]profile
}

type profile struct {
	JQL        string
	Fields     []string
	MaxResults int
}

type jiraClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type jiraError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *jiraError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("jira request failed: %s", e.Status)
	}
	return fmt.Sprintf("jira request failed: %s: %s", e.Status, e.Body)
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	var configPath string
	args, err := parseGlobalFlags(args, &configPath)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "auth":
		return runAuth(args[1:], stdout, configPath)
	case "search", "list":
		return runSearch(args[1:], stdout, configPath)
	case "get":
		return runGet(args[1:], stdout, configPath)
	case "create":
		return runCreate(args[1:], stdout, configPath)
	case "update":
		return runUpdate(args[1:], stdout, configPath)
	case "comment":
		return runComment(args[1:], stdout, configPath)
	case "transitions":
		return runTransitions(args[1:], stdout, configPath)
	case "transition":
		return runTransition(args[1:], stdout, configPath)
	case "fields":
		return runFields(args[1:], stdout, configPath)
	case "issue-fields":
		return runIssueFields(args[1:], stdout, configPath)
	case "profiles":
		return runProfiles(args[1:], stdout, configPath)
	case "triage":
		return runTriage(args[1:], stdout, configPath)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func parseGlobalFlags(args []string, configPath *string) ([]string, error) {
	var out []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				return nil, errors.New("missing value for --config")
			}
			*configPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--config="):
			*configPath = strings.TrimPrefix(arg, "--config=")
		default:
			out = append(out, arg)
		}
	}
	return out, nil
}

func runAuth(args []string, stdout io.Writer, configPath string) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("usage: jiractrl auth check")
	}

	cfg, err := loadConfig(configPath, 15*time.Second)
	if err != nil {
		return err
	}

	client := newJiraClient(cfg)
	var me jiraUser
	if err := client.do(context.Background(), http.MethodGet, "/rest/api/2/myself", nil, &me); err != nil {
		return err
	}

	name := firstNonEmpty(me.DisplayName, me.Name, me.EmailAddress, me.Key)
	if name == "" {
		name = "(authenticated user)"
	}
	fmt.Fprintf(stdout, "Authenticated as %s\n", name)
	return nil
}

func runSearch(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	jql := fs.String("jql", "", "JQL query to run")
	profileName := fs.String("profile", "", "profile from config.toml to use")
	maxResults := fs.Int("max", 0, "maximum number of issues to return")
	rawJSON := fs.Bool("json", false, "print raw JSON response")
	fields := fs.String("fields", "", "comma-separated fields to request")
	withDescription := fs.Bool("description", false, "include issue descriptions in text output")

	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl search --jql '<query>' [--max 20] [--json] [--description]")
	}

	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}

	selectedFields := strings.TrimSpace(*fields)
	selectedMax := *maxResults
	if strings.TrimSpace(*profileName) != "" {
		p, ok := cfg.Profiles[*profileName]
		if !ok {
			return fmt.Errorf("unknown profile %q", *profileName)
		}
		if strings.TrimSpace(*jql) == "" {
			*jql = p.JQL
		}
		if selectedFields == "" && len(p.Fields) > 0 {
			selectedFields = strings.Join(p.Fields, ",")
		}
		if selectedMax == 0 && p.MaxResults > 0 {
			selectedMax = p.MaxResults
		}
	}
	if strings.TrimSpace(*jql) == "" {
		return errors.New("missing required --jql or --profile with jql")
	}
	if selectedFields == "" {
		selectedFields = defaultFields
	}
	if selectedMax == 0 {
		selectedMax = cfg.DefaultMaxResults
	}
	if selectedMax == 0 {
		selectedMax = 20
	}
	if selectedMax < 1 || selectedMax > 1000 {
		return errors.New("--max must be between 1 and 1000")
	}

	client := newJiraClient(cfg)
	if *withDescription {
		selectedFields = addField(selectedFields, "description")
	}
	result, err := client.search(context.Background(), *jql, selectedFields, selectedMax)
	if err != nil {
		return err
	}

	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	printIssues(stdout, result, *withDescription)
	return nil
}

func runGet(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	rawJSON := fs.Bool("json", false, "print raw JSON response")
	fields := fs.String("fields", "summary,description,status,assignee,priority,issuetype,labels,created,updated,comment", "comma-separated fields to request")

	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl get ISSUE-123 [--json]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl get ISSUE-123 [--json]")
	}

	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}

	client := newJiraClient(cfg)
	issue, err := client.getIssue(context.Background(), fs.Arg(0), *fields)
	if err != nil {
		return err
	}

	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(issue)
	}

	printIssue(stdout, issue)
	return nil
}

func runCreate(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	project := fs.String("project", "", "project key")
	issueType := fs.String("type", "Task", "issue type")
	summary := fs.String("summary", "", "issue summary")
	description := fs.String("description", "", "issue description")
	descriptionFile := fs.String("description-file", "", "path to description file")
	rawJSON := fs.Bool("json", false, "print JSON response")

	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl create --project KEY --type Task --summary '...' [--description '...']")
	}
	if strings.TrimSpace(*project) == "" || strings.TrimSpace(*summary) == "" {
		return errors.New("missing required --project or --summary")
	}
	body, err := readInlineOrFile(*description, *descriptionFile)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	created, err := client.createIssue(context.Background(), *project, *issueType, *summary, body)
	if err != nil {
		return err
	}
	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(created)
	}
	fmt.Fprintf(stdout, "Created %s\n", created.Key)
	return nil
}

func runUpdate(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	summary := fs.String("summary", "", "new summary")
	description := fs.String("description", "", "new description")
	descriptionFile := fs.String("description-file", "", "path to description file")
	fieldValues := multiFlag{}
	fs.Var(&fieldValues, "field", "field assignment as name=value; may be repeated")

	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl update ISSUE-123 [--summary '...'] [--description '...'] [--field name=value]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl update ISSUE-123 [--summary '...'] [--description '...'] [--field name=value]")
	}
	fields := map[string]any{}
	if strings.TrimSpace(*summary) != "" {
		fields["summary"] = *summary
	}
	if strings.TrimSpace(*description) != "" || strings.TrimSpace(*descriptionFile) != "" {
		body, err := readInlineOrFile(*description, *descriptionFile)
		if err != nil {
			return err
		}
		fields["description"] = body
	}
	for _, value := range fieldValues {
		name, raw, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("invalid --field %q; use name=value", value)
		}
		fields[strings.TrimSpace(name)] = strings.TrimSpace(raw)
	}
	if len(fields) == 0 {
		return errors.New("no updates requested")
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	if err := client.updateIssue(context.Background(), fs.Arg(0), fields); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated %s\n", fs.Arg(0))
	return nil
}

func runComment(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	body := fs.String("body", "", "comment body")
	bodyFile := fs.String("body-file", "", "path to comment body file")
	rawJSON := fs.Bool("json", false, "print JSON response")

	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl comment ISSUE-123 --body '...'")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl comment ISSUE-123 --body '...'")
	}
	comment, err := readInlineOrFile(*body, *bodyFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(comment) == "" {
		return errors.New("missing required --body or --body-file")
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	result, err := client.addComment(context.Background(), fs.Arg(0), comment)
	if err != nil {
		return err
	}
	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	fmt.Fprintf(stdout, "Commented on %s\n", fs.Arg(0))
	return nil
}

func runTransitions(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("transitions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl transitions ISSUE-123 [--json]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl transitions ISSUE-123 [--json]")
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	result, err := client.transitions(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	for _, transition := range result.Transitions {
		fmt.Fprintf(stdout, "%s  %s\n", transition.ID, transition.Name)
	}
	return nil
}

func runTransition(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("transition", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	to := fs.String("to", "", "transition name or id")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl transition ISSUE-123 --to 'In Progress'")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" || strings.TrimSpace(*to) == "" {
		return errors.New("usage: jiractrl transition ISSUE-123 --to 'In Progress'")
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	transitions, err := client.transitions(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	id := ""
	for _, transition := range transitions.Transitions {
		if strings.EqualFold(transition.ID, *to) || strings.EqualFold(transition.Name, *to) {
			id = transition.ID
			break
		}
	}
	if id == "" {
		return fmt.Errorf("transition %q not found", *to)
	}
	if err := client.transitionIssue(context.Background(), fs.Arg(0), id); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Transitioned %s via %s\n", fs.Arg(0), *to)
	return nil
}

func runFields(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("fields", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl fields [--json]")
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	fields, err := client.fields(context.Background())
	if err != nil {
		return err
	}
	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(fields)
	}
	for _, field := range fields {
		fmt.Fprintf(stdout, "%s  %s\n", field.ID, field.Name)
	}
	return nil
}

func runIssueFields(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("issue-fields", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl issue-fields ISSUE-123 [--json]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl issue-fields ISSUE-123 [--json]")
	}
	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	client := newJiraClient(cfg)
	issue, err := client.getIssueRaw(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(issue.Fields)
	}
	for name, value := range issue.Fields {
		if value != nil {
			fmt.Fprintf(stdout, "%s\n", name)
		}
	}
	return nil
}

func runProfiles(args []string, stdout io.Writer, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl profiles list|show NAME")
	}
	cfg, err := loadConfig(configPath, 5*time.Second)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		for name := range cfg.Profiles {
			fmt.Fprintln(stdout, name)
		}
		return nil
	case "show":
		if len(args) != 2 {
			return errors.New("usage: jiractrl profiles show NAME")
		}
		p, ok := cfg.Profiles[args[1]]
		if !ok {
			return fmt.Errorf("unknown profile %q", args[1])
		}
		fmt.Fprintf(stdout, "%s\n", args[1])
		fmt.Fprintf(stdout, "  jql: %s\n", p.JQL)
		if len(p.Fields) > 0 {
			fmt.Fprintf(stdout, "  fields: %s\n", strings.Join(p.Fields, ", "))
		}
		if p.MaxResults > 0 {
			fmt.Fprintf(stdout, "  max_results: %d\n", p.MaxResults)
		}
		return nil
	default:
		return errors.New("usage: jiractrl profiles list|show NAME")
	}
}

func runTriage(args []string, stdout io.Writer, configPath string) error {
	fs := flag.NewFlagSet("triage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	jql := fs.String("jql", "", "JQL query to triage")
	maxResults := fs.Int("max", 10, "maximum number of issues to triage")
	dryRun := fs.Bool("dry-run", true, "print proposed triage without updating Jira")
	rawJSON := fs.Bool("json", false, "print JSON triage report")

	if err := fs.Parse(args); err != nil {
		return errors.New("usage: jiractrl triage --jql '<query>' [--max 10] [--dry-run] [--json]")
	}
	if strings.TrimSpace(*jql) == "" {
		return errors.New("missing required --jql")
	}
	if *maxResults < 1 || *maxResults > 100 {
		return errors.New("--max must be between 1 and 100")
	}
	if !*dryRun {
		return errors.New("only dry-run triage is implemented; omit --dry-run or set --dry-run=true")
	}

	cfg, err := loadConfig(configPath, 30*time.Second)
	if err != nil {
		return err
	}

	client := newJiraClient(cfg)
	result, err := client.search(context.Background(), *jql, triageFields, *maxResults)
	if err != nil {
		return err
	}

	report := make([]triageResult, 0, len(result.Issues))
	for _, issue := range result.Issues {
		report = append(report, classifyIssue(issue))
	}

	if *rawJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	printTriageReport(stdout, report)
	return nil
}

func loadConfig(configPath string, timeout time.Duration) (config, error) {
	cfg := config{
		BaseURL:           defaultBaseURL,
		Timeout:           timeout,
		DefaultMaxResults: 20,
		DefaultOutput:     "text",
		Profiles:          map[string]profile{},
	}
	if configPath == "" {
		configPath = os.Getenv("JIRACTRL_CONFIG")
	}
	if configPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configPath = filepath.Join(home, ".config", binaryName, "config.toml")
		}
	}
	if configPath != "" {
		if fileCfg, err := readConfigFile(configPath); err == nil {
			cfg = mergeConfig(cfg, fileCfg)
		} else if !errors.Is(err, os.ErrNotExist) {
			return config{}, err
		}
	}
	cfg.BaseURL = strings.TrimRight(firstNonEmpty(os.Getenv("JIRACTRL_BASE_URL"), os.Getenv("JIRA_BASE_URL"), cfg.BaseURL), "/")
	cfg.Token = firstNonEmpty(os.Getenv("JIRACTRL_TOKEN"), os.Getenv("JIRA_PAT"), os.Getenv("JIRA_TOKEN"), cfg.Token)
	if strings.TrimSpace(cfg.Token) == "" {
		return config{}, errors.New("set token in config.toml or JIRACTRL_TOKEN/JIRA_PAT")
	}
	return cfg, nil
}

const triageFields = "summary,description,status,assignee,priority,issuetype,labels,created,updated,comment"

func readConfigFile(path string) (cfg config, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%v", recovered)
		}
	}()

	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}

	cfg = config{Profiles: map[string]profile{}}
	section := ""
	profileName := ""

	for lineNo, rawLine := range strings.Split(string(data), "\n") {
		line := stripTOMLComment(strings.TrimSpace(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			profileName = ""
			if strings.HasPrefix(section, "profiles.") {
				profileName = strings.TrimPrefix(section, "profiles.")
				if profileName == "" {
					return config{}, fmt.Errorf("%s:%d: empty profile name", path, lineNo+1)
				}
				if cfg.Profiles == nil {
					cfg.Profiles = map[string]profile{}
				}
				if _, ok := cfg.Profiles[profileName]; !ok {
					cfg.Profiles[profileName] = profile{}
				}
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return config{}, fmt.Errorf("%s:%d: expected key = value", path, lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch {
		case section == "jira":
			switch key {
			case "base_url":
				cfg.BaseURL = mustParseTOMLString(path, lineNo+1, value)
			case "token":
				cfg.Token = mustParseTOMLString(path, lineNo+1, value)
			}
		case section == "defaults":
			switch key {
			case "max_results":
				cfg.DefaultMaxResults = mustParseTOMLInt(path, lineNo+1, value)
			case "output":
				cfg.DefaultOutput = mustParseTOMLString(path, lineNo+1, value)
			}
		case profileName != "":
			p := cfg.Profiles[profileName]
			switch key {
			case "jql":
				p.JQL = mustParseTOMLString(path, lineNo+1, value)
			case "fields":
				p.Fields = mustParseTOMLStringArray(path, lineNo+1, value)
			case "max_results":
				p.MaxResults = mustParseTOMLInt(path, lineNo+1, value)
			}
			cfg.Profiles[profileName] = p
		}
	}

	return cfg, nil
}

func mergeConfig(base, override config) config {
	if override.BaseURL != "" {
		base.BaseURL = override.BaseURL
	}
	if override.Token != "" {
		base.Token = override.Token
	}
	if override.DefaultMaxResults != 0 {
		base.DefaultMaxResults = override.DefaultMaxResults
	}
	if override.DefaultOutput != "" {
		base.DefaultOutput = override.DefaultOutput
	}
	if base.Profiles == nil {
		base.Profiles = map[string]profile{}
	}
	for name, p := range override.Profiles {
		base.Profiles[name] = p
	}
	return base
}

func stripTOMLComment(line string) string {
	inQuote := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == '#' && !inQuote {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func mustParseTOMLString(path string, lineNo int, value string) string {
	parsed, err := strconv.Unquote(value)
	if err != nil {
		panic(fmt.Sprintf("%s:%d: expected quoted string", path, lineNo))
	}
	return parsed
}

func mustParseTOMLInt(path string, lineNo int, value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("%s:%d: expected integer", path, lineNo))
	}
	return parsed
}

func mustParseTOMLStringArray(path string, lineNo int, value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		panic(fmt.Sprintf("%s:%d: expected string array", path, lineNo))
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if body == "" {
		return nil
	}
	var values []string
	for _, part := range splitCommaList(body) {
		values = append(values, mustParseTOMLString(path, lineNo, strings.TrimSpace(part)))
	}
	return values
}

func splitCommaList(value string) []string {
	var parts []string
	start := 0
	inQuote := false
	escaped := false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == ',' && !inQuote {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func newJiraClient(cfg config) *jiraClient {
	return &jiraClient{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		http: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (c *jiraClient) search(ctx context.Context, jql, fields string, maxResults int) (*searchResponse, error) {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("fields", fields)
	q.Set("maxResults", strconv.Itoa(maxResults))

	var result searchResponse
	err := c.do(ctx, http.MethodGet, "/rest/api/2/search?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *jiraClient) getIssue(ctx context.Context, key, fields string) (*jiraIssue, error) {
	q := url.Values{}
	q.Set("fields", fields)

	var result jiraIssue
	err := c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"?"+q.Encode(), nil, &result)
	return &result, err
}

func (c *jiraClient) getIssueRaw(ctx context.Context, key string) (*rawIssue, error) {
	var result rawIssue
	err := c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key), nil, &result)
	return &result, err
}

func (c *jiraClient) createIssue(ctx context.Context, project, issueType, summary, description string) (*createdIssue, error) {
	body := map[string]any{
		"fields": map[string]any{
			"project": map[string]string{
				"key": project,
			},
			"issuetype": map[string]string{
				"name": issueType,
			},
			"summary": summary,
		},
	}
	if strings.TrimSpace(description) != "" {
		body["fields"].(map[string]any)["description"] = description
	}

	var result createdIssue
	err := c.do(ctx, http.MethodPost, "/rest/api/2/issue", body, &result)
	return &result, err
}

func (c *jiraClient) updateIssue(ctx context.Context, key string, fields map[string]any) error {
	body := map[string]any{
		"fields": fields,
	}
	return c.do(ctx, http.MethodPut, "/rest/api/2/issue/"+url.PathEscape(key), body, nil)
}

func (c *jiraClient) addComment(ctx context.Context, key, comment string) (*jiraComment, error) {
	body := map[string]any{
		"body": comment,
	}
	var result jiraComment
	err := c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", body, &result)
	return &result, err
}

func (c *jiraClient) transitions(ctx context.Context, key string) (*transitionResponse, error) {
	var result transitionResponse
	err := c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, &result)
	return &result, err
}

func (c *jiraClient) transitionIssue(ctx context.Context, key, transitionID string) error {
	body := map[string]any{
		"transition": map[string]string{
			"id": transitionID,
		},
	}
	return c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", body, nil)
}

func (c *jiraClient) fields(ctx context.Context) ([]jiraField, error) {
	var result []jiraField
	err := c.do(ctx, http.MethodGet, "/rest/api/2/field", nil, &result)
	return result, err
}

func (c *jiraClient) do(ctx context.Context, method, path string, body any, out any) error {
	var requestBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &jiraError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(respBody)),
		}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func classifyIssue(issue jiraIssue) triageResult {
	description := strings.TrimSpace(issue.Fields.Description)
	text := strings.ToLower(issue.Fields.Summary + "\n" + description)
	missing := missingTriageSignals(issue, text)
	labels := []string{"ai-triaged"}
	classification := "ready-for-engineering"
	confidence := "medium"
	reason := "Issue has a usable description and enough routing context for an engineer to start."

	if containsAny(text, "sev", "incident", "outage", "customer impact", "mitigation", "rollback", "postmortem", "alert") {
		classification = "incident-followup"
		labels = append(labels, "incident-followup")
		reason = "Issue appears to be related to an incident, alert, outage, mitigation, or customer-impacting follow-up."
		if len(missing) > 0 {
			confidence = "medium"
		} else {
			confidence = "high"
		}
	} else if len(missing) >= 3 || len(description) < 120 {
		classification = "needs-more-info"
		labels = append(labels, "needs-info")
		confidence = "high"
		reason = "Issue is missing several signals usually needed for incident or engineering triage."
	} else if isLikelyStale(issue) {
		classification = "stale-or-duplicate-risk"
		labels = append(labels, "stale-review")
		reason = "Issue is unassigned and has limited visible discussion, so it may need ownership or duplicate review."
	}

	return triageResult{
		Issue:            issue.Key,
		Summary:          issue.Fields.Summary,
		Status:           issue.Fields.Status.Name,
		Assignee:         firstNonEmpty(issue.Fields.Assignee.DisplayName, "unassigned"),
		Classification:   classification,
		Confidence:       confidence,
		Reason:           reason,
		Missing:          missing,
		SuggestedLabels:  labels,
		SuggestedComment: suggestedComment(classification, missing),
	}
}

func missingTriageSignals(issue jiraIssue, text string) []string {
	var missing []string
	if strings.TrimSpace(issue.Fields.Description) == "" {
		missing = append(missing, "description")
	}
	if !containsAny(text, "impact", "customer", "severity", "sev", "blast radius", "affected") {
		missing = append(missing, "impact")
	}
	if !containsAny(text, "region", "realm", "environment", "prod", "dev", "staging", "tenant") {
		missing = append(missing, "environment")
	}
	if !containsAny(text, "repro", "steps", "expected", "actual", "observed") {
		missing = append(missing, "reproduction_or_expected_actual")
	}
	if !containsAny(text, "log", "dashboard", "alarm", "metric", "trace", "opc-request-id", "runbook") {
		missing = append(missing, "evidence_or_logs")
	}
	if issue.Fields.Assignee.DisplayName == "" {
		missing = append(missing, "owner")
	}
	return missing
}

func isLikelyStale(issue jiraIssue) bool {
	return issue.Fields.Assignee.DisplayName == "" && issue.Fields.Comment.Total == 0
}

func suggestedComment(classification string, missing []string) string {
	switch classification {
	case "needs-more-info":
		return "Dry-run suggestion: please add the missing triage context: " + strings.Join(missing, ", ") + ". Useful details include impact, affected environment/region, reproduction or expected-vs-actual behavior, and links to logs, alarms, dashboards, or incidents."
	case "incident-followup":
		return "Dry-run suggestion: please confirm the incident/customer impact, mitigation status, affected regions, and links to alarms, dashboards, runbooks, or postmortem notes."
	case "stale-or-duplicate-risk":
		return "Dry-run suggestion: please confirm whether this still needs work, assign an owner, or link the duplicate/canonical issue."
	default:
		return "Dry-run suggestion: this looks ready for engineering triage. Confirm priority, owner, and next action."
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func printTriageReport(w io.Writer, report []triageResult) {
	if len(report) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	fmt.Fprintf(w, "Dry-run triage report for %d issue(s). No Jira updates were made.\n\n", len(report))
	for _, result := range report {
		fmt.Fprintf(w, "%s  [%s]  %s\n", result.Issue, result.Status, result.Summary)
		fmt.Fprintf(w, "  classification: %s (%s confidence)\n", result.Classification, result.Confidence)
		fmt.Fprintf(w, "  assignee: %s\n", result.Assignee)
		fmt.Fprintf(w, "  reason: %s\n", result.Reason)
		if len(result.Missing) > 0 {
			fmt.Fprintf(w, "  missing: %s\n", strings.Join(result.Missing, ", "))
		}
		fmt.Fprintf(w, "  suggested labels: %s\n", strings.Join(result.SuggestedLabels, ", "))
		fmt.Fprintf(w, "  suggested comment: %s\n\n", result.SuggestedComment)
	}
}

func printIssues(w io.Writer, result *searchResponse, withDescription bool) {
	if len(result.Issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	fmt.Fprintf(w, "Found %d issue(s)", len(result.Issues))
	if result.Total > len(result.Issues) {
		fmt.Fprintf(w, " of %d total", result.Total)
	}
	fmt.Fprintln(w, ":")

	for _, issue := range result.Issues {
		assignee := "unassigned"
		if issue.Fields.Assignee.DisplayName != "" {
			assignee = issue.Fields.Assignee.DisplayName
		}

		fmt.Fprintf(
			w,
			"%s  [%s]  %s  priority=%s  assignee=%s\n",
			issue.Key,
			firstNonEmpty(issue.Fields.Status.Name, "unknown"),
			issue.Fields.Summary,
			firstNonEmpty(issue.Fields.Priority.Name, "none"),
			assignee,
		)
		if withDescription {
			description := strings.TrimSpace(issue.Fields.Description)
			if description == "" {
				description = "(no description)"
			}
			fmt.Fprintf(w, "  description: %s\n", oneLine(description))
		}
	}
}

func printIssue(w io.Writer, issue *jiraIssue) {
	assignee := firstNonEmpty(issue.Fields.Assignee.DisplayName, "unassigned")
	fmt.Fprintf(w, "%s  [%s]  %s\n", issue.Key, firstNonEmpty(issue.Fields.Status.Name, "unknown"), issue.Fields.Summary)
	fmt.Fprintf(w, "type: %s\n", firstNonEmpty(issue.Fields.IssueType.Name, "unknown"))
	fmt.Fprintf(w, "priority: %s\n", firstNonEmpty(issue.Fields.Priority.Name, "none"))
	fmt.Fprintf(w, "assignee: %s\n", assignee)
	if issue.Fields.Created != "" {
		fmt.Fprintf(w, "created: %s\n", issue.Fields.Created)
	}
	if issue.Fields.Updated != "" {
		fmt.Fprintf(w, "updated: %s\n", issue.Fields.Updated)
	}
	if len(issue.Fields.Labels) > 0 {
		fmt.Fprintf(w, "labels: %s\n", strings.Join(issue.Fields.Labels, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Description:")
	description := strings.TrimSpace(issue.Fields.Description)
	if description == "" {
		description = "(no description)"
	}
	fmt.Fprintln(w, description)
	if issue.Fields.Comment.Total > 0 {
		fmt.Fprintf(w, "\nComments: %d\n", issue.Fields.Comment.Total)
		for _, comment := range issue.Fields.Comment.Comments {
			author := firstNonEmpty(comment.Author.DisplayName, comment.Author.Name, "unknown")
			fmt.Fprintf(w, "\n- %s", author)
			if comment.Created != "" {
				fmt.Fprintf(w, " at %s", comment.Created)
			}
			fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(comment.Body))
		}
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `jiractrl - a control plane for Jira for AI agents

Usage:
  jiractrl [--config config.toml] auth check
  jiractrl search --jql '<query>' [--max 20] [--json] [--description]
  jiractrl search --profile NAME [--json]
  jiractrl get ISSUE-123 [--json]
  jiractrl create --project KEY --type Task --summary '...' [--description '...']
  jiractrl update ISSUE-123 [--summary '...'] [--description '...'] [--field name=value]
  jiractrl comment ISSUE-123 --body '...'
  jiractrl transitions ISSUE-123
  jiractrl transition ISSUE-123 --to 'In Progress'
  jiractrl fields
  jiractrl issue-fields ISSUE-123
  jiractrl profiles list
  jiractrl profiles show NAME
  jiractrl triage --jql '<query>' [--max 10] [--dry-run] [--json]

Environment:
  JIRACTRL_CONFIG    Optional config path
  JIRACTRL_BASE_URL  Jira base URL
  JIRACTRL_TOKEN     Jira personal access token
  JIRA_BASE_URL      Fallback Jira base URL
  JIRA_PAT           Fallback Jira personal access token

Examples:
  jiractrl auth check
  jiractrl search --jql 'project = MYPROJ AND statusCategory != Done ORDER BY updated DESC'
  jiractrl search --profile my_open --json
  jiractrl get MYPROJ-123
  jiractrl comment MYPROJ-123 --body 'Following up with more context.'
`)
}

func addField(fields, field string) string {
	parts := strings.Split(fields, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == field {
			return fields
		}
	}
	if strings.TrimSpace(fields) == "" {
		return field
	}
	return fields + "," + field
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func readInlineOrFile(inline, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return inline, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(inline) != "" {
		return "", errors.New("use either inline text or file input, not both")
	}
	return string(data), nil
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

type jiraUser struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type searchResponse struct {
	StartAt    int         `json:"startAt"`
	MaxResults int         `json:"maxResults"`
	Total      int         `json:"total"`
	Issues     []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	ID     string     `json:"id"`
	Key    string     `json:"key"`
	Fields issueField `json:"fields"`
}

type rawIssue struct {
	ID     string         `json:"id"`
	Key    string         `json:"key"`
	Fields map[string]any `json:"fields"`
}

type issueField struct {
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Status      namedValue   `json:"status"`
	Priority    namedValue   `json:"priority"`
	IssueType   namedValue   `json:"issuetype"`
	Assignee    jiraUser     `json:"assignee"`
	Labels      []string     `json:"labels"`
	Created     string       `json:"created"`
	Updated     string       `json:"updated"`
	Comment     commentBlock `json:"comment"`
}

type namedValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type commentBlock struct {
	Total    int           `json:"total"`
	Comments []jiraComment `json:"comments"`
}

type jiraComment struct {
	Author  jiraUser `json:"author"`
	Body    string   `json:"body"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
}

type createdIssue struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

type transitionResponse struct {
	Transitions []transition `json:"transitions"`
}

type transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		Name string `json:"name"`
	} `json:"to"`
}

type jiraField struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Custom      bool     `json:"custom"`
	Order       int      `json:"orderable,omitempty"`
	ClauseNames []string `json:"clauseNames,omitempty"`
}

type triageResult struct {
	Issue            string   `json:"issue"`
	Summary          string   `json:"summary"`
	Status           string   `json:"status"`
	Assignee         string   `json:"assignee"`
	Classification   string   `json:"classification"`
	Confidence       string   `json:"confidence"`
	Reason           string   `json:"reason"`
	Missing          []string `json:"missing,omitempty"`
	SuggestedLabels  []string `json:"suggested_labels"`
	SuggestedComment string   `json:"suggested_comment"`
}
