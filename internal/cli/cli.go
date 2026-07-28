package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/config"
	"github.com/owainlewis/jiractrl/internal/jira"
	"github.com/owainlewis/jiractrl/internal/triage"
)

type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func Run(args []string, stdout, stderr io.Writer) error {
	err := (App{Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}).Run(args)
	if err == nil || !wantsJSON(args) || IsReported(err) {
		return err
	}
	if writeErr := writeErrorJSON(stderr, err); writeErr != nil {
		return writeErr
	}
	return &reportedError{err: err}
}

func (a App) Run(args []string) error {
	var configPath string
	args, err := parseGlobalFlags(args, &configPath)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printUsage(a.Stdout)
		return nil
	}

	switch args[0] {
	case "auth":
		return a.runAuth(args[1:], configPath)
	case "server-info":
		return a.runServerInfo(args[1:], configPath)
	case "search", "list":
		return a.runSearch(args[1:], configPath)
	case "get":
		return a.runGet(args[1:], configPath)
	case "create":
		return a.runCreate(args[1:], configPath)
	case "update":
		return a.runUpdate(args[1:], configPath)
	case "assign":
		return a.runAssign(args[1:], configPath)
	case "comment":
		return a.runComment(args[1:], configPath)
	case "comments":
		return a.runComments(args[1:], configPath)
	case "transitions":
		return a.runTransitions(args[1:], configPath)
	case "transition":
		return a.runTransition(args[1:], configPath)
	case "fields":
		return a.runFields(args[1:], configPath)
	case "issue-fields":
		return a.runIssueFields(args[1:], configPath)
	case "profiles":
		return a.runProfiles(args[1:], configPath)
	case "projects":
		return a.runProjects(args[1:], configPath)
	case "meta":
		return a.runMeta(args[1:], configPath)
	case "users":
		return a.runUsers(args[1:], configPath)
	case "links":
		return a.runLinks(args[1:], configPath)
	case "attachments":
		return a.runAttachments(args[1:], configPath)
	case "changelog":
		return a.runChangelog(args[1:], configPath)
	case "worklogs":
		return a.runWorklogs(args[1:], configPath)
	case "watchers":
		return a.runWatchers(args[1:], configPath)
	case "bulk":
		return a.runBulk(args[1:], configPath)
	case "triage":
		return a.runTriage(args[1:], configPath)
	case "help", "-h", "--help":
		if len(args) > 1 {
			printCommandHelp(a.Stdout, args[1])
			return nil
		}
		printUsage(a.Stdout)
		return nil
	default:
		printUsage(a.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a App) runServerInfo(args []string, configPath string) error {
	fs := newFlagSet("server-info")
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("usage: jiractrl server-info [--json]")
	}

	client, err := a.client(configPath, 15*time.Second)
	if err != nil {
		return err
	}
	info, err := client.ServerInfo(context.Background())
	if err != nil {
		return err
	}
	if *rawJSON {
		return writeSuccessJSON(a.Stdout, info)
	}

	fmt.Fprintf(a.Stdout, "Deployment: %s (%s)\n", info.Deployment, info.DeploymentSource)
	if info.Version != "" {
		fmt.Fprintf(a.Stdout, "Version: %s\n", info.Version)
	}
	if info.BaseURL != "" {
		fmt.Fprintf(a.Stdout, "Base URL: %s\n", info.BaseURL)
	}
	fmt.Fprintln(a.Stdout, "Capabilities:")
	fmt.Fprintf(a.Stdout, "  platform: %s\n", info.Capabilities.Platform)
	fmt.Fprintf(a.Stdout, "  software: %s\n", info.Capabilities.Software)
	fmt.Fprintf(a.Stdout, "  service_management: %s\n", info.Capabilities.ServiceManagement)
	return nil
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

func (a App) runAuth(args []string, configPath string) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("usage: jiractrl auth check [--json]")
	}
	fs := newFlagSet("auth check")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errors.New("usage: jiractrl auth check [--json]")
	}

	client, err := a.client(configPath, 15*time.Second)
	if err != nil {
		return err
	}
	me, err := client.Myself(context.Background())
	if err != nil {
		return err
	}

	name := firstNonEmpty(me.DisplayName, me.Name, me.EmailAddress, me.Key, "(authenticated user)")
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{
			"authenticated": true,
			"user":          me,
		})
	}
	fmt.Fprintf(a.Stdout, "Authenticated as %s\n", name)
	return nil
}

func (a App) runSearch(args []string, configPath string) error {
	fs := newFlagSet("search")
	jql := fs.String("jql", "", "JQL query to run")
	profileName := fs.String("profile", "", "profile from config.toml to use")
	maxResults := fs.Int("max", 0, "maximum issues per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard issue limit when --all is set")
	cursor := fs.String("cursor", "", "opaque continuation cursor from a prior search")
	jsonOutput := fs.Bool("json", false, "print stable JSON envelope")
	rawJSON := fs.Bool("raw-json", false, "print the exact single-page Jira JSON response")
	fields := fs.String("fields", "", "comma-separated fields to request")
	withDescription := fs.Bool("description", false, "include issue descriptions in text output")
	reconcileValues := multiFlag{}
	fs.Var(&reconcileValues, "reconcile", "Cloud issue ID to reconcile for read-after-write consistency; may be repeated")

	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl search --jql '<query>' [--max 20] [--cursor CURSOR] [--all --limit 1000] [--json] [--description]")
	}
	if *limit < 1 || *limit > 10000 {
		return errors.New("--limit must be between 1 and 10000")
	}
	if *jsonOutput && *rawJSON {
		return errors.New("use either --json or --raw-json, not both")
	}
	if *rawJSON && *allResults {
		return errors.New("--raw-json cannot be combined with --all")
	}
	limitSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "limit" {
			limitSet = true
		}
	})
	if limitSet && !*allResults {
		return errors.New("--limit requires --all")
	}
	reconcileIDs, err := parsePositiveIDs(reconcileValues)
	if err != nil {
		return err
	}

	cfg, err := config.Load(configPath, 30*time.Second)
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
		selectedFields = config.DefaultFields
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

	client, err := newJiraClient(cfg)
	if err != nil {
		return err
	}
	if *withDescription {
		selectedFields = addField(selectedFields, "description")
	}
	options := jira.SearchOptions{
		JQL:               *jql,
		Fields:            splitCommaValues(selectedFields),
		MaxResults:        selectedMax,
		Cursor:            strings.TrimSpace(*cursor),
		ReconcileIssueIDs: reconcileIDs,
	}
	if *rawJSON {
		raw, err := client.SearchRawJSON(context.Background(), options)
		if err != nil {
			return err
		}
		_, err = a.Stdout.Write(raw)
		return err
	}
	var result *jira.SearchResponse
	if *allResults {
		result, err = client.SearchAll(context.Background(), options, *limit)
	} else {
		result, err = client.Search(context.Background(), options)
	}
	if err != nil {
		return err
	}

	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	printIssues(a.Stdout, result, *withDescription)
	return nil
}

func (a App) runGet(args []string, configPath string) error {
	fs := newFlagSet("get")
	jsonOutput := fs.Bool("json", false, "print stable JSON envelope")
	rawJSON := fs.Bool("raw-json", false, "print the exact Jira JSON response")
	fields := fs.String("fields", "summary,description,status,assignee,priority,issuetype,labels,created,updated,comment", "comma-separated fields to request")

	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl get ISSUE-123 [--json]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl get ISSUE-123 [--json]")
	}
	if *jsonOutput && *rawJSON {
		return errors.New("use either --json or --raw-json, not both")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	if *rawJSON {
		raw, err := client.GetIssueRawJSON(context.Background(), fs.Arg(0), *fields)
		if err != nil {
			return err
		}
		_, err = a.Stdout.Write(raw)
		return err
	}
	issue, err := client.GetIssue(context.Background(), fs.Arg(0), *fields)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, issue)
	}
	printIssue(a.Stdout, issue)
	return nil
}

func (a App) runCreate(args []string, configPath string) error {
	fs := newFlagSet("create")
	project := fs.String("project", "", "project key")
	issueType := fs.String("type", "Task", "issue type")
	summary := fs.String("summary", "", "issue summary")
	description := fs.String("description", "", "issue description")
	descriptionFile := fs.String("description-file", "", "path to description file")
	parent := fs.String("parent", "", "parent issue key for a subtask")
	inputPath := fs.String("input", "", "structured JSON input file, or - for stdin")
	dryRun := fs.Bool("dry-run", false, "print the exact Jira request without sending it")
	jsonOutput := fs.Bool("json", false, "print JSON response")

	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl create (--project KEY --summary TEXT | --input FILE|-) [--dry-run] [--json]")
	}
	if fs.NArg() != 0 {
		return errors.New("usage: jiractrl create (--project KEY --summary TEXT | --input FILE|-) [--dry-run] [--json]")
	}

	var payload map[string]any
	if strings.TrimSpace(*inputPath) != "" {
		if flagWasSet(fs, "project", "type", "summary", "description", "description-file", "parent") {
			return errors.New("--input conflicts with create convenience flags")
		}
		var err error
		payload, err = a.readMutationInput(*inputPath)
		if err != nil {
			return err
		}
		if err := validateMutationEnvelope("create", payload); err != nil {
			return err
		}
	} else {
		if strings.TrimSpace(*project) == "" || strings.TrimSpace(*summary) == "" {
			return errors.New("missing required --project or --summary")
		}
		body, err := readInlineOrFile(*description, *descriptionFile)
		if err != nil {
			return err
		}
		fields := map[string]any{
			"project":   map[string]string{"key": *project},
			"issuetype": map[string]string{"name": *issueType},
			"summary":   *summary,
		}
		if strings.TrimSpace(body) != "" {
			fields["description"] = body
		}
		if strings.TrimSpace(*parent) != "" {
			fields["parent"] = map[string]string{"key": strings.TrimSpace(*parent)}
		}
		payload = map[string]any{"fields": fields}
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*parent) != "" {
		resolvedType, err := client.ResolveProjectIssueType(context.Background(), *project, *issueType)
		if err != nil {
			return fmt.Errorf("validate subtask issue type: %w", err)
		}
		if !resolvedType.Subtask {
			return &jira.ValidationError{Field: "type", Message: "--parent requires a subtask issue type"}
		}
		fields := payload["fields"].(map[string]any)
		fields["issuetype"] = map[string]string{"id": resolvedType.ID}
	}
	if *dryRun {
		request, err := client.PlanCreateIssue(context.Background(), payload)
		if err != nil {
			return err
		}
		return writeDryRun(a.Stdout, request)
	}
	created, err := client.CreateIssueWithPayload(context.Background(), payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, created)
	}
	fmt.Fprintf(a.Stdout, "Created %s\n", created.Key)
	return nil
}

func (a App) runUpdate(args []string, configPath string) error {
	fs := newFlagSet("update")
	summary := fs.String("summary", "", "new summary")
	description := fs.String("description", "", "new description")
	descriptionFile := fs.String("description-file", "", "path to description file")
	parent := fs.String("parent", "", "new parent issue key on Jira Cloud")
	fieldValues := multiFlag{}
	inputPath := fs.String("input", "", "structured JSON input file, or - for stdin")
	dryRun := fs.Bool("dry-run", false, "print the exact Jira request without sending it")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	fs.Var(&fieldValues, "field", "field assignment as name=value; may be repeated")

	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl update ISSUE-123 [--summary '...'] [--description '...'] [--field name=value]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl update ISSUE-123 [--summary '...'] [--description '...'] [--field name=value]")
	}

	var payload map[string]any
	if strings.TrimSpace(*inputPath) != "" {
		if flagWasSet(fs, "summary", "description", "description-file", "field", "parent") {
			return errors.New("--input conflicts with update convenience flags")
		}
		var err error
		payload, err = a.readMutationInput(*inputPath)
		if err != nil {
			return err
		}
		if err := validateMutationEnvelope("update", payload); err != nil {
			return err
		}
	} else {
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
		if strings.TrimSpace(*parent) != "" {
			fields["parent"] = map[string]string{"key": strings.TrimSpace(*parent)}
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
		payload = map[string]any{"fields": fields}
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*parent) != "" {
		if err := client.ValidateParentUpdate(context.Background()); err != nil {
			return err
		}
	}
	if *dryRun {
		request, err := client.PlanUpdateIssue(context.Background(), fs.Arg(0), payload)
		if err != nil {
			return err
		}
		return writeDryRun(a.Stdout, request)
	}
	if err := client.UpdateIssueWithPayload(context.Background(), fs.Arg(0), payload); err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{
			"issue":   fs.Arg(0),
			"updated": true,
		})
	}
	fmt.Fprintf(a.Stdout, "Updated %s\n", fs.Arg(0))
	return nil
}

func (a App) runComment(args []string, configPath string) error {
	return a.runComments(append([]string{"add"}, args...), configPath)
}

func (a App) runTransitions(args []string, configPath string) error {
	fs := newFlagSet("transitions")
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl transitions ISSUE-123 [--json]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl transitions ISSUE-123 [--json]")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.Transitions(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	if *rawJSON {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, transition := range result.Transitions {
		fmt.Fprintf(a.Stdout, "%s  %s\n", transition.ID, transition.Name)
	}
	return nil
}

func (a App) runTransition(args []string, configPath string) error {
	fs := newFlagSet("transition")
	to := fs.String("to", "", "transition name or id")
	inputPath := fs.String("input", "", "structured JSON input file, or - for stdin")
	dryRun := fs.Bool("dry-run", false, "print the exact Jira request without sending it")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl transition ISSUE-123 --to 'In Progress'")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl transition ISSUE-123 (--to NAME_OR_ID | --input FILE|-) [--dry-run] [--json]")
	}

	payload := map[string]any{}
	if strings.TrimSpace(*inputPath) != "" {
		var err error
		payload, err = a.readMutationInput(*inputPath)
		if err != nil {
			return err
		}
		if err := validateMutationEnvelope("transition", payload); err != nil {
			return err
		}
		if _, hasTransition := payload["transition"]; hasTransition && strings.TrimSpace(*to) != "" {
			return errors.New("--to conflicts with --input transition")
		}
	}
	if strings.TrimSpace(*to) == "" {
		if _, ok := payload["transition"]; !ok {
			return errors.New("transition requires --to or --input with transition.id")
		}
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	selectedName := ""
	if strings.TrimSpace(*to) != "" {
		transitions, err := client.Transitions(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		id := ""
		for _, transition := range transitions.Transitions {
			if strings.EqualFold(transition.ID, *to) || strings.EqualFold(transition.Name, *to) {
				id = transition.ID
				selectedName = transition.Name
				break
			}
		}
		if id == "" {
			return fmt.Errorf("transition %q not found", *to)
		}
		payload["transition"] = map[string]any{"id": id}
	}

	if *dryRun {
		request, err := client.PlanTransitionIssue(context.Background(), fs.Arg(0), payload)
		if err != nil {
			return err
		}
		return writeDryRun(a.Stdout, request)
	}
	if err := client.TransitionIssueWithPayload(context.Background(), fs.Arg(0), payload); err != nil {
		return err
	}
	transitionID := payload["transition"].(map[string]any)["id"].(string)
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{
			"issue": fs.Arg(0),
			"transition": map[string]string{
				"id":   transitionID,
				"name": selectedName,
			},
			"transitioned": true,
		})
	}
	label := *to
	if strings.TrimSpace(label) == "" {
		label = transitionID
	}
	fmt.Fprintf(a.Stdout, "Transitioned %s via %s\n", fs.Arg(0), label)
	return nil
}

func (a App) runFields(args []string, configPath string) error {
	fs := newFlagSet("fields")
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl fields [--json]")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	fields, err := client.Fields(context.Background())
	if err != nil {
		return err
	}
	if *rawJSON {
		return writeSuccessJSON(a.Stdout, fields)
	}
	for _, field := range fields {
		fmt.Fprintf(a.Stdout, "%s  %s\n", field.ID, field.Name)
	}
	return nil
}

func (a App) runIssueFields(args []string, configPath string) error {
	fs := newFlagSet("issue-fields")
	rawJSON := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil {
		return errors.New("usage: jiractrl issue-fields ISSUE-123 [--json]")
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New("usage: jiractrl issue-fields ISSUE-123 [--json]")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	issue, err := client.GetIssueRaw(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	if *rawJSON {
		return writeSuccessJSON(a.Stdout, issue.Fields)
	}
	for name, value := range issue.Fields {
		if value != nil {
			fmt.Fprintf(a.Stdout, "%s\n", name)
		}
	}
	return nil
}

func (a App) runProfiles(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl profiles list|show NAME [--json]")
	}
	subcommand := args[0]
	fs := newFlagSet("profiles " + subcommand)
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil {
		return errors.New("usage: jiractrl profiles list|show NAME [--json]")
	}
	cfg, err := config.Load(configPath, 5*time.Second)
	if err != nil {
		return err
	}

	switch subcommand {
	case "list":
		if fs.NArg() != 0 {
			return errors.New("usage: jiractrl profiles list [--json]")
		}
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, names)
		}
		for _, name := range names {
			fmt.Fprintln(a.Stdout, name)
		}
		return nil
	case "show":
		if fs.NArg() != 1 {
			return errors.New("usage: jiractrl profiles show NAME [--json]")
		}
		name := fs.Arg(0)
		p, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("unknown profile %q", name)
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, map[string]any{
				"name":       name,
				"jql":        p.JQL,
				"fields":     p.Fields,
				"maxResults": p.MaxResults,
			})
		}
		fmt.Fprintf(a.Stdout, "%s\n", name)
		fmt.Fprintf(a.Stdout, "  jql: %s\n", p.JQL)
		if len(p.Fields) > 0 {
			fmt.Fprintf(a.Stdout, "  fields: %s\n", strings.Join(p.Fields, ", "))
		}
		if p.MaxResults > 0 {
			fmt.Fprintf(a.Stdout, "  max_results: %d\n", p.MaxResults)
		}
		return nil
	default:
		return errors.New("usage: jiractrl profiles list|show NAME [--json]")
	}
}

func (a App) runTriage(args []string, configPath string) error {
	fs := newFlagSet("triage")
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

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.Search(context.Background(), jira.SearchOptions{
		JQL:        *jql,
		Fields:     splitCommaValues(triage.Fields),
		MaxResults: *maxResults,
	})
	if err != nil {
		return err
	}

	report := make([]triage.Result, 0, len(result.Issues))
	for _, issue := range result.Issues {
		report = append(report, triage.Classify(issue))
	}
	if *rawJSON {
		return writeSuccessJSON(a.Stdout, report)
	}
	printTriageReport(a.Stdout, report)
	return nil
}

func (a App) client(configPath string, timeout time.Duration) (*jira.Client, error) {
	cfg, err := config.Load(configPath, timeout)
	if err != nil {
		return nil, err
	}
	return newJiraClient(cfg)
}

func newJiraClient(cfg config.Config) (*jira.Client, error) {
	deployment, err := jira.ParseDeployment(cfg.Deployment)
	if err != nil {
		return nil, err
	}
	client := jira.NewClient(cfg.BaseURL, cfg.Token, cfg.Email, deployment, cfg.Timeout)
	client.SetRetryPolicy(jira.RetryPolicy{
		MaxAttempts: cfg.RetryMaxAttempts,
		BaseDelay:   cfg.RetryBaseDelay,
		MaxDelay:    cfg.RetryMaxDelay,
	})
	return client, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
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

func parsePositiveIDs(values []string) ([]int64, error) {
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("invalid --reconcile issue ID %q: use a positive numeric Jira issue ID", value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func splitCommaValues(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func flagsBeforeLeadingPositional(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return args
	}
	reordered := append([]string(nil), args[1:]...)
	return append(reordered, args[0])
}

func flagWasSet(fs *flag.FlagSet, names ...string) bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		set[f.Name] = true
	})
	for _, name := range names {
		if set[name] {
			return true
		}
	}
	return false
}
