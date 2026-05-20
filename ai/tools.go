package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// KnowledgeBase is what the bot's tools operate over. Repo in the main
// package satisfies this implicitly — defining the interface here keeps the
// ai package free of any dependency on the rest of the codebase.
type KnowledgeBase interface {
	ListFiles() ([]string, error)
	ReadFile(rel string) (string, error)
	Grep(pattern string, maxHits int) ([]string, error)
}

// Tool name constants — shared by tool schemas and the dispatcher so a
// rename can't drift between them.
const (
	ToolListFiles = "list_files"
	ToolReadFile  = "read_file"
	ToolGrep      = "grep"
)

// Dispatch runs a single tool call by name and returns the string result
// the model will see. Provider-neutral: the adapter is responsible for
// extracting the tool name and JSON input from whatever wire format the
// provider uses.
func Dispatch(kb KnowledgeBase, name string, rawInput json.RawMessage) string {
	switch name {
	case ToolListFiles:
		files, err := kb.ListFiles()
		if err != nil {
			return "error: " + err.Error()
		}
		return strings.Join(files, "\n")
	case ToolReadFile:
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return "error: bad input: " + err.Error()
		}
		content, err := kb.ReadFile(args.Path)
		if err != nil {
			return "error: " + err.Error()
		}
		return content
	case ToolGrep:
		var args struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal(rawInput, &args); err != nil {
			return "error: bad input: " + err.Error()
		}
		hits, err := kb.Grep(args.Pattern, 200)
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
