package main

import (
	"encoding/json"
	"os"
)

func main() {
	var input struct {
		API   int    `json:"api"`
		Kind  string `json:"kind"`
		ID    string `json:"operation_id"`
		Event struct {
			Data struct {
				RevisionID string `json:"revision_id"`
			} `json:"data"`
		} `json:"event"`
	}
	if json.NewDecoder(os.Stdin).Decode(&input) != nil || input.API != 5 || input.Kind != "event" ||
		input.ID == "" || input.Event.Data.RevisionID == "" {
		os.Exit(1)
	}
	result := map[string]any{"set": map[string]string{
		"key": "publication-receipt", "value": input.ID + ":" + input.Event.Data.RevisionID,
	}}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(1)
	}
}
