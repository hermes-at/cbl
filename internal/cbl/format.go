package cbl

import (
	"encoding/json"
	"fmt"
	"io"
)

type RenderOptions struct {
	JSON   bool
	Waybar bool
}

func Render(w io.Writer, snap UsageSnapshot, opts RenderOptions) error {
	switch {
	case opts.Waybar:
		payload := map[string]any{
			"text":    snap.summaryLine(),
			"tooltip": snap.tooltip(),
			"class":   snap.waybarClass(),
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		return enc.Encode(payload)
	case opts.JSON:
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(snap)
	default:
		_, err := fmt.Fprintln(w, snap.tooltip())
		return err
	}
}
