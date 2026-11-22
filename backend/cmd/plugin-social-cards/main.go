//go:build wasip1

// This command serves the first-party social metadata hook as a WASI plugin.
package main

import (
	"encoding/json"
	"os"

	"github.com/mbougarne/modawana/backend/internal/extension"
)

func main() {
	var input struct {
		Kind        string                        `json:"kind"`
		Hook        string                        `json:"hook"`
		HookVersion int                           `json:"hook_version"`
		Payload     extension.RenderedMetaPayload `json:"payload"`
	}
	if json.NewDecoder(os.Stdin).Decode(&input) != nil || input.Kind != "hook" ||
		input.Hook != extension.HookRenderedMeta || input.HookVersion != 1 {
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(extension.SelectRenderedMeta(input.Payload)); err != nil {
		os.Exit(1)
	}
}
