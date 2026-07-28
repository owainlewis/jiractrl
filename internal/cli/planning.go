package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

const softwareReadHardLimit = 10000

func (a App) runBoards(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl boards list|get|issues|backlog")
	}
	switch args[0] {
	case "list":
		return a.runBoardList(args[1:], configPath)
	case "get":
		return a.runBoardGet(args[1:], configPath)
	case "issues":
		return a.runSoftwareIssueRead("board", args[1:], configPath)
	case "backlog":
		return a.runSoftwareIssueRead("backlog", args[1:], configPath)
	default:
		return errors.New("usage: jiractrl boards list|get|issues|backlog")
	}
}

func (a App) runBoardList(args []string, configPath string) error {
	fs := newFlagSet("boards list")
	name := fs.String("name", "", "filter by board name")
	boardType := fs.String("type", "", "filter by scrum or kanban")
	project := fs.String("project", "", "filter by project key or ID")
	start := fs.Int("start", 0, "zero-based board offset")
	maxResults := fs.Int("max", 50, "maximum boards per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard board limit when --all is set")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("usage: jiractrl boards list [--name TEXT] [--type scrum|kanban] [--project KEY] [--start N] [--max 50] [--all --limit 1000] [--json]")
	}
	if err := validatePlanningPage(*start, *maxResults, *allResults, *limit, flagWasSet(fs, "limit")); err != nil {
		return err
	}
	boardKind := strings.ToLower(strings.TrimSpace(*boardType))
	if boardKind != "" && boardKind != "scrum" && boardKind != "kanban" {
		return errors.New("--type must be scrum or kanban")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := collectSoftwareBoards(context.Background(), client, *name, boardKind, *project, *start, *maxResults, *allResults, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, board := range result.Boards {
		fmt.Fprintf(a.Stdout, "%d  %s  %s\n", board.ID, board.Type, board.Name)
	}
	if result.Page.HasMore {
		fmt.Fprintf(a.Stdout, "More boards available at --start %d\n", result.Page.Next)
	}
	return nil
}

func (a App) runBoardGet(args []string, configPath string) error {
	fs := newFlagSet("boards get")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl boards get BOARD_ID [--json]")
	}
	boardID, err := parsePlanningID("board", fs.Arg(0))
	if err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.SoftwareBoard(context.Background(), boardID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	fmt.Fprintf(a.Stdout, "%d  %s  %s\n", result.ID, result.Type, result.Name)
	return nil
}

func (a App) runSprints(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl sprints list|get|issues|move")
	}
	switch args[0] {
	case "list":
		return a.runSprintList(args[1:], configPath)
	case "get":
		return a.runSprintGet(args[1:], configPath)
	case "issues":
		return a.runSoftwareIssueRead("sprint", args[1:], configPath)
	case "move":
		return a.runSprintMove(args[1:], configPath)
	default:
		return errors.New("usage: jiractrl sprints list|get|issues|move")
	}
}

func (a App) runSprintList(args []string, configPath string) error {
	fs := newFlagSet("sprints list")
	state := fs.String("state", "", "comma-separated active, future, or closed states")
	start := fs.Int("start", 0, "zero-based sprint offset")
	maxResults := fs.Int("max", 50, "maximum sprints per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard sprint limit when --all is set")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl sprints list BOARD_ID [--state active,future,closed] [--start N] [--max 50] [--all --limit 1000] [--json]")
	}
	if err := validatePlanningPage(*start, *maxResults, *allResults, *limit, flagWasSet(fs, "limit")); err != nil {
		return err
	}
	boardID, err := parsePlanningID("board", fs.Arg(0))
	if err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := collectSoftwareSprints(context.Background(), client, boardID, *state, *start, *maxResults, *allResults, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, sprint := range result.Sprints {
		fmt.Fprintf(a.Stdout, "%d  %s  %s\n", sprint.ID, sprint.State, sprint.Name)
	}
	if result.Page.HasMore {
		fmt.Fprintf(a.Stdout, "More sprints available at --start %d\n", result.Page.Next)
	}
	return nil
}

func (a App) runSprintGet(args []string, configPath string) error {
	fs := newFlagSet("sprints get")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl sprints get SPRINT_ID [--json]")
	}
	sprintID, err := parsePlanningID("sprint", fs.Arg(0))
	if err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.SoftwareSprint(context.Background(), sprintID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	fmt.Fprintf(a.Stdout, "%d  %s  %s\n", result.ID, result.State, result.Name)
	return nil
}

func (a App) runSoftwareIssueRead(scope string, args []string, configPath string) error {
	fs := newFlagSet(scope + " issues")
	cursor := fs.String("cursor", "", "opaque continuation cursor")
	maxResults := fs.Int("max", 50, "maximum issues per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard issue limit when --all is set")
	jql := fs.String("jql", "", "additional JQL filter")
	fields := fs.String("fields", "summary,status,assignee,priority,issuetype", "comma-separated fields")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return fmt.Errorf("usage: jiractrl %s ID [--cursor CURSOR] [--max 50] [--all --limit 1000] [--jql JQL] [--fields LIST] [--json]", planningScopeCommand(scope))
	}
	if err := validatePlanningPage(0, *maxResults, *allResults, *limit, flagWasSet(fs, "limit")); err != nil {
		return err
	}
	idName := "board"
	if scope == "sprint" {
		idName = "sprint"
	}
	id, err := parsePlanningID(idName, fs.Arg(0))
	if err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := collectSoftwareIssues(context.Background(), client, scope, id, jira.SoftwareIssueOptions{
		MaxResults: *maxResults,
		Cursor:     *cursor,
		JQL:        *jql,
		Fields:     splitCommaValues(*fields),
	}, *allResults, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, issue := range result.Issues {
		summary, _ := issue.Fields["summary"].(string)
		fmt.Fprintf(a.Stdout, "%s  %s\n", issue.Key, summary)
	}
	if result.Page.HasMore {
		fmt.Fprintf(a.Stdout, "More issues available at --cursor %s\n", result.Page.Next)
	}
	return nil
}

func (a App) runSprintMove(args []string, configPath string) error {
	fs := newFlagSet("sprints move")
	var issueValues multiFlag
	fs.Var(&issueValues, "issue", "issue key or ID; may be repeated or comma-separated")
	before := fs.String("before", "", "rank moved issues before this issue")
	after := fs.String("after", "", "rank moved issues after this issue")
	rankField := fs.Int64("rank-field", 0, "optional numeric rank custom field ID")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl sprints move SPRINT_ID --issue ISSUE [--issue ISSUE] [--before ISSUE|--after ISSUE] [--rank-field ID] [--json]")
	}
	sprintID, err := parsePlanningID("sprint", fs.Arg(0))
	if err != nil {
		return err
	}
	issues := planningIssueValues(issueValues)
	if err := validatePlanningWrite(issues, *before, *after, *rankField, false); err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.MoveIssuesToSprint(context.Background(), sprintID, issues, *before, *after, *rankField)
	if err != nil {
		return err
	}
	return a.printSoftwareWrite(result, *jsonOutput)
}

func (a App) runBacklog(args []string, configPath string) error {
	if len(args) == 0 || args[0] != "move" {
		return errors.New("usage: jiractrl backlog move --issue ISSUE [--board BOARD_ID] [--json]")
	}
	fs := newFlagSet("backlog move")
	var issueValues multiFlag
	fs.Var(&issueValues, "issue", "issue key or ID; may be repeated or comma-separated")
	board := fs.String("board", "", "optional board ID for board-scoped backlog")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errors.New("usage: jiractrl backlog move --issue ISSUE [--issue ISSUE] [--board BOARD_ID] [--json]")
	}
	issues := planningIssueValues(issueValues)
	if len(issues) < 1 || len(issues) > jira.SoftwareWriteIssueLimit {
		return fmt.Errorf("provide between 1 and %d --issue values", jira.SoftwareWriteIssueLimit)
	}
	var boardID int64
	var err error
	if strings.TrimSpace(*board) != "" {
		boardID, err = parsePlanningID("board", *board)
		if err != nil {
			return err
		}
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.MoveIssuesToBacklog(context.Background(), boardID, issues)
	if err != nil {
		return err
	}
	return a.printSoftwareWrite(result, *jsonOutput)
}

func (a App) runRank(args []string, configPath string) error {
	fs := newFlagSet("rank")
	var issueValues multiFlag
	fs.Var(&issueValues, "issue", "issue key or ID; may be repeated or comma-separated")
	before := fs.String("before", "", "rank before this issue")
	after := fs.String("after", "", "rank after this issue")
	rankField := fs.Int64("rank-field", 0, "optional numeric rank custom field ID")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("usage: jiractrl rank --issue ISSUE [--issue ISSUE] (--before ISSUE|--after ISSUE) [--rank-field ID] [--json]")
	}
	issues := planningIssueValues(issueValues)
	if err := validatePlanningWrite(issues, *before, *after, *rankField, true); err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.RankIssues(context.Background(), issues, *before, *after, *rankField)
	if err != nil {
		return err
	}
	return a.printSoftwareWrite(result, *jsonOutput)
}

func (a App) runEstimate(args []string, configPath string) error {
	fs := newFlagSet("estimate")
	board := fs.String("board", "", "board ID that selects the estimation field")
	value := fs.String("value", "", "new Jira Software estimate value")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 ||
		strings.TrimSpace(*board) == "" || strings.TrimSpace(*value) == "" {
		return errors.New("usage: jiractrl estimate ISSUE --board BOARD_ID --value VALUE [--json]")
	}
	boardID, err := parsePlanningID("board", *board)
	if err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.EstimateIssue(context.Background(), fs.Arg(0), boardID, *value)
	if err != nil {
		return err
	}
	if *jsonOutput {
		var data any
		if len(result) == 0 {
			data = map[string]any{
				"issue": fs.Arg(0), "boardId": boardID, "value": *value, "accepted": true,
			}
		} else if err := json.Unmarshal(result, &data); err != nil {
			return fmt.Errorf("decode Jira estimate response: %w", err)
		}
		return writeSuccessJSON(a.Stdout, data)
	}
	fmt.Fprintf(a.Stdout, "Estimated %s on board %d as %s\n", fs.Arg(0), boardID, *value)
	return nil
}

func (a App) printSoftwareWrite(result *jira.SoftwareWriteReceipt, jsonOutput bool) error {
	if result.Partial {
		partialErr := errors.New("Jira Software partially completed the requested operation")
		if jsonOutput {
			classified := contractError{
				Kind: "partial_failure", Message: partialErr.Error(), Status: http.StatusMultiStatus,
			}
			if err := writeJSON(a.Stdout, struct {
				OK    bool                       `json:"ok"`
				Data  *jira.SoftwareWriteReceipt `json:"data"`
				Error contractError              `json:"error"`
			}{OK: false, Data: result, Error: classified}); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(a.Stdout, "%s partially completed for %d issues\n%s\n", result.Operation, len(result.Issues), result.Details)
		}
		return &reportedError{err: partialErr}
	}
	if jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	fmt.Fprintf(a.Stdout, "%s accepted for %d issues\n", result.Operation, len(result.Issues))
	return nil
}

func collectSoftwareBoards(ctx context.Context, client *jira.Client, name, boardType, project string, start, maxResults int, allResults bool, limit int) (*jira.SoftwareBoardPage, error) {
	result := &jira.SoftwareBoardPage{}
	first := start
	requestMax := min(maxResults, limit)
	for {
		page, err := client.SoftwareBoards(ctx, name, boardType, project, start, requestMax)
		if err != nil {
			return nil, err
		}
		result.Boards = append(result.Boards, page.Boards...)
		result.Page = page.Page
		if !allResults || !page.Page.HasMore || len(result.Boards) >= limit {
			break
		}
		if page.Page.Next <= start || len(page.Boards) == 0 {
			return nil, errors.New("Jira Software returned a non-advancing board page")
		}
		start = page.Page.Next
		requestMax = min(maxResults, limit-len(result.Boards))
	}
	result.Page.StartAt = first
	result.Page.MaxResults = maxResults
	result.Page.Returned = len(result.Boards)
	return result, nil
}

func collectSoftwareSprints(ctx context.Context, client *jira.Client, boardID int64, state string, start, maxResults int, allResults bool, limit int) (*jira.SoftwareSprintPage, error) {
	result := &jira.SoftwareSprintPage{}
	first := start
	requestMax := min(maxResults, limit)
	for {
		page, err := client.SoftwareBoardSprints(ctx, boardID, state, start, requestMax)
		if err != nil {
			return nil, err
		}
		result.Sprints = append(result.Sprints, page.Sprints...)
		result.Page = page.Page
		if !allResults || !page.Page.HasMore || len(result.Sprints) >= limit {
			break
		}
		if page.Page.Next <= start || len(page.Sprints) == 0 {
			return nil, errors.New("Jira Software returned a non-advancing sprint page")
		}
		start = page.Page.Next
		requestMax = min(maxResults, limit-len(result.Sprints))
	}
	result.Page.StartAt = first
	result.Page.MaxResults = maxResults
	result.Page.Returned = len(result.Sprints)
	return result, nil
}

func collectSoftwareIssues(ctx context.Context, client *jira.Client, scope string, id int64, options jira.SoftwareIssueOptions, allResults bool, limit int) (*jira.SoftwareIssuePage, error) {
	result := &jira.SoftwareIssuePage{}
	cursor := options.Cursor
	pageSize := options.MaxResults
	requestMax := min(pageSize, limit)
	pages := 0
	for {
		options.Cursor = cursor
		options.MaxResults = requestMax
		var page *jira.SoftwareIssuePage
		var err error
		switch scope {
		case "board":
			page, err = client.SoftwareBoardIssues(ctx, id, false, options)
		case "backlog":
			page, err = client.SoftwareBoardIssues(ctx, id, true, options)
		case "sprint":
			page, err = client.SoftwareSprintIssues(ctx, id, options)
		default:
			return nil, fmt.Errorf("unsupported Jira Software issue scope %q", scope)
		}
		if err != nil {
			return nil, err
		}
		pages++
		result.Issues = append(result.Issues, page.Issues...)
		result.Page = page.Page
		if !allResults || !page.Page.HasMore || len(result.Issues) >= limit {
			break
		}
		if page.Page.Next == "" || page.Page.Next == cursor || len(page.Issues) == 0 {
			return nil, errors.New("Jira Software returned a non-advancing issue page")
		}
		cursor = page.Page.Next
		requestMax = min(pageSize, limit-len(result.Issues))
	}
	result.Page.Returned = len(result.Issues)
	result.Page.Limit = pageSize
	result.Page.Pages = pages
	return result, nil
}

func validatePlanningPage(start, maxResults int, allResults bool, limit int, limitSet bool) error {
	if start < 0 || maxResults < 1 || maxResults > jira.SoftwareReadPageLimit {
		return fmt.Errorf("--start must be non-negative and --max must be between 1 and %d", jira.SoftwareReadPageLimit)
	}
	if limit < 1 || limit > softwareReadHardLimit {
		return fmt.Errorf("--limit must be between 1 and %d", softwareReadHardLimit)
	}
	if limitSet && !allResults {
		return errors.New("--limit requires --all")
	}
	return nil
}

func validatePlanningWrite(issues []string, before, after string, rankField int64, requireRank bool) error {
	if len(issues) < 1 || len(issues) > jira.SoftwareWriteIssueLimit {
		return fmt.Errorf("provide between 1 and %d --issue values", jira.SoftwareWriteIssueLimit)
	}
	beforeSet := strings.TrimSpace(before) != ""
	afterSet := strings.TrimSpace(after) != ""
	if beforeSet && afterSet {
		return errors.New("use only one of --before or --after")
	}
	if requireRank && !beforeSet && !afterSet {
		return errors.New("rank requires exactly one of --before or --after")
	}
	if rankField < 0 {
		return errors.New("--rank-field must be non-negative")
	}
	return nil
}

func parsePlanningID(name, value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%s ID must be a positive integer", name)
	}
	return id, nil
}

func planningIssueValues(values []string) []string {
	var issues []string
	for _, value := range values {
		issues = append(issues, splitCommaValues(value)...)
	}
	return issues
}

func planningScopeCommand(scope string) string {
	switch scope {
	case "board":
		return "boards issues BOARD_ID"
	case "backlog":
		return "boards backlog BOARD_ID"
	case "sprint":
		return "sprints issues SPRINT_ID"
	default:
		return scope
	}
}
