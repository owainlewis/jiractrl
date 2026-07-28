package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a App) runAttachments(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl attachments list|upload|download|remove")
	}
	switch args[0] {
	case "list":
		return a.runAttachmentsList(args[1:], configPath)
	case "upload":
		return a.runAttachmentsUpload(args[1:], configPath)
	case "download":
		return a.runAttachmentsDownload(args[1:], configPath)
	case "remove":
		return a.runAttachmentsRemove(args[1:], configPath)
	default:
		return errors.New("usage: jiractrl attachments list|upload|download|remove")
	}
}

func (a App) runAttachmentsList(args []string, configPath string) error {
	fs := newFlagSet("attachments list")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl attachments list ISSUE [--json]")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	attachments, err := client.IssueAttachments(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{"issue": fs.Arg(0), "attachments": attachments})
	}
	for _, attachment := range attachments {
		fmt.Fprintf(a.Stdout, "%s  %s  %d bytes  %s\n",
			attachment.ID, attachment.Filename, attachment.Size, attachment.MimeType)
	}
	return nil
}

func (a App) runAttachmentsUpload(args []string, configPath string) error {
	fs := newFlagSet("attachments upload")
	path := fs.String("file", "", "explicit local file path")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil ||
		fs.NArg() != 1 || strings.TrimSpace(*path) == "" {
		return errors.New("usage: jiractrl attachments upload ISSUE --file PATH [--json]")
	}
	file, err := os.Open(*path)
	if err != nil {
		return fmt.Errorf("open attachment: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect attachment: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("attachment upload path must be a regular file")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	attachments, err := client.UploadAttachment(context.Background(), fs.Arg(0), filepath.Base(*path), file)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{"issue": fs.Arg(0), "attachments": attachments})
	}
	for _, attachment := range attachments {
		fmt.Fprintf(a.Stdout, "Uploaded attachment %s (%s) to %s\n",
			attachment.ID, attachment.Filename, fs.Arg(0))
	}
	return nil
}

func (a App) runAttachmentsDownload(args []string, configPath string) error {
	fs := newFlagSet("attachments download")
	output := fs.String("output", "", "explicit destination path")
	overwrite := fs.Bool("overwrite", false, "replace an existing destination")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil ||
		fs.NArg() != 1 || strings.TrimSpace(*output) == "" {
		return errors.New("usage: jiractrl attachments download ATTACHMENT_ID --output PATH [--overwrite] [--json]")
	}
	clean, err := safeDownloadPath(*output)
	if err != nil {
		return err
	}
	if !*overwrite {
		if _, err := os.Lstat(clean); err == nil {
			return fmt.Errorf("refusing to overwrite %q; pass --overwrite to replace it", clean)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect download destination: %w", err)
		}
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	bytesWritten, err := downloadAttachmentFile(context.Background(), client, fs.Arg(0), clean, *overwrite)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{
			"attachmentId": fs.Arg(0),
			"path":         clean,
			"bytes":        bytesWritten,
			"downloaded":   true,
		})
	}
	fmt.Fprintf(a.Stdout, "Downloaded attachment %s to %s (%d bytes)\n", fs.Arg(0), clean, bytesWritten)
	return nil
}

func downloadAttachmentFile(ctx context.Context, client interface {
	DownloadAttachment(context.Context, string, io.Writer) (int64, error)
}, attachmentID, destination string, overwrite bool) (int64, error) {
	directory := filepath.Dir(destination)
	temp, err := os.CreateTemp(directory, "."+filepath.Base(destination)+".jiractrl-*")
	if err != nil {
		return 0, fmt.Errorf("create temporary download: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	written, downloadErr := client.DownloadAttachment(ctx, attachmentID, temp)
	closeErr := temp.Close()
	if downloadErr != nil {
		return 0, downloadErr
	}
	if closeErr != nil {
		return 0, closeErr
	}

	if !overwrite {
		if err := os.Link(tempPath, destination); err != nil {
			if os.IsExist(err) {
				return 0, fmt.Errorf("refusing to overwrite %q; pass --overwrite to replace it", destination)
			}
			return 0, fmt.Errorf("install downloaded attachment without overwrite: %w", err)
		}
		return written, nil
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return 0, fmt.Errorf("install downloaded attachment: %w", err)
	}
	return written, nil
}

func safeDownloadPath(path string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", errors.New("download path contains a NUL byte")
	}
	portablePath := strings.ReplaceAll(filepath.ToSlash(path), `\`, "/")
	for _, part := range strings.FieldsFunc(portablePath, func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return "", errors.New("download path must not contain parent traversal")
		}
	}
	clean := filepath.Clean(path)
	if clean == "." || filepath.Base(clean) == "." || filepath.Base(clean) == string(filepath.Separator) {
		return "", errors.New("download path must name a file")
	}
	return clean, nil
}

func (a App) runAttachmentsRemove(args []string, configPath string) error {
	fs := newFlagSet("attachments remove")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl attachments remove ATTACHMENT_ID [--json]")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	if err := client.RemoveAttachment(context.Background(), fs.Arg(0)); err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{"attachmentId": fs.Arg(0), "removed": true})
	}
	fmt.Fprintf(a.Stdout, "Removed attachment %s\n", fs.Arg(0))
	return nil
}
