package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (a App) runLinks(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl links types|list|add|remove")
	}
	switch args[0] {
	case "types":
		fs := newFlagSet("links types")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("usage: jiractrl links types [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		types, err := client.IssueLinkTypes(context.Background())
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, map[string]any{"types": types})
		}
		for _, linkType := range types {
			fmt.Fprintf(a.Stdout, "%s  %s  inward=%q  outward=%q\n",
				linkType.ID, linkType.Name, linkType.Inward, linkType.Outward)
		}
		return nil
	case "list":
		fs := newFlagSet("links list")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl links list ISSUE [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		links, err := client.IssueLinks(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, map[string]any{"issue": fs.Arg(0), "links": links})
		}
		for _, link := range links {
			fmt.Fprintf(a.Stdout, "%s  %s  --%s:%s-->  %s\n",
				link.ID, fs.Arg(0), link.Direction, link.Relation, link.Issue.Key)
		}
		return nil
	case "add":
		fs := newFlagSet("links add")
		linkType := fs.String("type", "", "exact Jira link type name")
		outward := fs.String("outward", "", "outward/source issue key")
		inward := fs.String("inward", "", "inward/target issue key")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 ||
			strings.TrimSpace(*linkType) == "" ||
			strings.TrimSpace(*outward) == "" ||
			strings.TrimSpace(*inward) == "" {
			return errors.New("usage: jiractrl links add --type NAME --outward ISSUE --inward ISSUE [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		receipt, err := client.AddIssueLink(context.Background(), *linkType, *outward, *inward)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, receipt)
		}
		fmt.Fprintf(a.Stdout,
			"Jira accepted %s --%s--> %s; duplicate link requests also succeed and no link ID is returned\n",
			*outward, *linkType, *inward)
		return nil
	case "remove":
		fs := newFlagSet("links remove")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl links remove LINK_ID [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		if err := client.RemoveIssueLink(context.Background(), fs.Arg(0)); err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, map[string]any{"linkId": fs.Arg(0), "removed": true})
		}
		fmt.Fprintf(a.Stdout, "Removed issue link %s\n", fs.Arg(0))
		return nil
	default:
		return errors.New("usage: jiractrl links types|list|add|remove")
	}
}
