package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// dispatch runs a single tool call by name and returns the string result the
// model will see. Provider-neutral: the adapter is responsible for extracting
// the tool name and JSON input from whatever wire format the provider uses.
func dispatch(repo *Repo, name string, rawInput json.RawMessage) string {
	switch name {
	case "list_files":
		files, err := repo.ListFiles()
		if err != nil {
			return "error: " + err.Error()
		}
		return strings.Join(files, "\n")
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return "error: bad input: " + err.Error()
		}
		content, err := repo.ReadFile(args.Path)
		if err != nil {
			return "error: " + err.Error()
		}
		return content
	case "grep":
		var args struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return "error: bad input: " + err.Error()
		}
		hits, err := repo.Grep(args.Pattern, 200)
		if err != nil {
			return "error: " + err.Error()
		}
		if len(hits) == 0 {
			return "(no matches)"
		}
		return strings.Join(hits, "\n")
	default:
		return fmt.Sprintf("error: unknown tool %q", name)
	}
}
