package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

const (
	defaultBulkMaxItems = 50
	maxBulkItems        = 1000
	maxBulkInputBytes   = 8 << 20
)

type bulkInputItem struct {
	Index    int
	Identity string
	Issue    string
	Payload  map[string]any
}

type bulkItemResult struct {
	Index    int            `json:"index"`
	Identity string         `json:"identity"`
	Issue    string         `json:"issue,omitempty"`
	Status   string         `json:"status"`
	Success  bool           `json:"success"`
	Data     any            `json:"data,omitempty"`
	Error    *contractError `json:"error,omitempty"`
}

type bulkResult struct {
	Operation string           `json:"operation"`
	Requested int              `json:"requested"`
	Processed int              `json:"processed"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Skipped   int              `json:"skipped"`
	DryRun    bool             `json:"dryRun"`
	Stopped   bool             `json:"stopped"`
	Complete  bool             `json:"complete"`
	Results   []bulkItemResult `json:"results"`
}

type bulkEnvelope struct {
	OK    bool           `json:"ok"`
	Data  *bulkResult    `json:"data"`
	Error *contractError `json:"error,omitempty"`
}

type bulkPartialError struct {
	result *bulkResult
}

func (e *bulkPartialError) Error() string {
	return fmt.Sprintf("bulk %s completed with %d failed and %d skipped items",
		e.result.Operation, e.result.Failed, e.result.Skipped)
}

func (a App) runBulk(args []string, configPath string) error {
	if len(args) == 0 || (args[0] != "create" && args[0] != "update" && args[0] != "transition") {
		return errors.New("usage: jiractrl bulk create|update|transition --input FILE|- [--max-items 50] [--dry-run] [--json]")
	}
	operation := args[0]
	fs := newFlagSet("bulk " + operation)
	input := fs.String("input", "", "JSON array or JSONL file, or - for stdin")
	localMax := fs.Int("max-items", defaultBulkMaxItems, "maximum input items accepted locally")
	dryRun := fs.Bool("dry-run", false, "plan every item without mutation")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || strings.TrimSpace(*input) == "" {
		return errors.New("usage: jiractrl bulk create|update|transition --input FILE|- [--max-items 50] [--dry-run] [--json]")
	}
	if *localMax < 1 || *localMax > maxBulkItems {
		return fmt.Errorf("--max-items must be between 1 and %d", maxBulkItems)
	}
	items, err := a.readBulkInput(*input, *localMax)
	if err != nil {
		return err
	}
	if err := validateBulkItems(operation, items); err != nil {
		return err
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result := &bulkResult{
		Operation: operation,
		Requested: len(items),
		DryRun:    *dryRun,
		Complete:  true,
		Results:   make([]bulkItemResult, 0, len(items)),
	}
	ctx := context.Background()
	for i, item := range items {
		entry := bulkItemResult{Index: item.Index, Identity: item.Identity, Issue: item.Issue}
		var data any
		var itemErr error
		if *dryRun {
			entry.Status = "planned"
			entry.Success = true
			data, itemErr = planBulkItem(ctx, client, operation, item)
		} else {
			entry.Status = "succeeded"
			entry.Success = true
			data, itemErr = executeBulkItem(ctx, client, operation, item)
		}
		if itemErr != nil {
			entry.Status = "failed"
			entry.Success = false
			classified := classifyError(itemErr)
			entry.Error = &classified
			result.Failed++
			result.Complete = false
		} else {
			entry.Data = data
			result.Succeeded++
		}
		result.Processed++
		result.Results = append(result.Results, entry)

		if itemErr != nil && ambiguousBulkFailure(itemErr) {
			result.Stopped = true
			for _, remaining := range items[i+1:] {
				skippedError := contractError{
					Kind:    "skipped",
					Message: "not attempted after an ambiguous mutation failure",
				}
				result.Results = append(result.Results, bulkItemResult{
					Index: remaining.Index, Identity: remaining.Identity, Issue: remaining.Issue,
					Status: "skipped", Success: false, Error: &skippedError,
				})
				result.Skipped++
			}
			break
		}
	}

	if *jsonOutput || *dryRun {
		envelope := bulkEnvelope{OK: result.Complete, Data: result}
		if !result.Complete {
			partial := contractError{
				Kind:    "partial_failure",
				Message: (&bulkPartialError{result: result}).Error(),
			}
			envelope.Error = &partial
		}
		if err := writeJSON(a.Stdout, envelope); err != nil {
			return err
		}
	} else {
		for _, item := range result.Results {
			fmt.Fprintf(a.Stdout, "%d  %s  %s\n", item.Index, item.Identity, item.Status)
			if item.Error != nil {
				if item.Error.Status != 0 {
					fmt.Fprintf(a.Stdout, "  HTTP %d: %s\n", item.Error.Status, item.Error.Message)
				} else {
					fmt.Fprintf(a.Stdout, "  %s: %s\n", item.Error.Kind, item.Error.Message)
				}
			}
		}
		fmt.Fprintf(a.Stdout, "Bulk %s: %d succeeded, %d failed, %d skipped\n",
			operation, result.Succeeded, result.Failed, result.Skipped)
	}
	if result.Failed > 0 || result.Skipped > 0 {
		return &reportedError{err: &bulkPartialError{result: result}}
	}
	return nil
}

func (a App) readBulkInput(path string, limit int) ([]bulkInputItem, error) {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = a.Stdin
		if reader == nil {
			reader = os.Stdin
		}
	} else {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read --input: %w", err)
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBulkInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read --input: %w", err)
	}
	if len(data) > maxBulkInputBytes {
		return nil, fmt.Errorf("--input exceeds the %d-byte limit", maxBulkInputBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("--input is empty")
	}

	var objects []map[string]any
	if trimmed[0] == '[' {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&objects); err != nil {
			return nil, fmt.Errorf("parse --input JSON array: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, errors.New("--input must contain one JSON array")
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(trimmed))
		scanner.Buffer(make([]byte, 64*1024), maxBulkInputBytes)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			decoder := json.NewDecoder(bytes.NewReader(line))
			decoder.UseNumber()
			var object map[string]any
			if err := decoder.Decode(&object); err != nil {
				return nil, fmt.Errorf("parse JSONL item %d: %w", len(objects), err)
			}
			var trailing any
			if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("JSONL item %d must contain one object", len(objects))
			}
			objects = append(objects, object)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read JSONL input: %w", err)
		}
	}
	if len(objects) == 0 {
		return nil, errors.New("--input contains no items")
	}
	if len(objects) > limit {
		return nil, fmt.Errorf("bulk input has %d items, exceeding --max-items %d", len(objects), limit)
	}
	items := make([]bulkInputItem, len(objects))
	for i, object := range objects {
		if object == nil {
			return nil, fmt.Errorf("bulk item %d must be a JSON object", i)
		}
		identity := fmt.Sprintf("item-%d", i)
		if value, ok := object["identity"]; ok {
			stringValue, ok := value.(string)
			if !ok || strings.TrimSpace(stringValue) == "" {
				return nil, fmt.Errorf("bulk item %d identity must be a non-empty string", i)
			}
			identity = stringValue
			delete(object, "identity")
		}
		issue := ""
		if value, ok := object["issue"]; ok {
			stringValue, ok := value.(string)
			if !ok || strings.TrimSpace(stringValue) == "" {
				return nil, fmt.Errorf("bulk item %d issue must be a non-empty string", i)
			}
			issue = stringValue
			delete(object, "issue")
		}
		items[i] = bulkInputItem{Index: i, Identity: identity, Issue: issue, Payload: object}
	}
	return items, nil
}

func validateBulkItems(operation string, items []bulkInputItem) error {
	for _, item := range items {
		if operation == "create" {
			if item.Issue != "" {
				return fmt.Errorf("bulk item %d: create does not accept issue", item.Index)
			}
		} else if item.Issue == "" {
			return fmt.Errorf("bulk item %d: %s requires issue", item.Index, operation)
		}
		if err := validateMutationEnvelope(operation, item.Payload); err != nil {
			return fmt.Errorf("bulk item %d: %w", item.Index, err)
		}
		if operation == "transition" {
			if _, ok := item.Payload["transition"]; !ok {
				return fmt.Errorf("bulk item %d: transition requires transition.id", item.Index)
			}
		}
	}
	return nil
}

func planBulkItem(ctx context.Context, client *jira.Client, operation string, item bulkInputItem) (any, error) {
	switch operation {
	case "create":
		return client.PlanCreateIssue(ctx, item.Payload)
	case "update":
		return client.PlanUpdateIssue(ctx, item.Issue, item.Payload)
	case "transition":
		return client.PlanTransitionIssue(ctx, item.Issue, item.Payload)
	default:
		return nil, fmt.Errorf("unsupported bulk operation %q", operation)
	}
}

func executeBulkItem(ctx context.Context, client *jira.Client, operation string, item bulkInputItem) (any, error) {
	switch operation {
	case "create":
		return client.CreateIssueWithPayload(ctx, item.Payload)
	case "update":
		if err := client.UpdateIssueWithPayload(ctx, item.Issue, item.Payload); err != nil {
			return nil, err
		}
		return map[string]any{"issue": item.Issue, "updated": true}, nil
	case "transition":
		if err := client.TransitionIssueWithPayload(ctx, item.Issue, item.Payload); err != nil {
			return nil, err
		}
		return map[string]any{"issue": item.Issue, "transitioned": true}, nil
	default:
		return nil, fmt.Errorf("unsupported bulk operation %q", operation)
	}
}

func ambiguousBulkFailure(err error) bool {
	var jiraErr *jira.Error
	if !errors.As(err, &jiraErr) {
		return true
	}
	return jiraErr.StatusCode == http.StatusRequestTimeout ||
		jiraErr.StatusCode == 499 ||
		jiraErr.StatusCode == http.StatusTooManyRequests ||
		jiraErr.StatusCode >= 500
}
