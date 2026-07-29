package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

func (a App) runJSM(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl jsm service-desks|queues|request-types|requests|comments|participants|slas")
	}
	switch args[0] {
	case "service-desks":
		return a.runJSMServiceDesks(args[1:], configPath)
	case "queues":
		return a.runJSMQueues(args[1:], configPath)
	case "request-types":
		return a.runJSMRequestTypes(args[1:], configPath)
	case "requests":
		return a.runJSMRequests(args[1:], configPath)
	case "comments":
		return a.runJSMComments(args[1:], configPath)
	case "participants":
		return a.runJSMParticipants(args[1:], configPath)
	case "slas":
		return a.runJSMSLAs(args[1:], configPath)
	default:
		return errors.New("usage: jiractrl jsm service-desks|queues|request-types|requests|comments|participants|slas")
	}
}

func (a App) runJSMServiceDesks(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl jsm service-desks list|get")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("jsm service-desks list")
		start := fs.Int("start", 0, "zero-based offset")
		maxResults := fs.Int("max", 50, "maximum results")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("usage: jiractrl jsm service-desks list [--start N] [--max 50] [--json]")
		}
		if err := validateJSMPageFlags(*start, *maxResults); err != nil {
			return err
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.JSMServiceDesks(context.Background(), *start, *maxResults)
		return a.printJSMResult(result, *jsonOutput, err)
	case "get":
		fs := newFlagSet("jsm service-desks get")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl jsm service-desks get SERVICE_DESK_ID [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.JSMServiceDesk(context.Background(), fs.Arg(0))
		return a.printJSMResult(result, *jsonOutput, err)
	default:
		return errors.New("usage: jiractrl jsm service-desks list|get")
	}
}

func (a App) runJSMQueues(args []string, configPath string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: jiractrl jsm queues list SERVICE_DESK_ID [--include-count]")
	}
	fs := newFlagSet("jsm queues list")
	start := fs.Int("start", 0, "zero-based offset")
	maxResults := fs.Int("max", 50, "maximum results")
	includeCount := fs.Bool("include-count", false, "include issue counts")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl jsm queues list SERVICE_DESK_ID [--start N] [--max 50] [--include-count] [--json]")
	}
	if err := validateJSMPageFlags(*start, *maxResults); err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.JSMQueues(context.Background(), fs.Arg(0), *start, *maxResults, *includeCount)
	return a.printJSMResult(result, *jsonOutput, err)
}

func (a App) runJSMRequestTypes(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl jsm request-types list|fields")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("jsm request-types list")
		start := fs.Int("start", 0, "zero-based offset")
		maxResults := fs.Int("max", 50, "maximum results")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl jsm request-types list SERVICE_DESK_ID [--start N] [--max 50] [--json]")
		}
		if err := validateJSMPageFlags(*start, *maxResults); err != nil {
			return err
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.JSMRequestTypes(context.Background(), fs.Arg(0), *start, *maxResults)
		return a.printJSMResult(result, *jsonOutput, err)
	case "fields":
		fs := newFlagSet("jsm request-types fields")
		serviceDesk := fs.String("service-desk", "", "service desk ID")
		requestType := fs.String("request-type", "", "request type ID")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 ||
			strings.TrimSpace(*serviceDesk) == "" || strings.TrimSpace(*requestType) == "" {
			return errors.New("usage: jiractrl jsm request-types fields --service-desk ID --request-type ID [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.JSMRequestTypeFields(context.Background(), *serviceDesk, *requestType)
		return a.printJSMResult(result, *jsonOutput, err)
	default:
		return errors.New("usage: jiractrl jsm request-types list|fields")
	}
}

func (a App) runJSMRequests(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl jsm requests list|get|create")
	}
	switch args[0] {
	case "list":
		return a.runJSMRequestList(args[1:], configPath)
	case "get":
		return a.runJSMRequestGet(args[1:], configPath)
	case "create":
		return a.runJSMRequestCreate(args[1:], configPath)
	default:
		return errors.New("usage: jiractrl jsm requests list|get|create")
	}
}

func (a App) runJSMRequestList(args []string, configPath string) error {
	fs := newFlagSet("jsm requests list")
	start := fs.Int("start", 0, "zero-based offset")
	maxResults := fs.Int("max", 50, "maximum results")
	serviceDesk := fs.String("service-desk", "", "filter by service desk ID")
	requestType := fs.String("request-type", "", "filter by request type ID")
	status := fs.String("status", "", "filter by request status")
	ownership := fs.String("ownership", "", "filter by request ownership")
	search := fs.String("search", "", "search request text")
	expand := fs.String("expand", "", "comma-separated expansions; defaults to participant,status,sla,requestType,serviceDesk")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("usage: jiractrl jsm requests list [--service-desk ID] [--request-type ID] [--status STATUS] [--ownership VALUE] [--search TEXT] [--expand LIST] [--start N] [--max 50] [--json]")
	}
	if err := validateJSMPageFlags(*start, *maxResults); err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.JSMRequests(context.Background(), jira.JSMRequestOptions{
		Start: *start, Limit: *maxResults, ServiceDeskID: *serviceDesk,
		RequestTypeID: *requestType, RequestStatus: *status,
		RequestOwnership: *ownership, SearchTerm: *search, Expand: *expand,
	})
	return a.printJSMResult(result, *jsonOutput, err)
}

func (a App) runJSMRequestGet(args []string, configPath string) error {
	fs := newFlagSet("jsm requests get")
	expand := fs.String("expand", "", "comma-separated expansions; defaults to participant,status,sla,requestType,serviceDesk")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl jsm requests get ISSUE [--expand LIST] [--json]")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.JSMRequest(context.Background(), fs.Arg(0), *expand)
	return a.printJSMResult(result, *jsonOutput, err)
}

func (a App) runJSMRequestCreate(args []string, configPath string) error {
	fs := newFlagSet("jsm requests create")
	serviceDesk := fs.String("service-desk", "", "service desk ID")
	requestType := fs.String("request-type", "", "request type ID")
	input := fs.String("input", "", "JSON request field values file, or - for stdin")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 ||
		strings.TrimSpace(*serviceDesk) == "" || strings.TrimSpace(*requestType) == "" ||
		strings.TrimSpace(*input) == "" {
		return errors.New("usage: jiractrl jsm requests create --service-desk ID --request-type ID --input FILE|- [--json]")
	}
	fieldValues, err := a.readMutationInput(*input)
	if err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.CreateJSMRequest(context.Background(), *serviceDesk, *requestType, fieldValues)
	return a.printJSMResult(result, *jsonOutput, err)
}

func (a App) runJSMComments(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl jsm comments list|add")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("jsm comments list")
		start := fs.Int("start", 0, "zero-based offset")
		maxResults := fs.Int("max", 50, "maximum results")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl jsm comments list ISSUE [--start N] [--max 50] [--json]")
		}
		if err := validateJSMPageFlags(*start, *maxResults); err != nil {
			return err
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.JSMRequestComments(context.Background(), fs.Arg(0), *start, *maxResults)
		return a.printJSMResult(result, *jsonOutput, err)
	case "add":
		fs := newFlagSet("jsm comments add")
		body := fs.String("body", "", "comment body")
		bodyFile := fs.String("body-file", "", "read comment body from file")
		visibility := fs.String("visibility", "", "required: public or internal")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl jsm comments add ISSUE (--body TEXT|--body-file PATH) --visibility public|internal [--json]")
		}
		if (*body != "") == (*bodyFile != "") {
			return errors.New("set exactly one of --body or --body-file")
		}
		commentBody, err := readInlineOrFile(*body, *bodyFile)
		if err != nil {
			return err
		}
		visibilityValue := strings.ToLower(strings.TrimSpace(*visibility))
		if visibilityValue != "public" && visibilityValue != "internal" {
			return errors.New("--visibility must explicitly be public or internal")
		}
		public := visibilityValue == "public"
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.AddJSMRequestComment(context.Background(), fs.Arg(0), commentBody, &public)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, map[string]any{
				"visibility": visibilityValue,
				"comment":    result,
			})
		}
		fmt.Fprintf(a.Stdout, "Added %s comment to %s\n", visibilityValue, fs.Arg(0))
		return nil
	default:
		return errors.New("usage: jiractrl jsm comments list|add")
	}
}

func (a App) runJSMParticipants(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl jsm participants list|add|remove")
	}
	operation := args[0]
	if operation == "list" {
		fs := newFlagSet("jsm participants list")
		start := fs.Int("start", 0, "zero-based offset")
		maxResults := fs.Int("max", 50, "maximum results")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl jsm participants list ISSUE [--start N] [--max 50] [--json]")
		}
		if err := validateJSMPageFlags(*start, *maxResults); err != nil {
			return err
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.JSMRequestParticipants(context.Background(), fs.Arg(0), *start, *maxResults)
		return a.printJSMResult(result, *jsonOutput, err)
	}
	if operation != "add" && operation != "remove" {
		return errors.New("usage: jiractrl jsm participants list|add|remove")
	}
	fs := newFlagSet("jsm participants " + operation)
	var accountIDs, usernames multiFlag
	fs.Var(&accountIDs, "account-id", "Cloud account ID; may be repeated or comma-separated")
	fs.Var(&usernames, "username", "Data Center username; may be repeated or comma-separated")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
		return fmt.Errorf("usage: jiractrl jsm participants %s ISSUE (--account-id ID|--username USER) [--json]", operation)
	}
	if len(accountIDs) == 0 && len(usernames) == 0 {
		return errors.New("provide at least one --account-id or --username")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.ChangeJSMRequestParticipants(context.Background(), fs.Arg(0), jira.JSMParticipantInput{
		AccountIDs: accountIDs, Usernames: usernames,
	}, operation == "remove")
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{
			"operation": operation,
			"response":  result,
		})
	}
	verb := "Added"
	if operation == "remove" {
		verb = "Removed"
	}
	fmt.Fprintf(a.Stdout, "%s request participants on %s\n", verb, fs.Arg(0))
	return nil
}

func (a App) runJSMSLAs(args []string, configPath string) error {
	if len(args) == 0 || args[0] != "list" {
		return errors.New("usage: jiractrl jsm slas list ISSUE [--start N] [--max 50] [--json]")
	}
	fs := newFlagSet("jsm slas list")
	start := fs.Int("start", 0, "zero-based offset")
	maxResults := fs.Int("max", 50, "maximum results")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl jsm slas list ISSUE [--start N] [--max 50] [--json]")
	}
	if err := validateJSMPageFlags(*start, *maxResults); err != nil {
		return err
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.JSMRequestSLAs(context.Background(), fs.Arg(0), *start, *maxResults)
	return a.printJSMResult(result, *jsonOutput, err)
}

func (a App) printJSMResult(result json.RawMessage, jsonOutput bool, err error) error {
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	return printPrettyJSMJSON(a.Stdout, result)
}

func printPrettyJSMJSON(w io.Writer, result json.RawMessage) error {
	if len(result) == 0 {
		fmt.Fprintln(w, "Accepted.")
		return nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(result)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func validateJSMPageFlags(start, maxResults int) error {
	if start < 0 || maxResults < 1 || maxResults > jira.JSMReadPageLimit {
		return fmt.Errorf("--start must be non-negative and --max must be between 1 and %d", jira.JSMReadPageLimit)
	}
	return nil
}
